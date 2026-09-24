// A minimal agtop plugin in Go, with no dependencies.
//
// It offers Claude three tools: note and notes, which keep notes in the
// plugin's data folder (the only place it may write), and agents, which
// calls agtop back to list its agents. Rename it, change the tools, and keep
// the plumbing: agtop speaks JSON-RPC 2.0 on fd 3, each message a 4-byte
// big-endian length and then that many bytes of JSON.
package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// --- tools: change these ---

var tools = []map[string]any{
	{
		"name":        "note",
		"description": "Keep a note for later sessions.",
		"inputSchema": map[string]any{
			"type":                 "object",
			"properties":           map[string]any{"text": map[string]any{"type": "string", "description": "The note."}},
			"required":             []string{"text"},
			"additionalProperties": false,
		},
	},
	{
		"name":        "notes",
		"description": "Read every note kept so far.",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false},
	},
	{
		"name":        "agents",
		"description": "List the agents running in agtop.",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false},
	},
}

// callTool runs one tool. session is the agtop session that called it.
func callTool(session, name string, args json.RawMessage) (string, error) {
	file := filepath.Join(os.Getenv("AGTOP_PLUGIN_DATA"), "notes.txt")
	switch name {
	case "note":
		var in struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(args, &in); err != nil || strings.TrimSpace(in.Text) == "" {
			return "", fmt.Errorf("text is required")
		}
		f, err := os.OpenFile(file, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return "", err
		}
		defer f.Close()
		_, err = fmt.Fprintf(f, "[%s] %s\n", session, in.Text)
		return "Noted.", err
	case "notes":
		b, err := os.ReadFile(file)
		if os.IsNotExist(err) || len(b) == 0 {
			return "No notes yet.", nil
		}
		return string(b), err
	case "agents":
		// Calling agtop back: needs "list" in the manifest's "sessions".
		var ss []struct{ ID, Name, State, Detail string }
		if err := call("sessions.list", nil, &ss); err != nil {
			return "", err
		}
		var out strings.Builder
		for _, s := range ss {
			fmt.Fprintf(&out, "%s %-8s %s: %s\n", s.ID, s.State, s.Name, s.Detail)
		}
		if out.Len() == 0 {
			return "No agents.", nil
		}
		return out.String(), nil
	}
	return "", fmt.Errorf("no tool named %s", name)
}

// handle answers agtop's requests and takes its notifications.
func handle(method string, params json.RawMessage) (any, *rpcError) {
	switch method {
	case "initialize":
		// params: protocol, name, dataDir, sessions, workspaces, network.
		return map[string]any{}, nil
	case "tools.list":
		return map[string]any{"tools": tools}, nil
	case "tools.call":
		var in struct {
			Session   string          `json:"session"`
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		_ = json.Unmarshal(params, &in)
		text, err := callTool(in.Session, in.Name, in.Arguments)
		if err != nil {
			return toolResult(err.Error(), true), nil
		}
		return toolResult(text, false), nil
	case "session.event":
		// Sessions this plugin follows (sessions.subscribe) report here.
		return nil, nil
	}
	return nil, &rpcError{Code: -32601, Message: "method not found: " + method}
}

func toolResult(text string, isErr bool) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}, "isError": isErr}
}

// --- plumbing: keep as is ---

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"` // json.RawMessage when read
	Error   *rpcError       `json:"error,omitempty"`
}

var (
	ipc = os.NewFile(3, "agtop")
	wmu sync.Mutex

	pmu     sync.Mutex
	nextID  int
	pending = map[string]chan message{}
)

// call calls agtop and decodes its result into out (nil to ignore it).
func call(method string, params, out any) error {
	p, _ := json.Marshal(params)
	pmu.Lock()
	nextID++
	id := json.RawMessage(fmt.Sprint(nextID))
	ch := make(chan message, 1)
	pending[string(id)] = ch
	pmu.Unlock()
	send(message{ID: id, Method: method, Params: p})
	m := <-ch
	if m.Error != nil {
		return fmt.Errorf("%s", m.Error.Message)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(m.Result.(json.RawMessage), out)
}

func send(m message) {
	m.JSONRPC = "2.0"
	b, _ := json.Marshal(m)
	buf := make([]byte, 4+len(b))
	binary.BigEndian.PutUint32(buf, uint32(len(b)))
	copy(buf[4:], b)
	wmu.Lock()
	defer wmu.Unlock()
	_, _ = ipc.Write(buf)
}

func main() {
	if ipc == nil {
		fmt.Fprintln(os.Stderr, "run me from agtop: I talk on fd 3")
		os.Exit(2)
	}
	r := bufio.NewReader(ipc)
	for {
		var hdr [4]byte
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			return // agtop closed the channel: time to go
		}
		body := make([]byte, binary.BigEndian.Uint32(hdr[:]))
		if _, err := io.ReadFull(r, body); err != nil {
			return
		}
		var m message
		var raw struct {
			Result json.RawMessage `json:"result"`
		}
		if json.Unmarshal(body, &m) != nil || json.Unmarshal(body, &raw) != nil {
			continue
		}
		if m.Method == "" {
			// A reply to one of our calls.
			m.Result = raw.Result
			pmu.Lock()
			ch := pending[string(m.ID)]
			delete(pending, string(m.ID))
			pmu.Unlock()
			if ch != nil {
				ch <- m
			}
			continue
		}
		// Requests run concurrently; notifications (no id) in order.
		if len(m.ID) == 0 {
			handle(m.Method, m.Params)
			continue
		}
		go func() {
			res, e := handle(m.Method, m.Params)
			if e == nil && res == nil {
				res = map[string]any{}
			}
			send(message{ID: m.ID, Result: res, Error: e})
		}()
	}
}
