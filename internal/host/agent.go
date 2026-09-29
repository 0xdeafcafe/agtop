package host

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/agent/event"
	"github.com/0xdeafcafe/agtop/internal/agent/tool"
	"github.com/0xdeafcafe/agtop/internal/agent/usage"
	"github.com/0xdeafcafe/agtop/internal/agtools"
	"github.com/0xdeafcafe/agtop/internal/jsonx"
	"github.com/0xdeafcafe/agtop/internal/netproof"
	"github.com/0xdeafcafe/agtop/internal/plugin"
)

// typeEvent is a line carrying one of agtop's own events: every agent's
// but Claude Code's, whose own lines go to clients that don't read
// events (see hello.go).
const typeEvent = "agtop_ev"

// asked is an approval or a question the agent is waiting on.
type asked struct{ question bool }

// start launches the session's agent through its adapter, if it isn't
// running. Called with mu held.
func (s *server) start() error {
	if s.conn != nil {
		return nil
	}
	s.spent = 0 // a new process counts from zero
	a, ok := agent.Get(agent.KindOf(s.cfg.Kind))
	if !ok {
		return fmt.Errorf("agtop doesn't know the agent %q", s.cfg.Kind)
	}
	d, ok := a.(agent.Driver)
	if !ok {
		return fmt.Errorf("agtop can't run %s", a.Name())
	}
	tmp := TempDir(s.cfg.ID)
	o := agent.StartOptions{
		Profile: agent.Profile{Kind: a.Kind(), Name: s.cfg.Account.Name, Dir: s.cfg.Account.Dir},
		Dir:     s.cfg.Cwd, SessionID: s.cfg.SessionID, Resume: s.began && s.cfg.SessionID != "", Fork: s.began && s.cfg.Fork,
		Model: s.cfg.Model, Effort: s.cfg.Effort, Mode: s.cfg.PermissionMode,
		Env: append([]string{"TMPDIR=" + tmp}, s.cfg.Env...), Flags: s.cfg.Flags, Binary: s.cfg.Binary,
		TempDir: tmp, Lean: s.cfg.Lean, Tap: s.tap, Lightly: true,
		// agtop's own tools only draw, so they never ask.
		Tools: []agent.ToolServer{{Name: agtools.Server, Trusted: agtools.Names(), Handle: agtools.Handle}},
	}
	if o.Binary == "" {
		// Found where its installer put it, off PATH: agtop started from
		// the Dock has a thin one.
		o.Binary = agent.Path(a.Kind())
	}
	// Approved plugins add subagents and prompt text, and their tools, which
	// ask like any other.
	pc := plugin.ForSession()
	o.Agents, o.Prompt = pc.Agents, pc.Prompt
	for _, srv := range pc.Servers {
		name, ok := plugin.NameOf(srv)
		if !ok {
			continue
		}
		id := s.cfg.ID
		o.Tools = append(o.Tools, agent.ToolServer{Name: srv, Handle: func(msg jsontext.Value) jsontext.Value { return s.broker.MCP(name, id, msg) }})
	}
	if len(pc.Servers) > 0 {
		go func() { _ = plugin.EnsureBroker() }()
	}
	conn, err := d.Start(context.Background(), o)
	if err != nil {
		return err
	}
	lowGC.Do(func() {
		// Running turns, its heap is the ring and lines passing through:
		// collecting at a quarter over what's live rather than double keeps
		// a long session's high water down, for little CPU. Before the first
		// turn it would only cost more collections while starting up.
		if os.Getenv("GOGC") == "" {
			debug.SetGCPercent(25)
		}
	})
	s.conn, s.options = conn, map[string][]event.Option{}
	s.info.ClaudePID = pidOf(conn)
	s.info.Error = ""
	go s.watchAgent(conn)
	return nil
}

// pidOf is the agent's process, or 0 when it has none of its own.
func pidOf(c agent.Conn) int {
	if p, ok := c.(agent.PIDer); ok {
		return p.PID()
	}
	return 0
}

// taps is whether c's lines reach the ring through tap, rather than as
// its events.
func taps(c agent.Conn) bool {
	t, ok := c.(agent.Tapper)
	return ok && t.Taps()
}

// watchAgent follows one agent's session until it ends.
func (s *server) watchAgent(conn agent.Conn) {
	for ev := range conn.Events() {
		s.mu.Lock()
		s.onAgentEvent(conn, ev)
		s.mu.Unlock()
	}
	var err error
	if e, ok := conn.(agent.Ender); ok {
		err = e.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != conn {
		return
	}
	s.conn, s.info.ClaudePID, s.info.Background = nil, 0, nil // they went with it
	s.pending = map[string]asked{}
	if s.info.State == "working" || s.info.State == "blocked" || s.info.State == "starting" {
		// It died mid-turn; the next message resumes it.
		s.info.State = "idle"
		if err != nil {
			s.info.Error = err.Error()
		}
		// Whatever was waiting for this turn to end goes now, rather than
		// sitting in a queue nothing will ever drain.
		if len(s.info.Queue) > 0 && !s.info.QueueHeld && s.info.Limit == nil {
			s.sendQueue()
			return
		}
	}
	s.publish()
}

// onAgentEvent takes one event from the session. Called with mu held.
func (s *server) onAgentEvent(conn agent.Conn, ev event.Event) {
	if !taps(conn) {
		if b, err := eventLine(ev); err == nil {
			s.record(b)
		}
	}
	switch e := ev.(type) {
	case event.Init:
		if e.Model != "" {
			s.info.Model = e.Model
		}
		if e.Mode != "" {
			s.info.PermissionMode = e.Mode
		}
		if e.SessionID != "" {
			s.info.SessionID = e.SessionID
		}
		if e.SessionID != "" && e.SessionID != s.cfg.SessionID {
			// The agent names its own sessions, and a fork's copy has an id
			// of its own: later starts resume this one.
			s.cfg.SessionID, s.cfg.Fork = e.SessionID, false
			s.saveConfig()
		}
		s.began = true
	case event.Message:
		s.onMessage(conn, e)
	case event.Approval:
		s.options[e.ID] = e.Options
		s.pending[e.ID] = asked{}
		s.info.State, s.info.Needs = "blocked", needsCall(&e.Call)
	case event.Question:
		s.pending[e.ID] = asked{question: true}
		s.info.State, s.info.Needs = "blocked", needsQuestion(e)
	case event.ApprovalCancelled:
		s.answered(e.ID)
	case event.TaskStarted, event.TaskDone, event.Background:
		if !s.onTask(ev) {
			return
		}
	case event.Commands:
		list := make([]wireCommand, len(e.List))
		for i, c := range e.List {
			list[i] = wireCommand(c)
		}
		s.commands, _ = jsonx.Marshal(map[string]any{"type": typeCommands, "commands": list})
		for c := range s.clients {
			c.push(s.commands)
		}
		return
	case event.Limited:
		s.limited = &e
		return
	case event.Quota:
		if _, own := conn.(agent.QuotaKeeper); !own {
			// Every agtop shows it at once.
			p := agent.Profile{Kind: agent.Kind(s.cfg.Kind), Dir: s.cfg.Account.Dir}
			q := e.Quota
			go func() { _ = usage.Record(QuotasPath(), QuotaKey(p), q) }()
		}
		return
	case event.Context:
		s.info.ContextTokens = e.Tokens
		return
	case event.TurnEnd:
		s.onTurnEnd(conn, e)
		return
	default:
		return
	}
	s.publish()
}

// onTask keeps the tasks running beside the turn, and reports whether
// that changed what the list shows. Called with mu held.
func (s *server) onTask(ev event.Event) bool {
	switch e := ev.(type) {
	case event.TaskStarted:
		// Backgrounded later, it still started now.
		if s.taskStart == nil {
			s.taskStart = map[string]time.Time{}
		}
		s.taskStart[e.ID] = time.Now()
		for i, t := range s.info.Background {
			if t.ID == e.ID {
				s.info.Background[i].StartedAt = s.taskStart[e.ID]
			}
		}
	case event.TaskDone:
		delete(s.taskStart, e.ID)
	case event.Background:
		s.info.Background = background(s.info.Background, e.Tasks, s.taskStart, time.Now())
		if len(s.info.Background) == 0 && s.info.State == "idle" && s.conn != nil {
			s.armIdle() // the last of it ended: rest from now
		}
		return true
	}
	return false
}

// wireCommand is a slash command as the commands line has always had it.
type wireCommand struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	ArgumentHint string   `json:"argumentHint"`
	Aliases      []string `json:"aliases"`
}

// onMessage keeps what the list shows of a message: what it's doing, how
// full its context is, and whether its prompt cache is warm. Called with mu
// held.
func (s *server) onMessage(conn agent.Conn, m event.Message) {
	if m.Role != "assistant" {
		return
	}
	if m.Tokens != nil {
		s.info.CacheWarm = time.Now().Add(cacheLife)
		go netproof.Answer(s.target(), time.Now())
		if m.Parent == "" {
			t := m.Tokens
			s.info.ContextTokens = int(t.Input + t.CacheRead + t.CacheWrite5m + t.CacheWrite1h)
		}
	}
	if s.info.State == "idle" {
		// It picked up on its own (a background task finished).
		s.info.State = "working"
		if s.idle != nil {
			s.idle.Stop()
		}
	}
	if m.Parent != "" {
		return
	}
	for _, p := range m.Parts {
		switch {
		case p.Kind == event.ToolCall && p.Call != nil:
			s.info.Detail = agent.Doing(agent.KindOf(s.cfg.Kind), p.Call)
		case p.Kind == event.Text:
			if t := strings.TrimSpace(p.Text); t != "" {
				s.info.Detail = firstLine(t)
			}
		}
	}
	_ = conn
}

// onTurnEnd settles a turn: its cost, and whether it stalled on a limit or
// an error, else rest or the queue. Called with mu held.
func (s *server) onTurnEnd(conn agent.Conn, e event.TurnEnd) {
	s.began = true
	s.info.CostUSD += TurnCost(&s.spent, e.Cost)
	s.askContext()
	if s.stalled(e) {
		s.publish()
		return
	}
	s.info.Retry, s.info.Limit, s.limited = nil, nil, nil
	if len(s.pending) == 0 {
		s.info.State = "idle"
		s.info.Needs = ""
		if t := strings.TrimSpace(e.Text); t != "" {
			s.info.Detail = firstLine(t)
		}
		if len(s.info.Queue) > 0 && !s.info.QueueHeld {
			// The whole queue goes as one message, unless you asked for
			// them one per turn.
			s.sendQueue()
			return
		}
		s.armIdle()
		if st, ok := conn.(agent.Staler); ok && st.Stale() {
			// Signed in as another account since it started: it rests
			// now, rather than holding the old sign-in and writing it
			// back as it refreshes it.
			go func() {
				s.mu.Lock()
				s.relogin(s.conn)
			}()
		}
		// Waiting for you now: give back what the turn used.
		go debug.FreeOSMemory()
	}
	s.publish()
}

// eventLine is ev as a line to clients.
func eventLine(ev event.Event) ([]byte, error) {
	b, err := event.Marshal(ev)
	if err != nil {
		return nil, err
	}
	return append(append([]byte(`{"type":"`+typeEvent+`","ev":`), b...), '}'), nil
}

// isEventLine is a line eventLine wrote about one of kinds.
func isEventLine(l []byte, kinds ...string) bool {
	rest, ok := bytes.CutPrefix(l, []byte(`{"type":"`+typeEvent+`","ev":{"t":"`))
	if !ok {
		return false
	}
	for _, k := range kinds {
		if bytes.HasPrefix(rest, []byte(k+`"`)) {
			return true
		}
	}
	return false
}

// answerAgent answers an approval or a question the agent asked. The
// clients answer as they would Claude Code: allow, always or deny, with the
// call's input as changed (a question's answers in AskUserQuestion's
// input), and a refusal's message.
func (s *server) answerAgent(conn agent.Conn, id string, req asked, o *op) error {
	if r, ok := conn.(agent.Responder); ok {
		if o.Op == "allow" {
			return r.Allow(id, o.Input, o.Always)
		}
		return r.Deny(id, o.Message, o.Interrupt)
	}
	if req.question && o.Op == "allow" {
		a, ok := conn.(agent.Answerer)
		if !ok {
			return fmt.Errorf("this agent can't take answers")
		}
		var in struct {
			Answers map[string]string `json:"answers"`
		}
		_ = jsonx.Unmarshal(o.Input, &in)
		answers := map[string][]string{}
		for q, labels := range in.Answers {
			answers[q] = strings.Split(labels, ", ")
		}
		return a.AnswerQuestion(id, answers)
	}
	want := []event.OptionKind{event.AllowOnce}
	switch {
	case o.Op == "deny":
		want = []event.OptionKind{event.RejectOnce, event.RejectAlways}
	case o.Always:
		want = []event.OptionKind{event.AllowAlways, event.AllowOnce}
	}
	s.mu.Lock()
	opts := s.options[id]
	delete(s.options, id)
	s.mu.Unlock()
	for _, k := range want {
		for _, opt := range opts {
			if opt.Kind == k {
				return conn.Answer(id, opt.ID)
			}
		}
	}
	// Refused with nothing to refuse with: cancelled is a no.
	return conn.Answer(id, "")
}

// stopAgent ends the agent's session, waiting a moment for it to go.
func stopAgent(conn agent.Conn) {
	done := make(chan struct{})
	go func() { _ = conn.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
	}
}

// needsCall says what an approval wants, in words for the list: the tool,
// and what it works on.
func needsCall(c *tool.Call) string {
	in := c.Input
	for _, v := range []string{in.Command, in.Path, in.Pattern, in.URL, in.Query, in.Description, in.Prompt} {
		if v != "" {
			return c.Name + " " + firstLine(v)
		}
	}
	return c.Name + " " + toolSummary(c.Raw)
}

// needsQuestion says what a question asks, in words for the list.
func needsQuestion(q event.Question) string {
	if len(q.Asks) > 0 {
		return "asks: " + firstLine(q.Asks[0].Text)
	}
	return "has a question"
}
