// Package claude is Claude Code as an agtop adapter: its config folders,
// its headless sessions as agtop's own events, its prices and commands.
package claude

import (
	"context"
	"errors"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/agent/event"
	"github.com/0xdeafcafe/agtop/internal/agent/usage"
	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/headless"
	"github.com/0xdeafcafe/agtop/internal/state"
)

// Kind is Claude Code's.
const Kind agent.Kind = "claude"

func init() { agent.Register(Adapter{}) }

// Adapter is Claude Code.
type Adapter struct {
	// Config is agtop's, for the folders it knows; nil loads it.
	Config func() state.Config
}

func (Adapter) Kind() agent.Kind { return Kind }
func (Adapter) Name() string     { return "Claude Code" }

func (Adapter) Caps() agent.Caps {
	return agent.CapResume | agent.CapFork | agent.CapRewind | agent.CapImages | agent.CapEffort | agent.CapModes |
		agent.CapSubagents | agent.CapBackground | agent.CapQuestions | agent.CapContext | agent.CapCompact |
		agent.CapMCP | agent.CapHooks | agent.CapPlugins | agent.CapStatusLine | agent.CapScreen
}

// Profiles are ~/.claude and every other folder agtop was given.
func (a Adapter) Profiles() []agent.Profile {
	cfg := a.config()
	var out []agent.Profile
	for _, acct := range cfg.AllAccounts() {
		out = append(out, Profile(acct))
	}
	return out
}

func (a Adapter) config() state.Config {
	if a.Config != nil {
		return a.Config()
	}
	return state.Load().Config
}

// Profile is a Claude config folder as agtop's own.
func Profile(a claude.Account) agent.Profile {
	return agent.Profile{Kind: Kind, Name: a.Name, Dir: a.ConfigDir}
}

// Account is a profile as the Claude config folder it is.
func Account(p agent.Profile) claude.Account {
	return claude.Account{Name: p.Name, ConfigDir: p.Dir}
}

// Cost is what model's tokens cost at list prices.
func (Adapter) Cost(model string, u usage.TokenUsage) (float64, bool) {
	if _, ok := claude.PriceFor(model); !ok {
		return 0, false
	}
	return claude.Cost(model, u, false), true
}

// Commands are the slash commands and skills a session in cwd can run.
func (Adapter) Commands(p agent.Profile, cwd string) []agent.Command {
	return claude.Commands(p.Dir, cwd)
}

// Start runs claude -p for agtop to draw.
func (Adapter) Start(ctx context.Context, o agent.StartOptions) (agent.Conn, error) {
	ho := headless.Options{Account: Account(o.Profile), Dir: o.Dir, Model: o.Model, Effort: o.Effort,
		PermissionMode: o.Mode, Flags: o.Flags, Env: o.Env, Binary: o.Binary}
	if o.Resume {
		ho.Resume = o.SessionID
	} else {
		ho.SessionID = o.SessionID
	}
	if o.Fork {
		ho.Flags = append(ho.Flags, "--fork-session")
	}
	s, err := headless.Start(ho)
	if err != nil {
		return nil, err
	}
	if _, err := s.Initialize(); err != nil {
		_ = s.Stop(time.Second)
		return nil, err
	}
	c := &conn{s: s, events: make(chan event.Event, 64), asks: map[string]headless.PermissionRequest{}}
	go c.relay(ctx)
	return c, nil
}

// conn is a running claude -p as an agent.Conn.
type conn struct {
	s      *headless.Session
	events chan event.Event
	mu     sync.Mutex
	asks   map[string]headless.PermissionRequest // approvals and questions not yet answered
}

func (c *conn) relay(ctx context.Context) {
	defer close(c.events)
	var n headless.Neutral
	for {
		select {
		case <-ctx.Done():
			_ = c.s.Stop(3 * time.Second)
			return
		case ev, ok := <-c.s.Events:
			if !ok {
				return
			}
			switch e := ev.(type) {
			case headless.PermissionRequest:
				c.mu.Lock()
				c.asks[e.ID] = e
				c.mu.Unlock()
			case headless.PermissionCancelled:
				c.mu.Lock()
				delete(c.asks, e.ID)
				c.mu.Unlock()
			}
			for _, out := range n.Event(ev) {
				c.events <- out
			}
		}
	}
}

func (c *conn) Events() <-chan event.Event { return c.events }

func (c *conn) Send(in agent.Input) error {
	var imgs []headless.Image
	for _, p := range in.Images {
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		mt := mime.TypeByExtension(strings.ToLower(filepath.Ext(p)))
		if mt == "" {
			mt = "image/png"
		}
		imgs = append(imgs, headless.Image{MediaType: mt, Data: b})
	}
	return c.s.SendWith(in.Text, imgs)
}

// take is the request id asked, once: answering it forgets it.
func (c *conn) take(id string) (headless.PermissionRequest, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.asks[id]
	delete(c.asks, id)
	return r, ok
}

var errNotAsked = errors.New("nothing is waiting on that answer")

// Answer answers an approval with one of the options headless.Neutral
// gave it: allow, always or deny.
func (c *conn) Answer(approvalID, optionID string) error {
	r, ok := c.take(approvalID)
	if !ok {
		return errNotAsked
	}
	switch optionID {
	case "allow":
		return c.s.Allow(r, r.Input, false)
	case "always":
		return c.s.Allow(r, r.Input, true)
	}
	return c.s.Deny(r, "", false)
}

// AnswerQuestion answers an AskUserQuestion. Claude takes several choices
// as one answer, joined.
func (c *conn) AnswerQuestion(id string, answers map[string][]string) error {
	r, ok := c.take(id)
	if !ok {
		return errNotAsked
	}
	_, qs := r.Questions()
	joined := map[string]string{}
	for q, labels := range answers {
		joined[q] = strings.Join(labels, ", ")
	}
	return c.s.Allow(r, r.AnswerInput(qs, joined), false)
}

func (c *conn) Interrupt() error            { return c.s.Interrupt() }
func (c *conn) SetModel(model string) error { return c.s.SetModel(model) }
func (c *conn) SetMode(mode string) error   { return c.s.SetPermissionMode(mode) }
func (c *conn) Close() error                { return c.s.Stop(3 * time.Second) }

var (
	_ agent.Adapter   = Adapter{}
	_ agent.Driver    = Adapter{}
	_ agent.Pricer    = Adapter{}
	_ agent.Commander = Adapter{}
	_ agent.Answerer  = (*conn)(nil)
)
