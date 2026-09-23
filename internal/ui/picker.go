package ui

import (
	"fmt"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/fleet"
)

// picker is a small dropdown for choosing one of an agent's pull requests.
type picker struct {
	title  string
	prs    []claude.PR
	cursor int
}

// openPR opens the selected agent's pull request in the browser, asking
// which one first when it has several.
func (m *Model) openPR(a *fleet.Agent) tea.Cmd {
	if a == nil || len(a.PRs) == 0 {
		m.flash("no pull request found for this agent", true)
		return nil
	}
	if len(a.PRs) == 1 {
		return browse(a.PRs[0].URL)
	}
	m.picker = &picker{title: "Pull requests · " + oneLine(a.DisplayName), prs: a.PRs}
	return nil
}

func browse(url string) tea.Cmd {
	return func() tea.Msg {
		if err := exec.Command("open", url).Run(); err != nil {
			return doneMsg{err: fmt.Errorf("couldn't open %s: %w", url, err)}
		}
		return doneMsg{text: "opened " + url}
	}
}

func (m *Model) pickerKey(s string) tea.Cmd {
	p := m.picker
	switch s {
	case "esc", "q", "ctrl+y":
		m.picker = nil
	case "up", "k", "shift+tab":
		p.cursor = max(0, p.cursor-1)
	case "down", "j", "tab":
		p.cursor = min(len(p.prs)-1, p.cursor+1)
	case "enter":
		url := p.prs[p.cursor].URL
		m.picker = nil
		return browse(url)
	}
	return nil
}

func (m *Model) pickerBody(w int) []string {
	p := m.picker
	out := []string{paint(cText+bold, p.title), ""}
	for i, pr := range p.prs {
		col := cGreen
		switch {
		case pr.State == "MERGED":
			col = cBlue
		case pr.State == "CLOSED" || pr.State == "unknown":
			col = cDim
		case pr.Checks.Failed > 0:
			col = cRed
		}
		checks := ""
		if pr.Checks.Passed+pr.Checks.Failed+pr.Checks.Pending > 0 {
			checks = fmt.Sprintf("  %d✓ %d✗ %d…", pr.Checks.Passed, pr.Checks.Failed, pr.Checks.Pending)
		}
		title := pr.Title
		if title == "" {
			title = strings.TrimPrefix(pr.URL, "https://github.com/")
		}
		line := paint(col, fmt.Sprintf("#%-6d", pr.Number)) + " " + paint(cText, fit(title, w-40)) + "  " + dim(fit(strings.ToLower(pr.State), 8)) + faint(checks)
		if i == p.cursor {
			line = highlight(paint(cOrange, "▍")+line, w)
		} else {
			line = " " + line
		}
		out = append(out, line)
	}
	return append(out, "", keys("↑↓", "choose", "enter", "open in browser", "esc", "close"))
}
