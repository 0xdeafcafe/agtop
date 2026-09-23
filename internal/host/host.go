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
	"slices"
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
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	Resume    bool   `json:"resume"` // the conversation already exists
	// Fork continues a copy of the conversation (Claude Code's
	// --fork-session), leaving the original to whoever has it open. Once
	// Claude Code names the copy, SessionID becomes that and Fork is cleared.
	Fork bool `json:"fork,omitempty"`
	// From is the conversation this one continues, for showing its history
	// (a fork's own transcript may start empty).
	From           string         `json:"from,omitempty"`
	Account        claude.Account `json:"account"`
	Cwd            string         `json:"cwd"`
	Name           string         `json:"name,omitempty"`
	Model          string         `json:"model,omitempty"`
	Effort         string         `json:"effort,omitempty"`
	PermissionMode string         `json:"permissionMode,omitempty"`
	Flags          []string       `json:"flags,omitempty"`
	Prompt         string         `json:"prompt,omitempty"` // first message
	Images         []string       `json:"images,omitempty"` // files attached to it
	IdleStop       Duration       `json:"idleStop,omitempty"`
	// LimitMode is what happens when a usage limit stops the session:
	// "auto" continues at the reset, "off" waits for you, and "" (opt-in)
	// asks once per session.
	LimitMode string `json:"limitMode,omitempty"`
	// RetryBase is the first wait before retrying an API error; each retry
	// doubles it. RetryMax caps the attempts. Zero means the defaults.
	RetryBase Duration `json:"retryBase,omitempty"`
	RetryMax  int      `json:"retryMax,omitempty"`
	Binary    string   `json:"binary,omitempty"`
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
	Effort         string  `json:"effort,omitempty"`
	PermissionMode string  `json:"permissionMode,omitempty"`
	CostUSD        float64 `json:"costUsd,omitempty"`
	// Queue holds messages sent while the agent was busy; the host sends
	// the first when the turn ends.
	Queue []string `json:"queue,omitempty"`
	// QueueHeld pauses sending the queue; QueueSeparate sends one queued
	// message per turn instead of the whole queue as one.
	QueueHeld     bool `json:"queueHeld,omitempty"`
	QueueSeparate bool `json:"queueSeparate,omitempty"`
	// Limit is set while a usage limit has stopped the session.
	Limit *Limit `json:"limit,omitempty"`
	// Retry is set while an API error is being retried, or has given up.
	Retry *Retry `json:"retry,omitempty"`
	// CacheWarm is when the prompt cache written by the last request
	// expires; a request after it re-reads the whole context.
	CacheWarm time.Time `json:"cacheWarm,omitempty"`
	Error     string    `json:"error,omitempty"`
	StartedAt time.Time `json:"startedAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Limit describes a usage limit that stopped the session.
type Limit struct {
	ResetsAt time.Time `json:"resetsAt,omitempty"`
	Window   string    `json:"window,omitempty"` // five_hour, seven_day, …
	// Continue is whether the session carries on at the reset; Ask is set
	// when that's still yours to decide.
	Continue bool `json:"continue"`
	Ask      bool `json:"ask,omitempty"`
}

// Retry describes an API error being retried.
type Retry struct {
	Reason  string    `json:"reason"`
	Attempt int       `json:"attempt"`
	Max     int       `json:"max"`
	Next    time.Time `json:"next,omitempty"`
	// GaveUp is set when retries ran out or the next would land after the
	// cache expired; a message from you retries.
	GaveUp bool   `json:"gaveUp,omitempty"`
	Why    string `json:"why,omitempty"`
}

// cacheLife is how long the prompt cache lasts. Claude Code writes the
// one-hour cache (usage reports ephemeral_1h_input_tokens).
const cacheLife = time.Hour

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
	typeTime     = "agtop_time"
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
	stamped  time.Time // when the last time mark went into the ring
	limitRaw headless.RateLimit
	wake     *time.Timer // a scheduled continue or retry
	gen      int         // bumped by every send; a stale timer does nothing
	idle     *time.Timer
	quit     chan struct{}
	stopOnce sync.Once
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
			HostPID: os.Getpid(), State: "idle", Model: cfg.Model, Effort: cfg.Effort, PermissionMode: cfg.PermissionMode,
			StartedAt: now, UpdatedAt: now},
	}
	s.publish()
	if cfg.Prompt != "" || len(cfg.Images) > 0 {
		if err := s.send(cfg.Prompt, cfg.Images, false); err != nil {
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
		Account: s.cfg.Account, Dir: s.cfg.Cwd, Model: s.cfg.Model, Effort: s.cfg.Effort,
		PermissionMode: s.cfg.PermissionMode, Flags: s.cfg.Flags, Binary: s.cfg.Binary,
		Tap: s.tap,
	}
	if s.began {
		o.Resume = s.cfg.SessionID
		if s.cfg.Fork {
			o.Flags = append(append([]string{}, o.Flags...), "--fork-session")
		}
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

// detach forgets the running process so nothing more is sent to it, and
// returns it for stopping outside the lock. Called with mu held.
func (s *server) detach() *headless.Session {
	sess := s.sess
	s.sess = nil
	s.info.ClaudePID = 0
	s.pending = map[string]headless.PermissionRequest{}
	return sess
}

// saveConfig writes the config back, for what changes while running.
// Called with mu held.
func (s *server) saveConfig() {
	b, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return
	}
	tmp := filepath.Join(dir(s.cfg.ID), "config.json.tmp")
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, filepath.Join(dir(s.cfg.ID), "config.json"))
	}
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
	// A time mark before output that follows a pause, so a client replaying
	// the ring knows when things happened, not just in what order.
	if now := time.Now(); now.Sub(s.stamped) >= 500*time.Millisecond {
		s.stamped = now
		b, _ := json.Marshal(map[string]any{"type": typeTime, "t": now.UnixMilli()})
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
		if s.cfg.Fork && ev.SessionID != "" && ev.SessionID != s.cfg.SessionID {
			// The copy has its own id now; later restarts resume that.
			s.cfg.SessionID, s.cfg.Fork = ev.SessionID, false
			s.saveConfig()
		}
	case headless.RateLimit:
		s.limitRaw = ev
		return
	case headless.Message:
		if ev.Role == "assistant" && ev.Usage != nil {
			s.info.CacheWarm = time.Now().Add(cacheLife)
		}
		if ev.Role == "assistant" && s.info.State == "idle" {
			// It picked up on its own (a background task finished).
			s.info.State = "working"
			if s.idle != nil {
				s.idle.Stop()
			}
		}
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
		s.info.Needs = needs(ev)
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
		if s.stalled(ev) {
			s.publish()
			return
		}
		s.info.Retry = nil
		s.info.Limit = nil
		s.limitRaw = headless.RateLimit{}
		if len(s.pending) == 0 {
			s.info.State = "idle"
			s.info.Needs = ""
			if t := strings.TrimSpace(ev.Text); t != "" {
				s.info.Detail = firstLine(t)
			}
			if len(s.info.Queue) > 0 && !s.info.QueueHeld {
				// The whole queue goes as one message, unless you asked
				// for them one per turn.
				s.sendQueue()
				return
			}
			s.armIdle()
		}
	default:
		return
	}
	s.publish()
}

// stalled handles a turn that ended on a usage limit or an API error,
// scheduling a continue or a retry when that's allowed. It reports whether
// the turn stalled. Called with mu held.
func (s *server) stalled(r headless.Result) bool {
	text := strings.ToLower(r.Text)
	switch {
	case r.IsError && (s.limitRaw.Status == "rejected" || strings.Contains(text, "usage limit") || strings.Contains(text, "limit reached")):
		l := &Limit{Window: limitWindow(s.limitRaw.Raw)}
		l.ResetsAt = limitReset(s.limitRaw.Raw)
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
	case !r.IsError:
		return false
	case isAuthError(text):
		s.info.State, s.info.Error = "idle", "log in to continue: "+firstLine(r.Text)
		return true
	case strings.Contains(text, "too long") || strings.Contains(text, "too large"):
		s.info.State, s.info.Error = "idle", firstLine(r.Text)+" · /compact may help"
		return true
	case isRetryable(text):
		s.retry(firstLine(r.Text))
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

func isRetryable(t string) bool {
	for _, k := range []string{"529", "overloaded", "api error: 5", "internal server error", "temporarily", "service unavailable", "timed out", "connection"} {
		if strings.Contains(t, k) {
			return true
		}
	}
	return false
}

func limitReset(raw json.RawMessage) time.Time {
	var r struct {
		ResetsAt int64 `json:"resetsAt"`
	}
	if json.Unmarshal(raw, &r) == nil && r.ResetsAt > 0 {
		return time.Unix(r.ResetsAt, 0)
	}
	return time.Time{}
}

func limitWindow(raw json.RawMessage) string {
	var r struct {
		Type string `json:"rateLimitType"`
	}
	_ = json.Unmarshal(raw, &r)
	return r.Type
}

// retry schedules the next attempt after an API error: 15s, then double
// each time. It gives up after RetryMax attempts, or when the next attempt
// would land after the prompt cache expired, since that attempt re-reads
// the whole context at full price; a message from you then retries.
func (s *server) retry(reason string) {
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
	r.Reason, r.Attempt = reason, r.Attempt+1
	wait := base << (r.Attempt - 1)
	r.Next = time.Now().Add(wait)
	switch {
	case r.Attempt > most:
		r.GaveUp, r.Why, r.Next = true, fmt.Sprintf("%d retries used", most), time.Time{}
	case !s.info.CacheWarm.IsZero() && r.Next.After(s.info.CacheWarm):
		r.GaveUp, r.Why, r.Next = true, "the next try would come after the cache expires", time.Time{}
	}
	s.info.Retry = r
	s.info.State = "idle"
	if r.GaveUp {
		return
	}
	s.after(wait, func() { _ = s.sendLocked("continue") })
}

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
	jitter := time.Duration(len(s.cfg.ID)*7+int(s.cfg.ID[0])) % 20 * time.Second
	s.after(time.Until(l.ResetsAt)+jitter, func() {
		s.info.Limit = nil
		if len(s.info.Queue) > 0 && !s.info.QueueHeld {
			s.sendQueue()
			return
		}
		_ = s.sendLocked("continue")
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
		stop := sess != nil && s.sess == sess && s.info.State == "idle"
		if stop {
			s.detach()
			s.publish()
		}
		s.mu.Unlock()
		if stop {
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

// send delivers a message, or queues it while the agent is busy. Images
// always go now: a queued message is text only.
func (s *server) send(text string, images []string, now bool) error {
	// Images are read before taking the lock: they can be megabytes.
	var pics []headless.Image
	for _, p := range images {
		im, err := readImage(p)
		if err != nil {
			return err
		}
		pics = append(pics, im)
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
	next, rest := strings.Join(q, "\n\n"), []string(nil)
	if s.info.QueueSeparate {
		next, rest = q[0], q[1:]
	}
	s.info.Queue = rest
	if err := s.sendLocked(next); err != nil {
		s.info.Queue = q
		s.publish()
	}
}

// readImage loads a picture to attach, refusing what the API won't take.
func readImage(path string) (headless.Image, error) {
	types := map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp"}
	mt := types[strings.ToLower(filepath.Ext(path))]
	if mt == "" {
		return headless.Image{}, fmt.Errorf("%s isn't a png, jpeg, gif or webp image", filepath.Base(path))
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return headless.Image{}, err
	}
	if len(b) > 5<<20 {
		return headless.Image{}, fmt.Errorf("%s is %d MB; images must be under 5 MB", filepath.Base(path), len(b)>>20)
	}
	return headless.Image{MediaType: mt, Data: b}, nil
}

// sendLocked gives Claude Code a message now; mid-turn it is picked up at
// the next step. Called with mu held.
func (s *server) sendLocked(text string) error { return s.deliver(text, nil, nil) }

// deliver is sendLocked with images already read. Called with mu held.
func (s *server) deliver(text string, images []string, pics []headless.Image) error {
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
	b, _ := json.Marshal(echo)
	s.record(b)
	s.info.State = "working"
	s.info.Detail = ""
	s.publish()
	return s.sess.SendWith(text, pics)
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
	Op        string          `json:"op"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Always    bool            `json:"always,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Message   string          `json:"message,omitempty"`
	Interrupt bool            `json:"interrupt,omitempty"`
	Mode      string          `json:"mode,omitempty"`
	Model     string          `json:"model,omitempty"`
	Effort    string          `json:"effort,omitempty"`
	Now       bool            `json:"now,omitempty"`
	Images    []string        `json:"images,omitempty"`
	Index     int             `json:"index,omitempty"`
	Was       string          `json:"was,omitempty"` // the queued text the client saw at Index
	To        int             `json:"to,omitempty"`
}

func (s *server) do(o op) error {
	if o.Op == "send" {
		return s.send(o.Text, o.Images, o.Now)
	}
	s.mu.Lock()
	sess := s.sess
	switch o.Op {
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
	case "effort":
		// Effort is fixed for a Claude Code process, so it takes hold the
		// next time one starts: right away when idle, else after this turn.
		s.cfg.Effort = o.Effort
		s.info.Effort = o.Effort
		s.publish()
		if sess != nil && s.info.State == "idle" {
			s.detach()
			s.publish()
			s.mu.Unlock()
			return sess.Stop(10 * time.Second)
		}
	case "stop":
		s.info.State = "stopped"
		s.detach()
		s.publish()
		s.mu.Unlock()
		if sess != nil {
			_ = sess.Stop(10 * time.Second)
		}
		s.stopOnce.Do(func() { close(s.quit) })
		return nil
	case "interrupt":
		// Stopping a turn shouldn't start the next queued one by itself.
		if len(s.info.Queue) > 0 && !s.info.QueueHeld {
			s.info.QueueHeld = true
			s.publish()
		}
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
// needs says what a waiting request wants, in words for the list.
func needs(r headless.PermissionRequest) string {
	if r.Tool == "AskUserQuestion" {
		var in struct {
			Questions []struct {
				Question string `json:"question"`
			} `json:"questions"`
		}
		_ = json.Unmarshal(r.Input, &in)
		if len(in.Questions) > 0 {
			return "asks: " + firstLine(in.Questions[0].Question)
		}
		return "has a question"
	}
	return r.Tool + " " + toolSummary(r.Input)
}

func toolSummary(input json.RawMessage) string {
	var m map[string]any
	if json.Unmarshal(input, &m) != nil {
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
