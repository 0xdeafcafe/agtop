package host

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/0xdeafcafe/agtop/internal/headless"
)

// Spawn writes cfg and starts its host as a detached process, returning once
// the host is accepting connections. cfg.ID and cfg.SessionID are filled in
// for a new conversation when empty.
// fillIDs names a new session, and gives a fork an id of its own.
func (cfg *Config) fillIDs() {
	if cfg.SessionID == "" {
		cfg.SessionID, cfg.ID = NewSessionID()
	}
	if cfg.ID == "" && cfg.Fork {
		_, cfg.ID = NewSessionID() // the copy is a session of its own
	}
	if cfg.ID == "" {
		cfg.ID = shortOf(cfg.SessionID)
	}
}

func Spawn(cfg Config) (Config, error) {
	cfg.fillIDs()
	d := dir(cfg.ID)
	if err := os.MkdirAll(d, 0o700); err != nil {
		return cfg, err
	}
	if info, err := ReadInfo(cfg.ID); err == nil && alive(info.HostPID) {
		return cfg, fmt.Errorf("%s is already running", cfg.ID)
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return cfg, err
	}
	if err := os.WriteFile(filepath.Join(d, "config.json"), b, 0o600); err != nil {
		return cfg, err
	}
	exe, err := os.Executable()
	if err != nil {
		return cfg, err
	}
	log, err := os.OpenFile(filepath.Join(d, "host.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return cfg, err
	}
	defer log.Close()
	_ = os.Remove(SockPath(cfg.ID))
	cmd := exec.Command(exe, "host", "run", cfg.ID)
	cmd.Dir = cfg.Cwd
	cmd.Stdout, cmd.Stderr = log, log
	// Its own session, so closing agtop or its terminal leaves it running.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return cfg, err
	}
	pid := cmd.Process.Pid
	// Reap it when it ends, or a stopped host lingers as a zombie that still
	// looks alive for as long as this process runs.
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if c, err := net.Dial("unix", SockPath(cfg.ID)); err == nil {
			_ = c.Close()
			return cfg, nil
		}
		select {
		case <-exited:
			tail, _ := os.ReadFile(filepath.Join(d, "host.log"))
			return cfg, fmt.Errorf("host for %s exited: %s", cfg.ID, lastLine(string(tail)))
		default:
		}
	}
	return cfg, fmt.Errorf("host for %s (pid %d) did not start listening", cfg.ID, pid)
}

func shortOf(sessionID string) string {
	var out []rune
	for _, r := range sessionID {
		if r == '-' {
			continue
		}
		out = append(out, r)
		if len(out) == 8 {
			break
		}
	}
	return string(out)
}

func lastLine(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '\n' {
			return s[i+1:]
		}
	}
	return s
}

// ReadInfo reads a session's last published info. A session whose host has
// gone is reported stopped.
func ReadInfo(id string) (Info, error) {
	info, err := readInfoFile(id)
	if err == nil && !alive(info.HostPID) {
		info.State, info.ClaudePID = "stopped", 0
	}
	return info, err
}

func readInfoFile(id string) (Info, error) {
	var info Info
	b, err := os.ReadFile(filepath.Join(dir(id), "info.json"))
	if err != nil {
		return info, err
	}
	return info, json.Unmarshal(b, &info)
}

// List returns every agtop-mode session, newest first.
func List() []Info { return new(Lister).List() }

// Lister lists sessions again and again, parsing only the info files that
// changed; every session ever started stays listed, so most are unchanged.
type Lister struct {
	infos map[string]listed
}

type listed struct {
	mod  time.Time
	size int64
	info Info
	err  error
}

// List is List.
func (l *Lister) List() []Info {
	if l.infos == nil {
		l.infos = map[string]listed{}
	}
	ents, _ := os.ReadDir(Root())
	var out []Info
	for _, e := range ents {
		id := e.Name()
		st, err := os.Stat(filepath.Join(dir(id), "info.json"))
		if err != nil {
			delete(l.infos, id)
			continue
		}
		c, ok := l.infos[id]
		if !ok || !c.mod.Equal(st.ModTime()) || c.size != st.Size() {
			c = listed{mod: st.ModTime(), size: st.Size()}
			c.info, c.err = readInfoFile(id)
			l.infos[id] = c
		}
		if c.err != nil {
			continue
		}
		info := c.info
		if info.State != "stopped" && !alive(info.HostPID) {
			info.State, info.ClaudePID = "stopped", 0
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}

// ReadConfig reads the config a session was started with, to restart it.
func ReadConfig(id string) (Config, error) {
	var cfg Config
	b, err := os.ReadFile(filepath.Join(dir(id), "config.json"))
	if err != nil {
		return cfg, err
	}
	return cfg, json.Unmarshal(b, &cfg)
}

// InfoEvent is a session's info, sent on connect and whenever it changes.
type InfoEvent struct{ Info Info }

// Answered says a permission request was settled, here or by another client.
type Answered struct{ ID string }

// ErrorEvent reports a command the host could not carry out.
type ErrorEvent struct{ Error string }

// Sent is a message a client sent, echoed so every client shows it.
type Sent struct {
	Text   string
	Images []string // names of attached images
}

// Stamp is when the output that follows it happened.
type Stamp struct{ At time.Time }

// Commands lists the session's slash commands.
type Commands struct{ Commands []headless.Command }

// Reply answers a control request a client passed through with Ask: ID is
// the client's own.
type Reply struct {
	ID    string
	Body  json.RawMessage
	Error string
}

// Context is what fills the context window, as last counted.
type Context struct{ Usage headless.ContextUsage }

// Decode reads one line from a host: its own events, or Claude Code's.
func Decode(line []byte) (any, error) {
	// Claude Code's lines start with their type; the host's own (and its
	// echo of what you sent) are written with sorted keys and don't. So
	// most lines, deltas above all, are only taken apart once.
	if bytes.HasPrefix(line, []byte(`{"type":"`)) && !bytes.HasPrefix(line, []byte(`{"type":"agtop_`)) {
		return headless.Decode(line)
	}
	var head struct {
		Type      string                 `json:"type"`
		Info      Info                   `json:"info"`
		RequestID string                 `json:"request_id"`
		Error     string                 `json:"error"`
		Sent      bool                   `json:"agtop_sent"`
		Images    []string               `json:"agtop_images"`
		Message   json.RawMessage        `json:"message"`
		Commands  []headless.Command     `json:"commands"`
		Context   *headless.ContextUsage `json:"context"`
		ID        string                 `json:"id"`
		Reply     json.RawMessage        `json:"reply"`
		T         int64                  `json:"t"`
	}
	if err := json.Unmarshal(line, &head); err != nil {
		return nil, err
	}
	switch head.Type {
	case typeInfo:
		return InfoEvent{Info: head.Info}, nil
	case typeAnswered:
		return Answered{ID: head.RequestID}, nil
	case "agtop_error":
		return ErrorEvent{Error: head.Error}, nil
	case typeCommands:
		return Commands{Commands: head.Commands}, nil
	case typeReply:
		return Reply{ID: head.ID, Body: head.Reply, Error: head.Error}, nil
	case typeContext:
		if head.Context == nil {
			return nil, errors.New("context line without a count")
		}
		return Context{Usage: *head.Context}, nil
	case typeTime:
		return Stamp{At: time.UnixMilli(head.T)}, nil
	}
	if head.Sent {
		var m struct {
			Content string `json:"content"`
		}
		_ = json.Unmarshal(head.Message, &m)
		return Sent{Text: m.Content, Images: head.Images}, nil
	}
	return headless.Decode(line)
}

// Client is a connection to one session's host.
type Client struct {
	// Lines carries every line from the host, replay first; it closes when
	// the connection does.
	Lines <-chan []byte

	c   net.Conn
	wmu sync.Mutex
}

// Dial connects to a running session.
func Dial(id string) (*Client, error) {
	c, err := net.Dial("unix", SockPath(id))
	if err != nil {
		return nil, err
	}
	lines := make(chan []byte, 1024)
	go func() {
		defer close(lines)
		r := headless.NewLineReader(c)
		for {
			l, ok := r.Next()
			if !ok {
				return
			}
			lines <- append([]byte(nil), l...)
		}
	}()
	return &Client{Lines: lines, c: c}, nil
}

func (c *Client) do(o op) error {
	b, err := json.Marshal(o)
	if err != nil {
		return err
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err = c.c.Write(append(b, '\n'))
	return err
}

// Send delivers a message, or queues it if the agent is busy.
func (c *Client) Send(text string) error { return c.do(op{Op: "send", Text: text}) }

// SendNow delivers a message mid-turn; Claude reads it at its next step.
func (c *Client) SendNow(text string) error { return c.do(op{Op: "send", Text: text, Now: true}) }

// SendImages delivers a message with image files attached; it goes now,
// even mid-turn, since only text can wait in the queue.
func (c *Client) SendImages(text string, paths []string) error {
	return c.do(op{Op: "send", Text: text, Images: paths, Now: true})
}

// Queue edits, by index into Info.Queue.
// The queue ops name a message by its place and its text as you saw it, so
// they still find it if the queue moved in between.

func (c *Client) EditQueued(i int, was, text string) error {
	return c.do(op{Op: "queue_edit", Index: i, Was: was, Text: text})
}
func (c *Client) RemoveQueued(i int, was string) error {
	return c.do(op{Op: "queue_remove", Index: i, Was: was})
}
func (c *Client) MoveQueued(i int, was string, to int) error {
	return c.do(op{Op: "queue_move", Index: i, Was: was, To: to})
}
func (c *Client) MergeQueued(i int, was string) error {
	return c.do(op{Op: "queue_merge", Index: i, Was: was})
}
func (c *Client) SendQueued(i int, was string) error {
	return c.do(op{Op: "queue_send", Index: i, Was: was})
}

// HoldQueue pauses or resumes sending the queue.
func (c *Client) HoldQueue(on bool) error { return c.do(op{Op: "queue_hold", Now: on}) }

// QueueSeparately sends queued messages one per turn instead of together.
func (c *Client) QueueSeparately(on bool) error { return c.do(op{Op: "queue_separate", Now: on}) }

// Allow lets a pending tool call run; input nil keeps the requested input.
func (c *Client) Allow(id string, input json.RawMessage, always bool) error {
	return c.do(op{Op: "allow", ID: id, Input: input, Always: always})
}

func (c *Client) Deny(id, message string, interrupt bool) error {
	return c.do(op{Op: "deny", ID: id, Message: message, Interrupt: interrupt})
}

func (c *Client) Interrupt() error { return c.do(op{Op: "interrupt"}) }

// StopTask stops one subagent or background shell by its task id, and
// nothing else.
func (c *Client) StopTask(id string) error         { return c.do(op{Op: "stop_task", ID: id}) }
func (c *Client) SetPermissionMode(m string) error { return c.do(op{Op: "mode", Mode: m}) }
func (c *Client) SetModel(m string) error          { return c.do(op{Op: "model", Model: m}) }

// Relogin tells the session ~/.claude is now signed in as another account.
func (c *Client) Relogin() error { return c.do(op{Op: "relogin"}) }

// ContinueAtReset says whether a session stopped by a usage limit carries
// on when the limit resets.
func (c *Client) ContinueAtReset(yes bool) error { return c.do(op{Op: "limit", Now: yes}) }

// SetEffort changes effort; it applies from the next Claude Code start.
func (c *Client) SetEffort(e string) error { return c.do(op{Op: "effort", Effort: e}) }

// Rewind carries on from another conversation, sessionID: a copy cut
// before one of your messages (resume), a fresh one, or one of its
// Branches. The path it leaves is kept as a branch, as left describes it.
// The connection closes once it has; dial again to see it.
func (c *Client) Rewind(sessionID string, resume bool, left Branch) error {
	return c.do(op{Op: "rewind", Text: sessionID, Now: resume, Branch: &left})
}

// Restart ends a session's host and starts it again on this agtop's
// binary, with change applied to its config first: how a host from an
// older agtop gets what's new. The session must be idle.
func Restart(id string, change func(*Config)) error {
	cfg, err := ReadConfig(id)
	if err != nil {
		return err
	}
	info, _ := ReadInfo(id)
	if c, err := Dial(id); err == nil {
		_ = c.Stop()
		c.Close()
	}
	for deadline := time.Now().Add(15 * time.Second); alive(info.HostPID); time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			return fmt.Errorf("the host for %s didn't stop", id)
		}
	}
	change(&cfg)
	cfg.Prompt, cfg.Images = "", nil
	_, err = Spawn(cfg)
	return err
}

// RewindByRestart is Rewind for a host from before it could (Proto 0):
// the host is restarted on this agtop, already carrying on from
// sessionID, with the path it leaves kept as a branch.
func RewindByRestart(id, sessionID string, resume bool, left Branch) error {
	return Restart(id, func(cfg *Config) {
		_, err := os.Stat(cfg.Account.TranscriptPath(cfg.Cwd, cfg.SessionID))
		cfg.rewindTo(sessionID, resume, &left, err == nil)
	})
}

// Stop ends the session and its host; the conversation is kept.
func (c *Client) Stop() error { return c.do(op{Op: "stop"}) }

// Ask passes a control request to Claude Code (req carries its subtype:
// side_question, export_conversation, …), waking it if it's asleep. The
// answer arrives on Lines as a Reply with this id. Hosts before Proto 2
// ignore it.
func (c *Client) Ask(id string, req any) error {
	b, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return c.do(op{Op: "ask", ID: id, Request: b})
}

// AskContext asks for a fresh count of what fills the context window; it
// arrives on Lines as a Context. Hosts before Proto 2 ignore it.
func (c *Client) AskContext() error { return c.do(op{Op: "context"}) }

// Close ends the connection; it's safe on a nil client.
func (c *Client) Close() error {
	if c == nil || c.c == nil {
		return nil
	}
	return c.c.Close()
}
