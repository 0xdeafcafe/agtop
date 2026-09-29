// Package host keeps a rush-mode session alive outside the rush view.
//
// Each session gets one small detached `rush host run <id>` process. It owns
// the session's agent, run headless through its adapter, and serves a unix
// socket: a client that
// connects is sent what the session has said so far, then everything live,
// and can send messages, answer permission prompts, interrupt or stop. When
// the session goes idle the host stops the agent and resumes the
// conversation on the next message, so an idle agent costs only the host.
package host

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/event"
	"github.com/0xdeafcafe/rush/internal/jsonx"
	"github.com/0xdeafcafe/rush/internal/netproof"
	"github.com/0xdeafcafe/rush/internal/plugin"
	"github.com/0xdeafcafe/rush/internal/proc"
	"github.com/0xdeafcafe/rush/internal/state"
)

// Config is how a session is started. It is written next to the socket so
// the host process reads it on launch.
type Config struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	Resume    bool   `json:"resume"` // the conversation already exists
	// Fork continues a copy of the conversation (Claude Code's
	// --fork-session), leaving the original to whoever has it open. Once
	// Claude Code names the copy, SessionID becomes that and Fork is cleared.
	Fork bool `json:"fork,omitzero"`
	// From is the conversation this one continues, for showing its history
	// (a fork's own transcript may start empty).
	From           string        `json:"from,omitempty"`
	Account        agent.Profile `json:"account"` // as {"name", "configDir"}: see wireConfig
	Cwd            string        `json:"cwd"`
	Name           string        `json:"name,omitempty"`
	Model          string        `json:"model,omitempty"`
	Effort         string        `json:"effort,omitempty"`
	PermissionMode string        `json:"permissionMode,omitempty"`
	Flags          []string      `json:"flags,omitempty"`
	Prompt         string        `json:"prompt,omitempty"` // first message
	Images         []string      `json:"images,omitempty"` // files attached to it
	IdleStop       Duration      `json:"idleStop,omitzero"`
	// LimitMode is what happens when a usage limit stops the session:
	// "auto" continues at the reset, "off" waits for you, and "" (opt-in)
	// asks once per session.
	LimitMode string `json:"limitMode,omitempty"`
	// RetryBase is the first wait before retrying an API error; each retry
	// doubles it. RetryMax caps the attempts. Zero means the defaults.
	RetryBase Duration `json:"retryBase,omitzero"`
	RetryMax  int      `json:"retryMax,omitzero"`
	Binary    string   `json:"binary,omitempty"`
	// Lean starts Claude Code without its non-essential network traffic:
	// it's ready in about half the time, but without the features that
	// need it (DesignSync, Projects, plugin downloads, live preview).
	Lean bool `json:"lean,omitzero"`
	// Branches are the paths /rewind left behind, newest last, so you can
	// go back down one.
	Branches []Branch `json:"branches,omitempty"`
	// StartedBy is the plugin that started the session, if one did; only
	// it may send to it or stop it.
	StartedBy string `json:"startedBy,omitempty"`
	// Meta is what whoever started it tagged it with (a plugin's card or
	// ticket id, say), handed back wherever the session is listed.
	Meta map[string]string `json:"meta,omitempty"`
	// Env is added to Claude Code's environment (KEY=value), on every start
	// of it: idle restarts and resumes too.
	Env []string `json:"env,omitempty"`
	// Kind is the agent the session runs, through its adapter. It's always
	// written; one read without it is Claude Code's (agent.Migrated).
	Kind string `json:"kind,omitempty"`
	// Profile is the profile the session was started under: which
	// providers it may move to, and what it does at a usage limit.
	Profile string `json:"profile,omitempty"`
	// SystemPrompt is added to the agent's system prompt, after rush's own.
	SystemPrompt string `json:"systemPrompt,omitempty"`
}

// Branch is a path of the conversation that /rewind left: its own
// conversation, sharing the turns before From with the one it left for.
type Branch struct {
	SessionID string    `json:"sessionId"`
	Left      time.Time `json:"left"`
	From      int       `json:"from,omitzero"`  // the first turn of its own
	Turns     int       `json:"turns,omitzero"` // your messages on it
	Last      string    `json:"last,omitempty"` // the last of them
}

// maxBranches is how many left paths an agent remembers.
const maxBranches = 30

// Info is what the list shows about a session; the host keeps it in
// info.json and sends it to clients whenever it changes.
type Info struct {
	ID             string  `json:"id"`
	SessionID      string  `json:"sessionId"`
	Account        string  `json:"account"`
	Cwd            string  `json:"cwd"`
	Name           string  `json:"name,omitempty"`
	HostPID        int     `json:"hostPid"`
	ClaudePID      int     `json:"claudePid,omitzero"`
	State          string  `json:"state"` // starting, working, blocked, idle, stopped
	Detail         string  `json:"detail,omitempty"`
	Needs          string  `json:"needs,omitempty"`
	Model          string  `json:"model,omitempty"`
	Effort         string  `json:"effort,omitempty"`
	PermissionMode string  `json:"permissionMode,omitempty"`
	CostUSD        float64 `json:"costUsd,omitzero"`
	// Billing is how its requests are paid for: plan, overage or metered
	// (usage.Billing); empty until the agent says.
	Billing string `json:"billing,omitempty"`
	// Queue holds messages sent while the agent was busy; the host sends
	// the first when the turn ends.
	Queue []string `json:"queue,omitempty"`
	// QueueHeld pauses sending the queue; QueueSeparate sends one queued
	// message per turn instead of the whole queue as one.
	QueueHeld     bool `json:"queueHeld,omitzero"`
	QueueSeparate bool `json:"queueSeparate,omitzero"`
	// Limit is set while a usage limit has stopped the session.
	Limit *Limit `json:"limit,omitempty"`
	// Retry is set while an API error is being retried, or has given up.
	Retry *Retry `json:"retry,omitempty"`
	// CacheWarm is when the prompt cache written by the last request
	// expires; a request after it re-reads the whole context.
	CacheWarm time.Time `json:"cacheWarm"`
	Error     string    `json:"error,omitempty"`
	StartedAt time.Time `json:"startedAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	// RewoundAt is when /rewind last switched it to an earlier point of
	// the conversation: everything before it is in the transcript.
	RewoundAt time.Time `json:"rewoundAt"`
	// ReplayFrom is set once the replay no longer reaches back to the
	// start: the turn it now begins with started then, and everything
	// before it is in the transcript.
	ReplayFrom time.Time `json:"replayFrom"`
	// Proto is what the host can do, so a newer rush can tell a host from
	// an older one (0) that needs restarting to do it: see Proto.
	Proto int `json:"proto,omitzero"`
	// Kind is the agent it runs: empty is Claude Code.
	Kind string `json:"kind,omitempty"`
	// Profile is the config's: the profile it was started under.
	Profile string `json:"profile,omitempty"`
	// StartedBy is the plugin that started it, if one did.
	StartedBy string `json:"startedBy,omitempty"`
	// Meta is the config's, as it started with.
	Meta map[string]string `json:"meta,omitempty"`
	// ContextTokens is how much context the last request sent: the
	// conversation's size as the model sees it, until it next compacts.
	ContextTokens int `json:"contextTokens,omitzero"`
	// Background is what Claude Code has running in the background: shells,
	// monitors, subagents and workflows, in the order they started.
	Background []Task `json:"background,omitempty"`
	// Relogin is an agent started before its profile was signed in as
	// another account: it starts again on the new one at its next safe
	// point, once its turn and the work it left running are done.
	Relogin bool `json:"relogin,omitempty"`
	// Homes: its agent starts in the home of the login in use, so it
	// follows a switch. A host from before homes stays on ~/.claude's
	// sign-in, and has to be replaced to move.
	Homes bool `json:"homes,omitzero"`
}

// Task is one thing running in the background.
type Task struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"` // local_bash, local_agent, monitor_mcp, ...
	Label     string    `json:"label"`
	StartedAt time.Time `json:"startedAt"`
}

// Proto is this build's host protocol: 1 adds rewind, 2 context usage and
// control requests passed through (the ask op), 3 moving a running tool
// to the background (the background op) and Info.Background, 4 other
// agents' sessions as rush's own events, 5 the client's hello (hello.go),
// 6 Claude Code's sessions as rush's own events too, to a client that
// says it reads them. A client sends its build's in the hello.
const Proto = 6

// Limit describes a usage limit that stopped the session.
type Limit struct {
	ResetsAt time.Time `json:"resetsAt"`
	Window   string    `json:"window,omitempty"` // five_hour, seven_day, …
	// Continue is whether the session carries on at the reset; Ask is set
	// when that's still yours to decide.
	Continue bool `json:"continue"`
	Ask      bool `json:"ask,omitzero"`
}

// Retry describes an API error being retried.
type Retry struct {
	Reason  string    `json:"reason"`
	Attempt int       `json:"attempt"`
	Max     int       `json:"max"`
	Next    time.Time `json:"next"`
	// GaveUp is set when retries ran out or the next would land after the
	// cache expired; a message from you retries.
	GaveUp bool   `json:"gaveUp,omitzero"`
	Why    string `json:"why,omitempty"`
	// Offline is set while the API can't be reached: it continues once it
	// can, however long that takes.
	Offline bool `json:"offline,omitzero"`
	// Proof is set while its prompt cache has expired and it waits for
	// proof the connection holds before trying again (see netproof).
	Proof bool `json:"proof,omitzero"`
}

// cacheLife is how long the prompt cache lasts. Claude Code writes the
// one-hour cache (usage reports ephemeral_1h_input_tokens).
const cacheLife = time.Hour

// Duration reads and writes as a Go duration string.
type Duration time.Duration

func (d Duration) MarshalJSON() ([]byte, error) { return jsonx.Marshal(time.Duration(d).String()) }
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := jsonx.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	*d = Duration(v)
	return err
}

// DefaultIdleStop is how long an idle session keeps Claude Code running:
// only a moment, since starting it again takes about a second and the
// prompt cache isn't lost, while a running one holds 150-200 MB.
const DefaultIdleStop = 3 * time.Second

// Root holds one directory per session.
func Root() string { return filepath.Join(state.Dir(), "sessions") }

func dir(id string) string { return filepath.Join(Root(), id) }

// TempDir is where a session's Claude Code and everything it runs keep
// their scratch files.
func TempDir(id string) string  { return filepath.Join(dir(id), "tmp") }
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
	// The host's own lines keep the name it had as agtop: sessions' logs
	// hold them, and hosts from before the rename still write them.
	typeInfo     = "agtop_info"
	typeAnswered = "agtop_answered"
	typeCommands = "agtop_commands"
	typeContext  = "agtop_context"
	typeReply    = "agtop_reply"
	typeTime     = "agtop_time"
)

type server struct {
	cfg Config
	ln  net.Listener

	mu sync.Mutex
	// conn is the running session, and options each of its approvals'
	// answers.
	conn    agent.Conn
	options map[string][]event.Option
	began   bool      // the conversation has a transcript to resume
	cwdAt   time.Time // when followCwd last looked
	spent   float64   // the running process's last cost total: see TurnCost
	ring    [][]byte  // big lines packed: see pack
	ringN   int       // the ring's size as written
	pk      packer
	clients map[*conn]struct{}
	pending map[string]asked
	info    Info
	// commands is the session's slash command list, kept apart from the
	// ring so every client gets it.
	commands []byte
	ctxOut   bool // the context-usage question is out
	// taskStart is when each of the agent's tasks still running started.
	taskStart map[string]time.Time
	asking    int            // control requests out for clients
	context   []byte         // the last answer, as the line clients get
	stamped   time.Time      // when the last time mark went into the ring
	limited   *event.Limited // the limit that stopped this turn, if one did
	wake      *time.Timer    // a scheduled continue or retry
	gen       int            // bumped by every send; a stale timer does nothing
	idle      *time.Timer
	// stopping is closed once an agent being stopped has gone; a new one
	// waits for it, so two never run the same conversation.
	stopping chan struct{}
	// quietWait is set while an idle agent due to move to another account
	// is being watched for its own work to end: one watch at a time.
	quietWait bool
	quit      chan struct{}
	stopOnce  sync.Once
	// broker reaches the approved plugins' MCP servers.
	broker plugin.Broker
}

var lowGC sync.Once

// ringMax bounds what a reconnecting client is replayed.
const ringMax = 8 << 20

// Run serves the session described by dir(id)/config.json until it is
// stopped. It is what `rush host run <id>` calls.
func Run(id string) error {
	// A host lives as long as its session and mostly waits. Its heap is the
	// replay ring and whatever line is passing through; a soft limit makes
	// the collector hand memory back rather than keep a turn's high water.
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(48 << 20)
	}
	// Run from a session's shell, it finds the agents' own programs, not
	// the stand-ins that ran it.
	_ = os.Setenv("PATH", WithoutShims(os.Getenv("PATH")))
	var cfg Config
	b, err := os.ReadFile(filepath.Join(dir(id), "config.json"))
	if err != nil {
		return err
	}
	if err := jsonx.Unmarshal(b, &cfg); err != nil {
		return err
	}
	if cfg.IdleStop == 0 {
		cfg.IdleStop = Duration(DefaultIdleStop)
	}
	endStrays(cfg.Kind, cfg.SessionID)
	sock := SockPath(id)
	_ = os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return err
	}
	now := time.Now()
	s := &server{
		cfg: cfg, ln: ln, began: cfg.Resume,
		clients: map[*conn]struct{}{}, pending: map[string]asked{},
		quit: make(chan struct{}),
		info: Info{ID: cfg.ID, Kind: cfg.Kind, SessionID: cfg.SessionID, Account: cfg.Account.Name, Cwd: cfg.Cwd, Name: cfg.Name,
			HostPID: os.Getpid(), State: "idle", Proto: Proto, Model: cfg.Model, Effort: cfg.Effort, PermissionMode: cfg.PermissionMode,
			StartedAt: now, UpdatedAt: now, StartedBy: cfg.StartedBy, Meta: cfg.Meta, Profile: cfg.Profile, Homes: true},
	}
	s.publish()
	if cfg.Prompt != "" || len(cfg.Images) > 0 {
		if err := s.send(cfg.Prompt, cfg.Images, false); err != nil {
			return err
		}
	}
	go s.accept()
	go s.watchSock(sock)
	go s.trimLoop()
	<-s.quit
	_ = ln.Close()
	_ = os.Remove(sock)
	return nil
}

// watchSock stops the host once its socket is gone, as when its folder was
// deleted: no client can reach it again, so it would only linger.
func (s *server) watchSock(sock string) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.quit:
			return
		case <-t.C:
			if _, err := os.Stat(sock); errors.Is(err, os.ErrNotExist) {
				_ = s.do(op{Op: "stop"})
				return
			}
		}
	}
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

// TurnCost is what one turn cost. A process's results carry its running
// total, not the turn's, so it is the rise since the last total, spent; a
// total below that is a new process counting from zero.
func TurnCost(spent *float64, total float64) float64 {
	d := total - *spent
	if d < 0 {
		d = total
	}
	*spent = total
	return d
}

// detach forgets the running session so nothing more is sent to it, and
// returns it for stopping outside the lock. Called with mu held.
func (s *server) detach() agent.Conn {
	c := s.conn
	s.conn = nil
	s.info.ClaudePID = 0
	s.info.Background = nil
	s.info.Relogin = false
	s.pending = map[string]asked{}
	return c
}

// retire stops a detached agent off the lock; start waits until it has
// gone. Called with mu held.
func (s *server) retire(conn agent.Conn) {
	done := make(chan struct{})
	s.stopping = done
	go func() {
		stopAgent(conn)
		s.mu.Lock()
		if s.stopping == done {
			s.stopping = nil
		}
		s.mu.Unlock()
		close(done)
		debug.FreeOSMemory()
	}()
}

// saveConfig writes the config back, for what changes while running.
// Called with mu held.
func (s *server) saveConfig() {
	b, err := jsonx.MarshalIndent(s.cfg)
	if err != nil {
		return
	}
	tmp := filepath.Join(dir(s.cfg.ID), "config.json.tmp")
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, filepath.Join(dir(s.cfg.ID), "config.json"))
	}
}

// background is the list the agent just sent, with when each one started:
// as it said then, or as the list first had it.
func background(was []Task, now []event.BackgroundTask, started map[string]time.Time, at time.Time) []Task {
	var out []Task
	for _, t := range now {
		started, ok := started[t.ID]
		if !ok {
			started = at
		}
		for _, w := range was {
			if w.ID == t.ID && !ok {
				started = w.StartedAt
			}
		}
		out = append(out, Task{ID: t.ID, Type: t.Type, Label: t.Label, StartedAt: started})
	}
	return out
}

// tap records the agent's own lines for replay and passes them to clients:
// its adapter keeps the traffic with rush's own tools out of them.
func (s *server) tap(line []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.record(append([]byte(nil), line...))
}

// record appends a line to the replay ring and sends it to clients. Called
// with mu held. Streaming deltas are dropped from the ring once the message
// they build arrives whole, so a replay carries each message once.
func (s *server) record(line []byte) {
	// A time mark before output that follows a pause, so a client replaying
	// the ring knows when things happened, not just in what order.
	// A turn's first line always gets one: a trimmed ring starts there.
	if now := time.Now(); now.Sub(s.stamped) >= 500*time.Millisecond || isEcho(line) {
		s.stamped = now
		b, _ := jsonx.Marshal(map[string]any{"type": typeTime, "t": now.UnixMilli()})
		s.ring = append(s.ring, b)
		s.ringN += len(b)
		for c := range s.clients {
			c.push(b)
		}
	}
	if isWholeMessage(line) {
		kept := s.ring[:0]
		n := 0
		for _, l := range s.ring {
			if !isStreamEvent(l) {
				kept = append(kept, l)
				n += lineLen(l)
			}
		}
		s.ring, s.ringN = kept, n
	}
	s.ring = append(s.ring, s.pk.pack(line))
	s.ringN += len(line)
	s.trim()
	for c := range s.clients {
		c.push(line)
	}
}

// trim drops the oldest of the ring once it outgrows ringMax: whole turns
// where it can, so a client takes the turns before the replay from the
// transcript, whole. Only a turn bigger than the ring itself loses its
// start. Called with mu held.
func (s *server) trim() {
	if s.ringN <= ringMax {
		return
	}
	n, cut, from := s.ringN, -1, s.info.ReplayFrom
	fallback, fallbackFrom := -1, from
	for i, l := range s.ring[:len(s.ring)-1] {
		if t, ok := turnStart(l, s.ring[i+1]); ok {
			from = t
			if n <= ringMax {
				cut = i
				break
			}
		}
		if fallback < 0 && n <= ringMax*3/4 {
			fallback, fallbackFrom = i, from
		}
		n -= lineLen(l)
	}
	if cut < 0 {
		// One turn is all of it: keep its latest part, with room to grow.
		cut, from = fallback, fallbackFrom
		if cut < 0 {
			cut = len(s.ring) - 1
		}
	}
	for _, l := range s.ring[:cut] {
		s.ringN -= lineLen(l)
	}
	s.ring = slices.Clone(s.ring[cut:])
	if !from.Equal(s.info.ReplayFrom) {
		s.info.ReplayFrom = from
		s.publish()
	}
}

// turnStart reports whether l is the time mark before a turn's first line,
// next, and when that was.
func turnStart(l, next []byte) (time.Time, bool) {
	if !bytes.HasPrefix(l, []byte(`{"t":`)) || !isEcho(next) {
		return time.Time{}, false
	}
	var m struct {
		T int64 `json:"t"`
	}
	if jsonx.Unmarshal(l, &m) != nil {
		return time.Time{}, false
	}
	return time.UnixMilli(m.T), true
}

// isEcho is the host's echo of a message sent: the start of a turn.
func isEcho(line []byte) bool {
	return bytes.HasPrefix(line, []byte(`{"agtop_`)) && bytes.Contains(line, []byte(`"agtop_sent":true`))
}

func isStreamEvent(l []byte) bool {
	return bytes.HasPrefix(l, []byte(`{"type":"stream_event"`)) || isEventLine(l, "delta", "part_start", "message_start")
}

func isWholeMessage(l []byte) bool {
	return bytes.HasPrefix(l, []byte(`{"type":"assistant"`)) || bytes.HasPrefix(l, []byte(`{"type":"user"`)) || isEventLine(l, "message")
}

// stalled handles a turn that ended on a usage limit or an API error,
// scheduling a continue or a retry when that's allowed. It reports whether
// the turn stalled. Called with mu held.
func (s *server) stalled(e event.TurnEnd) bool {
	isErr := e.Err != "" || e.Reason != "" && e.Reason != "done" && e.Reason != "interrupted"
	said := e.Err
	if said == "" {
		said = e.Text
	}
	text := strings.ToLower(said)
	switch {
	case isErr && (s.limited != nil || strings.Contains(text, "usage limit") || strings.Contains(text, "limit reached")):
		l := &Limit{}
		if s.limited != nil {
			l.Window, l.ResetsAt = s.limited.Window, s.limited.ResetsAt
		}
		switch s.cfg.LimitMode {
		case "auto":
			l.Continue = true
		case "off":
		default:
			l.Ask = true
		}
		if s.info.Limit != nil && !s.info.Limit.Ask {
			l.Continue, l.Ask = s.info.Limit.Continue, false // you already chose
		}
		s.info.Limit = l
		s.info.State = "idle"
		s.info.Detail = "usage limit reached"
		s.scheduleContinue()
		return true
	case !isErr && !strings.HasPrefix(said, "API Error:"):
		return false
	case isAuthError(text):
		s.info.State, s.info.Error = "idle", "log in to continue: "+firstLine(said)
		return true
	case strings.Contains(text, "too long") || strings.Contains(text, "too large"):
		s.info.State, s.info.Error = "idle", firstLine(said)+" · /compact may help"
		return true
	case IsOffline(text):
		s.retry(firstLine(said), true)
		return true
	case IsRetryable(text):
		s.retry(firstLine(said), false)
		return true
	}
	return false
}

func isAuthError(t string) bool {
	for _, k := range []string{"401", "authentication", "log in", "login", "oauth", "token has expired", "expired token", "apikeyhelper", "invalid api key"} {
		if strings.Contains(t, k) {
			return true
		}
	}
	return false
}

// IsRetryable is an API error, lowercased, that trying again gets past: a
// connection that dropped or stalled, the API overloaded or failing.
func IsRetryable(t string) bool {
	for _, k := range []string{"529", "overloaded", "api error: 5", "internal server error", "temporarily", "service unavailable", "timed out", "connection", "mid-response", "mid-stream", "stopped arriving"} {
		if strings.Contains(t, k) {
			return true
		}
	}
	return false
}

// IsOffline is an API error, lowercased, that says the network is down (or
// the machine slept), rather than that the API failed.
func IsOffline(t string) bool {
	for _, k := range []string{"can't reach the api", "unable to connect to api", "no response from api", "went to sleep", "internet", "enotfound", "eai_again", "econnrefused", "enetunreach", "ehostunreach", "enetdown"} {
		if strings.Contains(t, k) {
			return true
		}
	}
	return false
}

// retry handles a turn an API error stopped. While its prompt cache
// stays warm, a try costs little: it tries again 15s later, then doubling
// (once the API can be reached, if the network is down). A try after the
// cache expires re-reads the whole conversation at full price, so then it
// waits with every other session for proof the connection holds (see
// netproof), however long that takes. It gives up after RetryMax tries; a
// message from you then retries. Called with mu held.
func (s *server) retry(reason string, offline bool) {
	base, most := time.Duration(s.cfg.RetryBase), s.cfg.RetryMax
	if base <= 0 {
		base = 15 * time.Second
	}
	if most <= 0 {
		most = 8
	}
	r := s.info.Retry
	if r == nil || r.GaveUp {
		r = &Retry{Max: most}
	}
	now := time.Now()
	r.Reason, r.Attempt, r.Offline, r.Proof, r.Next = reason, r.Attempt+1, false, false, time.Time{}
	s.info.Retry, s.info.State = r, "idle"
	target := s.target()
	go netproofFail(target, now)
	if r.Attempt > most {
		r.GaveUp, r.Why = true, fmt.Sprintf("%d retries used", most)
		return
	}
	wait := base << (r.Attempt - 1)
	if !s.warmAt(now.Add(wait)) {
		r.Offline, r.Proof = offline, true
		s.pollOnline(r)
		return
	}
	if offline {
		r.Offline = true
		s.pollOnline(r)
		return
	}
	r.Next = now.Add(wait)
	s.after(wait, func() { _ = s.sendLocked("continue") })
}

// warmAt is whether the prompt cache is still warm at t. Not knowing
// counts as cold. Called with mu held.
func (s *server) warmAt(t time.Time) bool {
	return !s.info.CacheWarm.IsZero() && t.Before(s.info.CacheWarm)
}

// target is the API this session's requests go to.
func (s *server) target() string { return netproof.Target(s.cfg.Env...) }

// scheduleContinue arms the continue at a usage limit's reset, a few
// seconds apart per session so they don't all hit the fresh limit at once.
func (s *server) scheduleContinue() {
	l := s.info.Limit
	if l == nil || !l.Continue {
		return
	}
	if l.ResetsAt.IsZero() {
		// Nothing says when it resets, so nothing to wait for: your next
		// message tries again.
		l.Continue, l.Ask = false, false
		return
	}
	s.after(time.Until(l.ResetsAt)+s.jitter(), func() {
		s.info.Limit = nil
		s.resume()
	})
}

// jitter is this session's few seconds' wait after something every session
// waits on at once (a limit resetting, the network coming back), so they
// don't all send in the same moment.
func (s *server) jitter() time.Duration {
	return time.Duration(len(s.cfg.ID)*7+int(s.cfg.ID[0])) % 20 * time.Second
}

// resume carries on a stalled turn: with the queue, if there is one.
// Called with mu held.
func (s *server) resume() {
	if len(s.info.Queue) > 0 && !s.info.QueueHeld {
		s.sendQueue()
		return
	}
	_ = s.sendLocked("continue")
}

// onlineEvery is how often a session an API error stopped looks at
// whether it may try again.
var onlineEvery = 5 * time.Second

// mayGo and netproofFail are netproof's, replaced in tests.
var (
	mayGo        = netproof.MayGo
	netproofFail = netproof.Fail
)

// pollOnline looks every few seconds at whether the session may try
// again: warm, once the API can be reached; cold, on proof it holds, and
// first only if it's the one going first. Then it continues, a few seconds
// apart from the others. The look runs without mu held: a check can take
// seconds. Called with mu held.
func (s *server) pollOnline(r *Retry) {
	s.after(onlineEvery, func() {
		g, target, id, warm := s.gen, s.target(), s.cfg.ID, s.warmAt(time.Now())
		go func() {
			ok := mayGo(target, id, warm)
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.gen != g || s.info.Retry != r {
				return // you sent something meanwhile
			}
			if !ok {
				if r.Offline && !r.Proof {
					r.Proof = !s.warmAt(time.Now()) // the cache ran out while it waited
				}
				s.pollOnline(r)
				return
			}
			wait := s.jitter()
			r.Offline, r.Proof, r.Next = false, false, time.Now().Add(wait)
			s.publish()
			s.after(wait, s.resume)
		}()
	})
}

// after runs f with mu held once d has passed, replacing anything already
// scheduled.
func (s *server) after(d time.Duration, f func()) {
	if s.wake != nil {
		s.wake.Stop()
	}
	s.gen++
	g := s.gen
	s.wake = time.AfterFunc(max(0, d), func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.gen == g { // nothing was sent since it was set
			f()
		}
	})
}

// answered forgets a permission request and tells clients it is settled.
func (s *server) answered(id string) {
	if _, ok := s.pending[id]; !ok {
		return
	}
	delete(s.pending, id)
	b, _ := jsonx.Marshal(map[string]string{"type": typeAnswered, "request_id": id})
	s.record(b)
	if len(s.pending) == 0 && s.info.State == "blocked" {
		s.info.State = "working"
		s.info.Needs = ""
	}
}

// stillWorking is whether an idle agent has work of its own going: tasks
// in the background, or a question you asked it (a side question takes a
// model call). The context reading asked at each turn's end comes back
// well inside the rest, so it isn't waited on. Called with mu held.
func (s *server) stillWorking() bool {
	return len(s.info.Background) > 0 || s.asking > 0
}

// armIdle rests the agent once it has been idle for IdleStop. Called with
// mu held.
func (s *server) armIdle() {
	if s.idle != nil {
		s.idle.Stop()
	}
	conn := s.conn
	s.idle = time.AfterFunc(time.Duration(s.cfg.IdleStop), func() {
		// Work it left running in the background (a test run, a build, a
		// subagent, a monitor) would be cut off, and never reported back,
		// as would a question it's still answering: rest once it's done.
		s.mu.Lock()
		busy := s.stillWorking()
		s.mu.Unlock()
		if conn != nil && (busy || runsShells(pidOf(conn))) {
			s.mu.Lock()
			if s.conn == conn && s.info.State == "idle" {
				s.armIdle()
			}
			s.mu.Unlock()
			return
		}
		s.mu.Lock()
		stop := conn != nil && s.conn == conn && s.info.State == "idle"
		if stop {
			s.detach()
			s.publish()
		}
		s.mu.Unlock()
		if stop {
			stopAgent(conn)
			// Idle until the next message: give back what the turn used.
			debug.FreeOSMemory()
		}
	})
}

// relogin follows the profile being signed in as another account. A
// running agent keeps the account it started with, so it rests at its
// first safe point and your next message, or what's queued, starts it on
// the new one: an idle one now, one in a turn once the turn ends, one with
// work of its own still running (a build, a subagent, a question) once
// that's done. What's queued isn't held for that work: it goes to the old
// one. One a usage limit stopped carries on now instead of waiting for the
// reset. Called with mu held; returns with it released.
func (s *server) relogin(conn agent.Conn) {
	defer s.mu.Unlock()
	limited := s.info.Limit != nil
	if conn != nil && !limited {
		s.info.Relogin = true
		switch {
		case s.info.State != "idle":
			// onTurnEnd comes back here.
		case !s.quietWait:
			// Whether it has shells running is read from the process
			// table, which isn't done holding mu.
			s.quietWait = true
			go s.reloginWhenQuiet(conn)
		}
		s.publish()
		return
	}
	if s.info.State != "idle" || (conn == nil && !limited) {
		return
	}
	if conn != nil {
		s.detach()
		s.publish()
		s.retire(conn)
	}
	s.info.Limit = nil
	if len(s.info.Queue) > 0 && !s.info.QueueHeld {
		s.sendQueue()
	} else {
		_ = s.sendLocked("continue")
	}
}

// reloginWhenQuiet rests an idle agent due to move to another account once
// it has no work of its own running, looking again every quietCheck. What's
// queued isn't held for that work: it goes to the old one. Called without
// mu.
func (s *server) reloginWhenQuiet(conn agent.Conn) {
	s.mu.Lock()
	busy := s.stillWorking()
	s.mu.Unlock()
	busy = busy || runsShells(pidOf(conn))
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != conn || !s.info.Relogin || s.info.State != "idle" {
		s.quietWait = false // gone, or in a turn: its end comes back to relogin
		return
	}
	if busy {
		if len(s.info.Queue) > 0 && !s.info.QueueHeld {
			s.quietWait = false
			s.sendQueue()
			return
		}
		time.AfterFunc(quietCheck, func() { s.reloginWhenQuiet(conn) })
		return
	}
	s.quietWait = false
	s.detach()
	s.retire(conn)
	if len(s.info.Queue) > 0 && !s.info.QueueHeld {
		s.sendQueue()
		return
	}
	s.publish()
}

// quietCheck is how often an idle agent waiting to start again on another
// account is looked at for work of its own still running.
const quietCheck = 2 * time.Second

// publish writes info.json and sends the new info to clients. Called with
// mu held.
func (s *server) publish() {
	s.info.UpdatedAt = time.Now()
	b, _ := jsonx.Marshal(s.info)
	tmp := filepath.Join(dir(s.cfg.ID), "info.json.tmp")
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, filepath.Join(dir(s.cfg.ID), "info.json"))
	}
	// The line clients get wraps the same info, marshalled once: as a map
	// it would read {"info":…,"type":"agtop_info"}.
	line := make([]byte, 0, len(b)+32)
	line = append(append(append(line, `{"info":`...), b...), `,"type":"`+typeInfo+`"}`...)
	for c := range s.clients {
		c.push(line)
	}
}

// send delivers a message, or queues it while the agent is busy. Images
// always go now: a queued message is text only.
func (s *server) send(text string, images []string, now bool) error {
	// Images are looked at before taking the lock: they can be megabytes.
	var pics []string
	for _, p := range images {
		pic, err := readImage(p, TempDir(s.cfg.ID))
		if err != nil {
			return err
		}
		pics = append(pics, pic)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	waiting := s.info.Limit != nil && s.info.Limit.Continue && !s.info.Limit.ResetsAt.IsZero()
	busy := s.info.State == "working" || s.info.State == "blocked" || waiting
	if !now && busy && len(images) == 0 {
		s.info.Queue = append(s.info.Queue, text)
		s.publish()
		return nil
	}
	return s.deliver(text, images, pics)
}

// sendQueue sends what's queued: all of it as one message, or the first
// one if you asked for one per turn. If the send fails it goes back on the
// queue. Called with mu held.
func (s *server) sendQueue() {
	q := s.info.Queue
	next, rest := JoinQueue(q), []string(nil)
	if s.info.QueueSeparate {
		next, rest = q[0], q[1:]
	}
	s.info.Queue = rest
	if err := s.sendLocked(next); err != nil {
		s.info.Queue = q
		s.publish()
	}
}

// readImage checks a picture to attach, refusing what the API won't take,
// and is where it's read from: a PNG goes as a far smaller WebP, written
// to dir, when cwebp can make one.
func readImage(path, dir string) (string, error) {
	types := map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp"}
	mt := types[strings.ToLower(filepath.Ext(path))]
	if mt == "" {
		return "", fmt.Errorf("%s isn't a png, jpeg, gif or webp image", filepath.Base(path))
	}
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	size := st.Size()
	if mt == "image/png" {
		if w := toWebP(path); w != nil && int64(len(w)) < size && os.MkdirAll(dir, 0o700) == nil {
			out := filepath.Join(dir, strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))+"-"+hex.EncodeToString(randBytes(4))+".webp")
			if os.WriteFile(out, w, 0o600) == nil {
				path, size = out, int64(len(w))
			}
		}
	}
	if size > 5<<20 {
		return "", fmt.Errorf("%s is %d MB; images must be under 5 MB", filepath.Base(path), size>>20)
	}
	return path, nil
}

func randBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

// toWebP is a PNG, most often a screenshot, as a far smaller WebP, or nil
// when cwebp isn't installed or fails.
func toWebP(path string) []byte {
	cwebp, err := exec.LookPath("cwebp")
	if err != nil {
		return nil
	}
	b, err := exec.Command(cwebp, "-quiet", "-q", "90", path, "-o", "-").Output()
	if err != nil || len(b) == 0 {
		return nil
	}
	return b
}

// sendLocked gives the agent a message now; mid-turn it is picked up at
// the next step. Called with mu held.
func (s *server) sendLocked(text string) error { return s.deliver(text, nil, nil) }

// deliver is sendLocked with images: images as you attached them, pics
// where they're read from. Called with mu held.
func (s *server) deliver(text string, images, pics []string) error {
	s.gen++ // any continue or retry waiting is now moot
	s.info.Limit = nil
	if s.idle != nil {
		s.idle.Stop()
	}
	if s.wake != nil {
		s.wake.Stop()
	}
	s.info.Error = ""
	if s.info.Retry != nil && s.info.Retry.GaveUp {
		s.info.Retry = nil // your message is the retry
	}
	if err := s.start(); err != nil {
		s.info.Error = err.Error()
		s.publish()
		return err
	}
	// Echo it so every client shows the message before Claude answers.
	echo := map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": text}, "agtop_sent": true}
	if len(images) > 0 {
		var names []string
		for _, p := range images {
			names = append(names, filepath.Base(p))
		}
		echo["agtop_images"] = names
	}
	b, _ := jsonx.Marshal(echo)
	s.record(b)
	s.info.State = "working"
	s.info.Detail = ""
	s.publish()
	return s.conn.Send(agent.Input{Text: text, Images: pics})
}

// askContext asks the agent what fills the context window, unless it's
// asleep, can't say, or was already asked; clients get the answer as a
// typeContext line. Called with mu held.
func (s *server) askContext() {
	cr, ok := s.conn.(agent.ContextReader)
	if !ok || s.ctxOut {
		return
	}
	s.ctxOut = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		u, err := cr.ContextUsage(ctx)
		s.mu.Lock()
		defer s.mu.Unlock()
		s.ctxOut = false
		if err != nil {
			return
		}
		s.context, _ = jsonx.Marshal(map[string]any{"type": typeContext, "context": u})
		for c := range s.clients {
			c.push(s.context)
		}
	}()
}

// editQueue applies a queue op. Called with mu held.
func (s *server) editQueue(o op) error {
	q := s.info.Queue
	if o.Was != "" && (o.Index >= len(q) || o.Index < 0 || q[o.Index] != o.Was) {
		// The queue moved under you (it sent, or another client edited
		// it): find the message you meant, by its text.
		o.Index = slices.Index(q, o.Was)
		if o.Index < 0 {
			return errors.New("that message has already been sent")
		}
	}
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
		s.info.Queue = slices.Delete(slices.Clone(q), o.Index, o.Index+1)
		if err := s.sendLocked(text); err != nil {
			s.info.Queue = q
			s.publish()
			return err
		}
		return nil
	}
	s.info.Queue = q
	s.publish()
	return nil
}

// op is one command from a client.
type op struct {
	Op        string         `json:"op"`
	Text      string         `json:"text,omitempty"`
	ID        string         `json:"id,omitempty"`
	Always    bool           `json:"always,omitzero"`
	Input     jsontext.Value `json:"input,omitzero"`
	Message   string         `json:"message,omitempty"`
	Interrupt bool           `json:"interrupt,omitzero"`
	Mode      string         `json:"mode,omitempty"`
	Model     string         `json:"model,omitempty"`
	Effort    string         `json:"effort,omitempty"`
	Now       bool           `json:"now,omitzero"`
	Images    []string       `json:"images,omitempty"`
	Index     int            `json:"index,omitzero"`
	Was       string         `json:"was,omitempty"` // the queued text the client saw at Index
	To        int            `json:"to,omitzero"`
	Branch    *Branch        `json:"branch,omitempty"` // what rewind leaves
	Request   jsontext.Value `json:"request,omitzero"` // ask: the control request
	Proto     int            `json:"proto,omitzero"`   // hello: the client's protocol
}

func (s *server) do(o op) error {
	if o.Op == "send" {
		return s.send(o.Text, o.Images, o.Now)
	}
	s.mu.Lock()
	conn := s.conn
	switch o.Op {
	case "ask":
		// Asleep, it wakes to answer, and rests again once idle.
		if err := s.start(); err != nil {
			s.mu.Unlock()
			return err
		}
		a, ok := s.conn.(agent.Asker)
		if !ok {
			s.mu.Unlock()
			return fmt.Errorf("%s isn't something this agent can do", o.Op)
		}
		if s.info.State == "idle" {
			s.armIdle()
		}
		s.asking++
		s.mu.Unlock()
		go s.ask(a, &o)
		return nil
	case "context":
		s.askContext()
		s.mu.Unlock()
		return nil
	case "limit":
		if l := s.info.Limit; l != nil {
			l.Continue, l.Ask = o.Now, false
			if l.Continue {
				s.scheduleContinue()
			} else if s.wake != nil {
				s.wake.Stop()
			}
			s.publish()
		}
		s.mu.Unlock()
		return nil
	case "relogin":
		s.relogin(conn)
		return nil
	case "queue_hold", "queue_separate":
		if o.Op == "queue_hold" {
			s.info.QueueHeld = o.Now
		} else {
			s.info.QueueSeparate = o.Now
		}
		s.publish()
		s.mu.Unlock()
		return nil
	case "queue_edit", "queue_remove", "queue_move", "queue_merge", "queue_send":
		err := s.editQueue(o)
		s.mu.Unlock()
		return err
	case "allow", "deny":
		req, ok := s.pending[o.ID]
		if !ok || conn == nil {
			s.mu.Unlock()
			return fmt.Errorf("no pending request %s", o.ID)
		}
		s.answered(o.ID)
		s.publish()
		s.mu.Unlock()
		return s.answerAgent(conn, o.ID, req, &o)
	case "mode":
		s.cfg.PermissionMode = o.Mode
		s.info.PermissionMode = o.Mode
		s.publish()
	case "model":
		s.cfg.Model = o.Model
		if o.Model != "" {
			s.info.Model = o.Model
			s.publish()
		}
	case "effort":
		// Effort is fixed for an agent's process, so it takes hold the next
		// time one starts: right away when idle, else after this turn.
		s.cfg.Effort = o.Effort
		s.info.Effort = o.Effort
		s.publish()
		if conn != nil && s.info.State == "idle" {
			s.detach()
			s.publish()
			s.mu.Unlock()
			stopAgent(conn)
			return nil
		}
	case "rewind":
		if !agent.Supports(agent.Kind(s.cfg.Kind), agent.FeatureRewind) {
			s.mu.Unlock()
			return fmt.Errorf("%s isn't something this agent can do", o.Op)
		}
		err := s.rewind(o.Text, o.Now, o.Branch)
		s.mu.Unlock()
		if err == nil && conn != nil {
			stopAgent(conn)
		}
		return err
	case "stop":
		s.info.State = "stopped"
		s.detach()
		s.publish()
		s.mu.Unlock()
		if conn != nil {
			stopAgent(conn)
		}
		s.stopOnce.Do(func() { close(s.quit) })
		return nil
	}
	s.mu.Unlock()
	if conn == nil {
		return nil // applied on the next start
	}
	switch o.Op {
	case "interrupt":
		return conn.Interrupt()
	case "mode":
		return conn.SetMode(o.Mode)
	case "model":
		return conn.SetModel(o.Model)
	case "stop_task":
		if t, ok := conn.(agent.TaskStopper); ok {
			return t.StopTask(o.ID)
		}
		return fmt.Errorf("%s isn't something this agent can do", o.Op)
	case "background":
		if b, ok := conn.(agent.Backgrounder); ok {
			return b.Background(o.ID)
		}
		return fmt.Errorf("%s isn't something this agent can do", o.Op)
	}
	return nil
}

// ask passes a client's control request through and sends every client
// the reply, tagged with the client's id for it.
func (s *server) ask(a agent.Asker, o *op) {
	body, err := a.Ask(context.Background(), o.Request)
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	line, _ := jsonx.Marshal(map[string]any{"type": typeReply, "id": o.ID, "reply": body, "error": msg})
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asking--
	for c := range s.clients {
		c.push(line)
	}
}

// rewind carries on from sessionID instead: a copy of the conversation
// cut before one of your messages (resume), a new one when it was cut
// before the first, or a branch an earlier rewind left. The path it
// leaves becomes a branch, described by left. Clients are let go, to
// reconnect and draw it afresh. Called with mu held; the caller stops the
// old process.
func (s *server) rewind(sessionID string, resume bool, left *Branch) error {
	switch {
	case sessionID == "":
		return errors.New("no conversation to rewind to")
	case s.info.State == "working" || s.info.State == "blocked" || s.info.State == "starting":
		return errors.New("it's working: stop it (esc) or let the turn end, then rewind")
	}
	s.detach()
	s.cfg.rewindTo(sessionID, resume, left, s.began)
	s.began = resume
	s.saveConfig()
	s.ring, s.ringN, s.stamped = nil, 0, time.Time{}
	s.info.SessionID, s.info.RewoundAt, s.info.ReplayFrom = sessionID, time.Now(), time.Time{}
	s.info.Error, s.info.Retry, s.info.Needs = "", nil, ""
	if s.info.State != "stopped" {
		s.info.State = "idle"
	}
	s.publish()
	for c := range s.clients {
		c.close()
	}
	return nil
}

// rewindTo points the config at sessionID, keeping the conversation it
// leaves as a branch (when it has one, began) described by left.
func (cfg *Config) rewindTo(sessionID string, resume bool, left *Branch, began bool) {
	cfg.Branches = slices.DeleteFunc(cfg.Branches, func(b Branch) bool {
		return b.SessionID == sessionID || b.SessionID == cfg.SessionID
	})
	if left != nil && began && cfg.SessionID != "" && !cfg.Fork {
		b := *left
		b.SessionID, b.Left = cfg.SessionID, time.Now()
		cfg.Branches = append(cfg.Branches, b)
		if n := len(cfg.Branches); n > maxBranches {
			cfg.Branches = cfg.Branches[n-maxBranches:]
		}
	}
	cfg.SessionID, cfg.Resume, cfg.Fork, cfg.From, cfg.Prompt, cfg.Images = sessionID, resume, false, "", "", nil
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
	proto, ops := hello(nc)
	c := &conn{c: nc, out: make(chan []byte, 4096), gone: make(chan struct{})}
	var enc *encoder
	if proto >= eventsFrom {
		enc = &encoder{}
	}
	s.mu.Lock()
	replay := append([][]byte(nil), s.ring...)
	if s.commands != nil {
		replay = append(replay, s.commands)
	}
	if s.context != nil {
		replay = append(replay, s.context)
	}
	info, _ := jsonx.Marshal(map[string]any{"type": typeInfo, "info": s.info})
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
			if enc != nil {
				return enc.encode(w, l) == nil
			}
			_, err := w.Write(append(l, '\n'))
			return err == nil
		}
		var u unpacker
		for _, l := range replay {
			var err error
			if enc != nil {
				err = enc.replay(w, &u, l)
			} else if err = u.writeTo(w, l); err == nil {
				err = w.WriteByte('\n')
			}
			if err != nil {
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

	sc := bufio.NewScanner(ops)
	sc.Buffer(make([]byte, 0, 64<<10), 16<<20)
	for sc.Scan() {
		var o op
		if jsonx.Unmarshal(sc.Bytes(), &o) != nil {
			continue
		}
		if err := s.do(o); err != nil {
			b, _ := jsonx.Marshal(map[string]string{"type": "agtop_error", "error": err.Error()})
			c.push(b)
		}
		if o.Op == "stop" {
			return
		}
	}
}

// toolSummary picks the argument that says what a tool call does.
func toolSummary(input jsontext.Value) string {
	var m map[string]any
	if jsonx.Unmarshal(input, &m) != nil {
		return ""
	}
	if qs, ok := m["questions"].([]any); ok && len(qs) > 0 {
		if q, ok := qs[0].(map[string]any); ok {
			if t, ok := q["question"].(string); ok {
				return firstLine(t)
			}
		}
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
	// Counted in place rather than as runes, and copied: it's kept, and
	// should hold only itself, not the whole message it came from.
	n := 0
	for i := range s {
		if n == 200 {
			return s[:i] + "…"
		}
		n++
	}
	return strings.Clone(s)
}

// alive reports whether pid is a running process.
// Alive is whether a session's host process (Info.HostPID) is running. A
// host publishes "stopped" just before it exits, so the state alone can say
// stopped while the process is still there.
func Alive(pid int) bool { return alive(pid) }

func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// runsShells reports whether Claude Code (pid) has a Bash-tool shell still
// running under it, which is how its background work runs.
func runsShells(pid int) bool {
	if pid <= 0 {
		return false // not a process of its own
	}
	tab := proc.Snapshot(nil)
	for _, c := range tab.Descendants(pid) {
		if c != pid && strings.Contains(proc.CommandLine(c), "shell-snapshots") {
			return true
		}
	}
	return false
}
