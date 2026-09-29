package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/hooks"
	"github.com/0xdeafcafe/agtop/internal/plugin"
)

// --- a plugin found installed, waiting on you ---

// pluginPendingMsg is what checkPluginApprovals found: plugins sitting in
// ~/.config/agtop/plugins that were never approved, or changed since.
type pluginPendingMsg struct{ pending []plugin.Plugin }

// checkPluginApprovals looks once, off the UI, at plugin.Pending.
func (m *Model) checkPluginApprovals() tea.Cmd {
	return func() tea.Msg { return pluginPendingMsg{pending: plugin.Pending()} }
}

// openPluginApproval offers the first of pending, unless something already
// has the screen: a sheet, a dialog, a confirm, or you're not in Agents.
func (m *Model) openPluginApproval(pending []plugin.Plugin) {
	if len(pending) == 0 || m.sheet != nil || m.dialog != nil || m.confirm != nil || m.mode != modeList {
		return
	}
	m.sheet = &pluginApprovalSheet{p: pending[0], rest: pending[1:]}
}

// pluginApprovalSheet is agtop plugin approve, without a terminal: what
// plugin.Describe says it may do, and Approve or Not now.
type pluginApprovalSheet struct {
	p    plugin.Plugin
	rest []plugin.Plugin // more Pending, offered after this one

	cur  int // 0 Approve, 1 Not now
	busy bool
	err  string
}

func (s *pluginApprovalSheet) width(*Model) int { return 100 }

func (s *pluginApprovalSheet) body(m *Model, w, h int) []string {
	status := "found, not approved"
	if _, ok := plugin.Approvals()[s.p.Name]; ok {
		status = "changed since it was approved"
	}
	out := []string{sheetTitle("Plugin", s.p.Name+" · "+status, w), ""}
	body := wrap(plugin.Describe(s.p), w-2)
	room := max(3, h-len(out)-5)
	from, to := window(len(body), 0, room)
	for _, l := range body[from:to] {
		out = append(out, "  "+l)
	}
	out = append(out, "")
	choices := []struct{ what, about string }{
		{"Approve", "let it run, sandboxed, as described above"},
		{"Not now", "leave it stopped · asks again if it changes"},
	}
	for i, c := range choices {
		out = append(out, sheetRow(paint(cText+bold, c.what)+"  "+dim(c.about), i == s.cur, w))
	}
	if s.err != "" {
		out = append(out, "", paint(cRed, s.err))
	}
	k := keysFit(w, "↑↓", "choose", "enter", "pick", "esc", "not now")
	if s.busy {
		k = paint(cYellow, "⋯ working")
	}
	return append(out, "", k)
}

func (s *pluginApprovalSheet) key(m *Model, k tea.KeyPressMsg, str string) tea.Cmd {
	if s.busy {
		return nil
	}
	switch str {
	case "up", "shift+tab", "down", "tab":
		s.cur = 1 - s.cur
	case "y":
		s.cur = 0
		return s.pick(m)
	case "n", "esc", "ctrl+c":
		s.cur = 1
		return s.pick(m)
	case "enter":
		return s.pick(m)
	}
	return nil
}

func (s *pluginApprovalSheet) pick(m *Model) tea.Cmd {
	s.busy, s.err = true, ""
	p, rest := s.p, s.rest
	if s.cur == 1 {
		return sheetDo(func() ([]plugin.Plugin, error) {
			err := plugin.Decline(p)
			return rest, err
		}, s.done("left "+p.Name+" stopped"))
	}
	return sheetDo(func() ([]plugin.Plugin, error) {
		if err := plugin.Approve(p); err != nil {
			return nil, err
		}
		return rest, hooks.Reload()
	}, s.done(p.Name+" approved · sessions pick it up as they next start"))
}

// done applies the outcome of Approve or Decline: on success it moves to
// the next pending plugin, if any; on failure it stays put so you can see
// why and try again.
func (s *pluginApprovalSheet) done(said string) func(*Model, []plugin.Plugin, error) tea.Cmd {
	return func(m *Model, rest []plugin.Plugin, err error) tea.Cmd {
		if m.sheet != s {
			return nil
		}
		if err != nil {
			s.busy, s.err = false, err.Error()
			return nil
		}
		m.sheet = nil
		m.flash(said, false)
		m.openPluginApproval(rest)
		return nil
	}
}
