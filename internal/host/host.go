// Package host keeps an agtop-mode session alive outside the agtop view.
//
// Each session gets one small detached `agtop host run <id>` process. It owns
// Claude Code, run headless, and serves a unix socket: a client that
// connects is sent what the session has said so far, then everything live,
// and can send messages, answer permission prompts, interrupt or stop. When
// the session goes idle the host stops Claude Code and resumes the
// conversation on the next message, so an idle agent costs only the host.
package host

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/headless"
	"github.com/0xdeafcafe/agtop/internal/state"
)

// Config is how a session is started. It is written next to the socket so
// the host process reads it on launch.
type Config struct {
	ID             string         `json:"id"`
	SessionID      string         `json:"sessionId"`
	Resume         bool           `json:"resume"` // the conversation already exists
	Account        claude.Account `json:"account"`
	Cwd            string         `json:"cwd"`
	Name           string         `json:"name,omitempty"`
	Model          string         `json:"model,omitempty"`
	PermissionMode string         `json:"permissionMode,omitempty"`
	Flags          []string       `json:"flags,omitempty"`
	Prompt         string         `json:"prompt,omitempty"` // first message
	IdleStop       Duration       `json:"idleStop,omitempty"`
	Binary         string         `json:"binary,omitempty"`
}

// Info is what the list shows about a session; the host keeps it in
// info.json and sends it to clients whenever it changes.
type Info struct {
	ID             string  `json:"id"`
	SessionID      string  `json:"sessionId"`
	Account        string  `json:"account"`
	Cwd            string  `json:"cwd"`
	Name           string  `json:"name,omitempty"`
	HostPID        int     `json:"hostPid"`
	ClaudePID      int     `json:"claudePid,omitempty"`
	State          string  `json:"state"` // starting, working, blocked, idle, stopped
	Detail         string  `json:"detail,omitempty"`
	Needs          string  `json:"needs,omitempty"`
	Model          string  `json:"model,omitempty"`
	PermissionMode string  `json:"permissionMode,omitempty"`
	CostUSD        float64 `json:"costUsd,omitempty"`
	// Queue holds messages sent while the agent was busy; the host sends
	// the first when the turn ends.
	Queue     []string  `json:"queue,omitempty"`
	Error     string    `json:"error,omitempty"`
	StartedAt time.Time `json:"startedAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Duration reads and writes as a Go duration string.
type Duration time.Duration

func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(time.Duration(d).String()) }
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	*d = Duration(v)
	return err
}

// DefaultIdleStop is how long an idle session keeps Claude Code running.
const DefaultIdleStop = 10 * time.Minute

// Root holds one directory per session.
func Root() string { return filepath.Join(state.Dir(), "sessions") }

func dir(id string) string      { return filepath.Join(Root(), id) }
func SockPath(id string) string { return filepath.Join(dir(id), "host.sock") }

// NewSessionID returns a fresh conversation id and the short id derived
// from it, the way Claude Code pairs them.
func NewSessionID() (session, short string) {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	session = h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
	return session, h[0:8]
}

// Line types the host adds to Claude Code's own output.
const (
	typeInfo     = "agtop_info"
	typeAnswered = "agtop_answered"
	typeCommands = "agtop_commands"
)

type server struct {
	cfg Config
	ln  net.Listener

	mu      sync.Mutex
	sess    *headless.Session
	began   bool // the conversation has a transcript to resume
	ring    [][]byte
	ringN   int
	clients map[*conn]struct{}
	pending map[string]headless.PermissionRequest
	info    Info
	// commands is the slash command list from Claude Code's initialize
	// reply, kept apart from the ring so every client gets it.
	commands []byte
	initID   string
	idle     *time.Timer
	quit     chan struct{}
}

// ringMax bounds what a reconnecting client is replayed.
const ringMax = 8 << 20

// Run serves the session described by dir(id)/config.json until it is
// stopped. It is what `agtop host run <id>` calls.
func Run(id string) error {
	var cfg Config
	b, err := os.ReadFile(filepath.Join(dir(id), "config.json"))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return err
	}
	if cfg.IdleStop == 0 {
		cfg.IdleStop = Duration(DefaultIdleStop)
	}
	sock := SockPath(id)
	_ = os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return err
	}
	now := time.Now()
	s := &server{
		cfg: cfg, ln: ln, began: cfg.Resume,
		clients: map[*conn]struct{}{}, pending: map[string]headless.PermissionRequest{},
		quit: make(chan struct{}),
		info: Info{ID: cfg.ID, SessionID: cfg.SessionID, Account: cfg.Account.Name, Cwd: cfg.Cwd, Name: cfg.Name,
			HostPID: os.Getpid(), State: "idle", Model: cfg.Model, PermissionMode: cfg.PermissionMode,
			StartedAt: now, UpdatedAt: now},
	}
	s.publish()
	if cfg.Prompt != "" {
		if err := s.send(cfg.Prompt, false); err != nil {
			return err
		}
	}
	go s.accept()
	<-s.quit
	_ = ln.Close()
	_ = os.Remove(sock)
	return nil
}

func (s *server) accept() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.serve(c)
	}
}

// start launches Claude Code if it is not running. Called with mu held.
func (s *server) start() error {
	if s.sess != nil {
		return nil
	}
	o := headless.Options{
		Account: s.cfg.Account, Dir: s.cfg.Cwd, Model: s.cfg.Model,
		PermissionMode: s.cfg.PermissionMode, Flags: s.cfg.Flags, Binary: s.cfg.Binary,
		Tap: s.tap,
	}
	if s.began {
		o.Resume = s.cfg.SessionID
	} else {
		o.SessionID = s.cfg.SessionID
	}
	sess, err := headless.Start(o)
	if err != nil {
		return err
	}
	s.sess = sess
	s.info.ClaudePID = sess.PID()
	s.info.Error = ""
	if s.commands == nil {
		s.initID, _ = sess.Initialize()
	}
	go s.watch(sess)
	return nil
}

// tap records Claude Code's output for replay and passes it to clients.
func (s *server) tap(line []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.record(append([]byte(nil), line...))
}

// record appends a line to the replay ring and sends it to clients. Called
// with mu held. Streaming deltas are dropped from the ring once the message
// they build arrives whole, so a replay carries each message once.
func (s *server) record(line []byte) {
	if isWholeMessage(line) {
		kept := s.ring[:0]
		n := 0
		for _, l := range s.ring {
			if !isStreamEvent(l) {
				kept = append(kept, l)
				n += len(l)
			}
		}
		s.ring, s.ringN = kept, n
	}
	s.ring = append(s.ring, line)
	s.ringN += len(line)
	for s.ringN > ringMax && len(s.ring) > 1 {
		s.ringN -= len(s.ring[0])
		s.ring = s.ring[1:]
	}
	for c := range s.clients {
		c.push(line)
	}
}

func isStreamEvent(l []byte) bool {
	return strings.HasPrefix(string(l[:min(len(l), 32)]), `{"type":"stream_event"`)
}

func isWholeMessage(l []byte) bool {
	p := string(l[:min(len(l), 24)])
	return strings.HasPrefix(p, `{"type":"assistant"`) || strings.HasPrefix(p, `{"type":"user"`)
}

// watch follows one Claude Code process until it exits.
func (s *server) watch(sess *headless.Session) {
	for ev := range sess.Events {
		s.mu.Lock()
		s.onEvent(ev)
		s.mu.Unlock()
	}
	err := sess.Err()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sess != sess {
		return
	}
	s.sess = nil
	s.info.ClaudePID = 0
	s.pending = map[string]headless.PermissionRequest{}
	if s.info.State == "working" || s.info.State == "blocked" || s.info.State == "starting" {
		// It died mid-turn; the next message resumes it.
		s.info.State = "idle"
		if err != nil {
			s.info.Error = err.Error()
		}
	}
	s.publish()
}

func (s *server) onEvent(ev headless.Event) {
	switch ev := ev.(type) {
	case headless.Init:
		s.info.Model, s.info.PermissionMode = ev.Model, ev.PermissionMode
		s.info.SessionID = ev.SessionID
	case headless.Message:
		if ev.Role == "assistant" && ev.ParentToolUseID == "" {
			for _, b := range ev.Blocks {
				switch b.Type {
				case "tool_use":
					s.info.Detail = b.Name + " " + toolSummary(b.Input)
				case "text":
					if t := strings.TrimSpace(b.Text); t != "" {
						s.info.Detail = firstLine(t)
					}
				}
			}
		}
	case headless.PermissionRequest:
		s.pending[ev.ID] = ev
		s.info.State = "blocked"
		s.info.Needs = ev.Tool + " " + toolSummary(ev.Input)
	case headless.PermissionCancelled:
		s.answered(ev.ID)
	case headless.ControlReply:
		if ev.ID == s.initID && ev.Error == "" {
			s.commands, _ = json.Marshal(map[string]any{"type": typeCommands, "commands": headless.Commands(ev)})
			for c := range s.clients {
				c.push(s.commands)
			}
		}
		return
	case headless.Result:
		s.began = true
		s.info.CostUSD += ev.CostUSD
		if len(s.pending) == 0 {
			s.info.State = "idle"
			s.info.Needs = ""
			if t := strings.TrimSpace(ev.Text); t != "" {
				s.info.Detail = firstLine(t)
			}
			if len(s.info.Queue) > 0 {
				next := s.info.Queue[0]
				s.info.Queue = s.info.Queue[1:]
				_ = s.sendLocked(next)
				return
			}
			s.armIdle()
		}
	default:
		return
	}
	s.publish()
}

// answered forgets a permission request and tells clients it is settled.
func (s *server) answered(id string) {
	if _, ok := s.pending[id]; !ok {
		return
	}
	delete(s.pending, id)
	b, _ := json.Marshal(map[string]string{"type": typeAnswered, "request_id": id})
	s.record(b)
	if len(s.pending) == 0 && s.info.State == "blocked" {
		s.info.State = "working"
		s.info.Needs = ""
	}
}

func (s *server) armIdle() {
	if s.idle != nil {
		s.idle.Stop()
	}
	sess := s.sess
	s.idle = time.AfterFunc(time.Duration(s.cfg.IdleStop), func() {
		s.mu.Lock()
		stop := s.sess == sess && s.info.State == "idle"
		s.mu.Unlock()
		if stop && sess != nil {
			_ = sess.Stop(10 * time.Second)
		}
	})
}

// publish writes info.json and sends the new info to clients. Called with
// mu held.
func (s *server) publish() {
	s.info.UpdatedAt = time.Now()
	b, _ := json.Marshal(s.info)
	tmp := filepath.Join(dir(s.cfg.ID), "info.json.tmp")
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, filepath.Join(dir(s.cfg.ID), "info.json"))
	}
	line, _ := json.Marshal(map[string]any{"type": typeInfo, "info": s.info})
	for c := range s.clients {
		c.push(line)
	}
}

// send delivers a message, or queues it while the agent is busy.
func (s *server) send(text string, now bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !now && (s.info.State == "working" || s.info.State == "blocked") {
		s.info.Queue = append(s.info.Queue, text)
		s.publish()
		return nil
	}
	return s.sendLocked(text)
}

// sendLocked gives Claude Code a message now; mid-turn it is picked up at
// the next step. Called with mu held.
func (s *server) sendLocked(text string) error {
	if s.idle != nil {
		s.idle.Stop()
	}
	if err := s.start(); err != nil {
		s.info.Error = err.Error()
		s.publish()
		return err
	}
	// Echo it so every client shows the message before Claude answers.
	b, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": text}, "agtop_sent": true})
	s.record(b)
	s.info.State = "working"
	s.info.Detail = ""
	s.publish()
	return s.sess.Send(text)
}

// editQueue applies a queue op. Called with mu held.
func (s *server) editQueue(o op) error {
	q := s.info.Queue
	if o.Index < 0 || o.Index >= len(q) {
		return fmt.Errorf("no queued message %d", o.Index)
	}
	switch o.Op {
	case "queue_edit":
		q[o.Index] = o.Text
	case "queue_remove":
		q = append(q[:o.Index], q[o.Index+1:]...)
	case "queue_move":
		to := max(0, min(o.To, len(q)-1))
		item := q[o.Index]
		q = append(q[:o.Index], q[o.Index+1:]...)
		q = append(q[:to], append([]string{item}, q[to:]...)...)
	case "queue_merge":
		// Into the one after it, so a burst of thoughts goes as one message.
		if o.Index+1 >= len(q) {
			return fmt.Errorf("nothing after queued message %d to merge with", o.Index)
		}
		q[o.Index] = q[o.Index] + "\n\n" + q[o.Index+1]
		q = append(q[:o.Index+1], q[o.Index+2:]...)
	case "queue_send":
		text := q[o.Index]
		s.info.Queue = append(q[:o.Index], q[o.Index+1:]...)
		return s.sendLocked(text)
	}
	s.info.Queue = q
	s.publish()
	return nil
}

// op is one command from a client.
type op struct {
	Op        string          `json:"op"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Always    bool            `json:"always,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Message   string          `json:"message,omitempty"`
	Interrupt bool            `json:"interrupt,omitempty"`
	Mode      string          `json:"mode,omitempty"`
	Model     string          `json:"model,omitempty"`
	Now       bool            `json:"now,omitempty"`
	Index     int             `json:"index,omitempty"`
	To        int             `json:"to,omitempty"`
}

func (s *server) do(o op) error {
	if o.Op == "send" {
		return s.send(o.Text, o.Now)
	}
	s.mu.Lock()
	sess := s.sess
	switch o.Op {
	case "queue_edit", "queue_remove", "queue_move", "queue_merge", "queue_send":
		err := s.editQueue(o)
		s.mu.Unlock()
		return err
	case "allow", "deny":
		req, ok := s.pending[o.ID]
		if !ok || sess == nil {
			s.mu.Unlock()
			return fmt.Errorf("no pending request %s", o.ID)
		}
		s.answered(o.ID)
		s.publish()
		s.mu.Unlock()
		if o.Op == "allow" {
			return sess.Allow(req, o.Input, o.Always)
		}
		return sess.Deny(req, o.Message, o.Interrupt)
	case "mode":
		s.cfg.PermissionMode = o.Mode
		s.info.PermissionMode = o.Mode
		s.publish()
	case "model":
		s.cfg.Model = o.Model
	case "stop":
		s.info.State = "stopped"
		s.publish()
		s.mu.Unlock()
		if sess != nil {
			_ = sess.Stop(10 * time.Second)
		}
		close(s.quit)
		return nil
	}
	s.mu.Unlock()
	if sess == nil {
		return nil // applied on the next start
	}
	switch o.Op {
	case "interrupt":
		return sess.Interrupt()
	case "mode":
		return sess.SetPermissionMode(o.Mode)
	case "model":
		return sess.SetModel(o.Model)
	}
	return nil
}

// conn is one connected client.
type conn struct {
	c    net.Conn
	out  chan []byte
	gone chan struct{}
	once sync.Once
}

func (c *conn) push(line []byte) {
	select {
	case c.out <- line:
	default:
		// Too far behind to catch up; it reconnects and gets a replay.
		c.close()
	}
}

func (c *conn) close() {
	c.once.Do(func() {
		close(c.gone)
		_ = c.c.Close()
	})
}

func (s *server) serve(nc net.Conn) {
	c := &conn{c: nc, out: make(chan []byte, 4096), gone: make(chan struct{})}
	s.mu.Lock()
	replay := append([][]byte(nil), s.ring...)
	if s.commands != nil {
		replay = append(replay, s.commands)
	}
	info, _ := json.Marshal(map[string]any{"type": typeInfo, "info": s.info})
	s.clients[c] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.clients, c)
		s.mu.Unlock()
		c.close()
	}()

	go func() {
		w := bufio.NewWriterSize(nc, 64<<10)
		write := func(l []byte) bool {
			_, err := w.Write(append(l, '\n'))
			return err == nil
		}
		for _, l := range replay {
			if !write(l) {
				c.close()
				return
			}
		}
		write(info)
		for {
			if w.Flush() != nil {
				c.close()
				return
			}
			select {
			case l := <-c.out:
				if !write(l) {
					c.close()
					return
				}
				for more := true; more; {
					select {
					case l := <-c.out:
						write(l)
					default:
						more = false
					}
				}
			case <-c.gone:
				return
			}
		}
	}()

	sc := bufio.NewScanner(nc)
	sc.Buffer(make([]byte, 0, 64<<10), 16<<20)
	for sc.Scan() {
		var o op
		if json.Unmarshal(sc.Bytes(), &o) != nil {
			continue
		}
		if err := s.do(o); err != nil {
			b, _ := json.Marshal(map[string]string{"type": "agtop_error", "error": err.Error()})
			c.push(b)
		}
		if o.Op == "stop" {
			return
		}
	}
}

// toolSummary picks the argument that says what a tool call does.
func toolSummary(input json.RawMessage) string {
	var m map[string]any
	if json.Unmarshal(input, &m) != nil {
		return ""
	}
	for _, k := range []string{"command", "file_path", "path", "pattern", "url", "query", "description", "prompt"} {
		if v, ok := m[k].(string); ok && v != "" {
			return firstLine(v)
		}
	}
	return ""
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if r := []rune(s); len(r) > 200 {
		s = string(r[:200]) + "…"
	}
	return s
}

// alive reports whether pid is a running process.
func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
