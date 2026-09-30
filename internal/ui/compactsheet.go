package ui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/convo"
	"github.com/0xdeafcafe/rush/internal/fleet"
	"github.com/0xdeafcafe/rush/internal/host"
)

// #compact: compact a session with its own model, as /compact does, or
// have another write the summary (a cheaper Claude, a local Ollama model)
// and carry on in a fresh conversation that starts with it. The cache
// goes either way; the conversation left is a path for /rewind.

type summarizer struct {
	kind  agent.Kind
	label string // "Ollama", "Claude"
	model string // "" for the session's own /compact
}

type compactSheet struct {
	sess, id, name string
	opts           []summarizer
	errs           []string // summarisers that couldn't say their models
	loading        bool
	cur            int
}

// lastSummarizer is the one picked last, where the cursor starts.
var lastSummarizer summarizer

func (m *Model) openCompact(c *hostConn, a *fleet.Agent) tea.Cmd {
	if c == nil || c.client == nil {
		m.flash("#compact works on a session rush runs: open one first", true)
		return nil
	}
	m.sheet = &compactSheet{sess: c.key, id: a.ID, name: a.DisplayName, loading: true,
		opts: []summarizer{{label: "its own model", model: ""}}}
	type found struct {
		opts []summarizer
		errs []string
	}
	return later(func() found {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var opts []summarizer
		var errs []string
		for _, ad := range agent.InstalledAll() {
			sz, ok := ad.(agent.Summarizer)
			if !ok {
				continue
			}
			models, err := sz.SummaryModels(ctx)
			if err != nil {
				errs = append(errs, ad.Name()+": "+err.Error())
			}
			for _, md := range models {
				opts = append(opts, summarizer{kind: ad.Kind(), label: ad.Name(), model: md})
			}
		}
		return found{opts, errs}
	}, func(m *Model, f found) tea.Cmd {
		s, ok := m.sheet.(*compactSheet)
		if !ok {
			return nil
		}
		s.opts, s.errs, s.loading = append(s.opts, f.opts...), f.errs, false
		for i, o := range s.opts {
			if o == lastSummarizer {
				s.cur = i
			}
		}
		return nil
	})
}

func (s *compactSheet) width(*Model) int { return 76 }

func (s *compactSheet) body(m *Model, w, h int) []string {
	out := []string{sheetTitle("Compact", "who writes the summary it carries on with", w), ""}
	from, to := window(len(s.opts), s.cur, max(3, h-8))
	for i := from; i < to; i++ {
		o := s.opts[i]
		line := paint(cText, o.label) + dim("  keeps the cache, costs what its model does")
		if o.model != "" {
			line = paint(cText, fit(o.label, 18)) + paint(cGreen, o.model)
		}
		out = append(out, sheetRow(line, i == s.cur, w))
	}
	if s.loading {
		out = append(out, dim("  finding the models installed…"))
	}
	for _, e := range s.errs {
		out = append(out, "  "+paint(cYellow, "! ")+dim(e))
	}
	return append(out, "", dim("  another model starts it afresh with its summary: the cache is lost, the rest kept (/rewind)"),
		"", keysFit(w, "↑↓", "choose", "enter", "compact", "esc", "cancel"))
}

func (s *compactSheet) key(m *Model, _ tea.KeyPressMsg, k string) tea.Cmd {
	switch k {
	case "esc", "ctrl+c":
		m.sheet = nil
	case "up", "k", "shift+tab":
		s.cur = max(0, s.cur-1)
	case "down", "j", "tab":
		s.cur = min(len(s.opts)-1, s.cur+1)
	case "enter":
		m.sheet = nil
		c := m.host
		if c == nil || c.key != s.sess || c.client == nil {
			return nil
		}
		o := s.opts[s.cur]
		lastSummarizer = o
		if o.model == "" {
			m.flash("compacting "+s.name+"…", false)
			cl := c.client
			return hostCmd(func() error { return cl.Send("/compact") })
		}
		if st := c.sess.Info.State; st == "working" || st == "blocked" {
			m.flash("it's working: let the turn end, then #compact", true)
			return nil
		}
		return m.compactBy(c, s.id, o)
	}
	return nil
}

// compactBy has o summarise session c and carries it on from the summary.
func (m *Model) compactBy(c *hostConn, id string, o summarizer) tea.Cmd {
	text, left, key := c.sess.PlainText(), leftOf(c), c.key
	by := o.label + " " + o.model
	m.flash(fmt.Sprintf("%s is summarising %s of conversation…", by, convo.Tokens(len(text)/4)), false)
	return func() tea.Msg {
		sz, ok := agent.As[agent.Summarizer](o.kind)
		if !ok {
			return doneMsg{err: fmt.Errorf("%s can't summarise", o.label)}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		summary, err := sz.Summarize(ctx, o.model, convo.CompactSystem, text)
		if err != nil {
			return doneMsg{err: fmt.Errorf("%s couldn't summarise it: %w", by, err)}
		}
		newID, _ := host.NewSessionID()
		err = hangUp(id, func(cl *host.Client) error { return cl.Compacted(newID, convo.CompactedPrompt(by, summary), left) })
		if err != nil {
			return doneMsg{err: err}
		}
		return rewoundMsg{key: key, text: "compacted by " + by + " · the conversation it had is kept (/rewind)"}
	}
}
