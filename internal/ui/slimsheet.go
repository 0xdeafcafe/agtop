package ui

import (
	"fmt"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/rush/internal/convo"
)

// #slim: what a session carries in every request and has never used (MCP
// servers, subagents, skills), to drop for that session alone, and its
// conversation to compact. Dropped, a thing is out of the agent's context
// from its next start, right away when it's idle; r puts it all back.

type slimSheet struct {
	sess  string // the session, by its key
	items []convo.Unused
	keep  map[int]bool // unticked: stays
	cur   int
	was   []string // what the session goes without already
}

// openSlim opens #slim on the session c.
func (m *Model) openSlim(c *hostConn) {
	switch {
	case c == nil || c.client == nil:
		m.flash("#slim works on a session rush runs: open one first", true)
		return
	case c.sess.Usage == nil:
		m.flash("no breakdown of its context yet: it comes when a turn ends", false)
		return
	}
	m.sheet = &slimSheet{sess: c.key, items: c.sess.Unused(), keep: map[int]bool{}, was: c.sess.Info.Without}
}

func (s *slimSheet) width(*Model) int { return 84 }

func (s *slimSheet) conn(m *Model) *hostConn {
	if c := m.host; c != nil && c.key == s.sess {
		return c
	}
	return nil
}

// drops are the rules the ticked items go by, after what's gone already.
func (s *slimSheet) drops() (rules []string, tokens int) {
	rules = slices.Clone(s.was)
	for i, it := range s.items {
		if s.keep[i] {
			continue
		}
		tokens += it.Tokens
		for _, r := range it.Rules {
			if !slices.Contains(rules, r) {
				rules = append(rules, r)
			}
		}
	}
	return rules, tokens
}

func (s *slimSheet) body(m *Model, w, _ int) []string {
	out := []string{sheetTitle("Slim this session", "what it carries every request and has never used", w), ""}
	if len(s.items) == 0 {
		out = append(out, dim("  every MCP server, subagent and skill it's told of, it has used"))
	}
	for i, it := range s.items {
		box := paint(cGreen, "[✕]")
		if s.keep[i] {
			box = faint("[ ]")
		}
		line := box + " " + dim(fit(it.What, 11)) + paint(cText, fit(it.Name, max(10, w-32))) + dim(fmt.Sprintf("%7s", convo.Tokens(it.Tokens)))
		out = append(out, sheetRow(line, i == s.cur, w))
	}
	_, tokens := s.drops()
	out = append(out, "")
	if tokens > 0 {
		out = append(out, "  "+paint(cGreen, "≈ "+convo.Tokens(tokens)+" tokens")+dim(" less in every request, for this session only"))
	}
	if c := s.conn(m); c != nil && c.sess.Usage != nil {
		msg := c.sess.Usage.Messages
		if all := msg.ToolCalls + msg.ToolResults + msg.Assistant + msg.User + msg.Attachments; all > 0 {
			out = append(out, "  "+dim("the conversation is "+convo.Tokens(all)+", "+convo.Tokens(msg.ToolResults)+" of it what tools sent back · c compacts it"))
		}
	}
	if len(s.was) > 0 {
		out = append(out, "  "+dim(fmt.Sprintf("it goes without %d already · r puts everything back", len(s.was))))
	}
	return append(out, "", keysFit(w, "space", "drop or keep", "a", "all or none", "enter", "drop them", "c", "compact", "esc", "cancel"))
}

func (s *slimSheet) key(m *Model, _ tea.KeyPressMsg, k string) tea.Cmd {
	c := s.conn(m)
	switch k {
	case "esc", "ctrl+c":
		m.sheet = nil
	case "up", "k", "shift+tab":
		s.cur = max(0, s.cur-1)
	case "down", "j", "tab":
		s.cur = min(max(0, len(s.items)-1), s.cur+1)
	case "space", " ":
		s.keep[s.cur] = !s.keep[s.cur]
	case "a":
		// Any dropped: keep them all; else drop them all.
		dropping := false
		for i := range s.items {
			dropping = dropping || !s.keep[i]
		}
		for i := range s.items {
			s.keep[i] = dropping
		}
	case "c":
		m.sheet = nil
		if c == nil {
			return nil
		}
		m.flash("compacting the conversation…", false)
		cl := c.client
		return hostCmd(func() error { return cl.Send("/compact") })
	case "r":
		if c == nil || len(s.was) == 0 {
			return nil
		}
		m.sheet = nil
		m.flash("everything it went without is back from its next start", false)
		cl := c.client
		return hostCmd(func() error { return cl.SetWithout(nil) })
	case "enter":
		m.sheet = nil
		rules, tokens := s.drops()
		if c == nil || slices.Equal(rules, s.was) {
			return nil
		}
		m.flash(fmt.Sprintf("dropped ≈ %s tokens a request · from its next start, now if it's idle · #slim r puts them back", convo.Tokens(tokens)), false)
		cl := c.client
		return hostCmd(func() error { return cl.SetWithout(rules) })
	}
	return nil
}
