package host

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/agent/event"
	"github.com/0xdeafcafe/agtop/internal/headless"
)

// typeEvent is a line carrying one of agtop's own events, as a session
// of an agent other than Claude Code sends them.
const typeEvent = "agtop_ev"

// other is whether the session runs an agent other than Claude Code,
// through its adapter rather than headless.
func (cfg Config) other() bool { return cfg.Kind != "" && cfg.Kind != "claude" }

// startAgent starts the session's agent through its adapter. Called with
// mu held.
func (s *server) startAgent() error {
	a, ok := agent.Get(agent.Kind(s.cfg.Kind))
	if !ok {
		return fmt.Errorf("agtop doesn't know the agent %q", s.cfg.Kind)
	}
	d, ok := a.(agent.Driver)
	if !ok {
		return fmt.Errorf("agtop can't run %s", a.Name())
	}
	o := agent.StartOptions{
		Profile: agent.Profile{Kind: a.Kind(), Name: s.cfg.Account.Name, Dir: s.cfg.Account.ConfigDir},
		Dir:     s.cfg.Cwd, SessionID: s.cfg.SessionID, Resume: s.began && s.cfg.SessionID != "", Fork: s.began && s.cfg.Fork,
		Model: s.cfg.Model, Effort: s.cfg.Effort, Mode: s.cfg.PermissionMode,
		Env: append([]string{"TMPDIR=" + TempDir(s.cfg.ID)}, s.cfg.Env...), Flags: s.cfg.Flags, Binary: s.cfg.Binary,
	}
	conn, err := d.Start(context.Background(), o)
	if err != nil {
		return err
	}
	s.conn, s.options = conn, map[string][]event.Option{}
	if p, ok := conn.(interface{ PID() int }); ok {
		s.info.ClaudePID = p.PID()
	}
	s.info.Error = ""
	go s.watchAgent(conn)
	return nil
}

// watchAgent follows one agent's session until it ends: every event goes
// to clients as it is, and into the host's own bookkeeping as Claude Code
// would have said it.
func (s *server) watchAgent(conn agent.Conn) {
	for ev := range conn.Events() {
		s.mu.Lock()
		s.onAgentEvent(ev)
		s.mu.Unlock()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != conn {
		return
	}
	s.conn, s.info.ClaudePID, s.info.Background = nil, 0, nil
	s.pending = map[string]headless.PermissionRequest{}
	if s.info.State == "working" || s.info.State == "blocked" || s.info.State == "starting" {
		s.info.State = "idle"
		if len(s.info.Queue) > 0 && !s.info.QueueHeld && s.info.Limit == nil {
			s.sendQueue()
			return
		}
	}
	s.publish()
}

// onAgentEvent takes one event from an agent's session. Called with mu
// held.
func (s *server) onAgentEvent(ev event.Event) {
	switch e := ev.(type) {
	case event.Init:
		if e.SessionID != "" && e.SessionID != s.cfg.SessionID {
			// The agent names its own sessions: later starts resume this one.
			s.cfg.SessionID, s.cfg.Fork = e.SessionID, false
			s.saveConfig()
		}
		s.began = true
	case event.Approval:
		s.options[e.ID] = e.Options
	case event.Context:
		s.info.ContextTokens = e.Tokens
	}
	if b, err := eventLine(ev); err == nil {
		s.record(b)
	}
	for _, h := range headless.FromNeutral(ev) {
		s.onEvent(h)
	}
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
// clients answer as they would Claude Code: allow, always or deny, and a
// question's answers in AskUserQuestion's input.
func (s *server) answerAgent(conn agent.Conn, req headless.PermissionRequest, o op) error {
	if req.Tool == "AskUserQuestion" && o.Op == "allow" {
		a, ok := conn.(agent.Answerer)
		if !ok {
			return fmt.Errorf("this agent can't take answers")
		}
		var in struct {
			Answers map[string]string `json:"answers"`
		}
		_ = json.Unmarshal(o.Input, &in)
		answers := map[string][]string{}
		for q, labels := range in.Answers {
			answers[q] = strings.Split(labels, ", ")
		}
		return a.AnswerQuestion(req.ID, answers)
	}
	want := []event.OptionKind{event.AllowOnce}
	switch {
	case o.Op == "deny":
		want = []event.OptionKind{event.RejectOnce, event.RejectAlways}
	case o.Always:
		want = []event.OptionKind{event.AllowAlways, event.AllowOnce}
	}
	s.mu.Lock()
	opts := s.options[req.ID]
	delete(s.options, req.ID)
	s.mu.Unlock()
	for _, k := range want {
		for _, opt := range opts {
			if opt.Kind == k {
				return conn.Answer(req.ID, opt.ID)
			}
		}
	}
	// Refused with nothing to refuse with: cancelled is a no.
	return conn.Answer(req.ID, "")
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
