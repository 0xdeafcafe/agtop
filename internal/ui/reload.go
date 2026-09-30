package ui

import (
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
)

// restoreEnv carries what was picked across #reload: whether the
// Session had the keys, then the agent selected.
const restoreEnv = "RUSH_RESTORE"

// reload is #reload: the view quits and main runs the installed rush in
// its place. Sessions run in hosts of their own, so they run on untouched.
func (m *Model) reload() tea.Cmd {
	m.reloading = true
	return m.quit()
}

// Reload is what the next rush starts with, when #reload asked for one.
func (m *Model) Reload() (env string, ok bool) {
	if !m.reloading {
		return "", false
	}
	pane := "0"
	if m.paneFocus {
		pane = "1"
	}
	return restoreEnv + "=" + pane + m.sel, true
}

// takeRestore reads what the last rush had picked, for applyRestore, and
// keeps it from anything this one runs.
func (m *Model) takeRestore() {
	v := os.Getenv(restoreEnv)
	_ = os.Unsetenv(restoreEnv)
	if len(v) > 1 {
		m.restore, m.restorePane = v[1:], v[0] == '1'
	}
}

// applyRestore selects the agent the last rush had, once it's listed.
func (m *Model) applyRestore() {
	if m.restore == "" {
		return
	}
	for _, l := range m.lines {
		if l.kind == lineAgent && l.agent.Key == m.restore {
			m.sel, m.paneFocus, m.restore = m.restore, m.restorePane, ""
			return
		}
	}
}

// watchBinary says, once, that rush was installed again since it started:
// #reload runs the new one. The file is looked at off the UI goroutine.
func (m *Model) watchBinary() tea.Cmd {
	if m.rebuiltSaid || m.tick%5 != 0 {
		return nil
	}
	was := m.exeAt
	return func() tea.Msg {
		exe, err := os.Executable()
		if err != nil {
			return nil
		}
		fi, err := os.Stat(exe)
		if err != nil {
			return nil
		}
		at := fi.ModTime()
		return applyMsg(func(m *Model) tea.Cmd {
			switch {
			case m.exeAt.IsZero():
				m.exeAt = at
			case was.Equal(m.exeAt) && at.After(m.exeAt) && !m.rebuiltSaid:
				m.rebuiltSaid = true
				m.flash("rush was installed again · #reload runs it, sessions carry on", false)
			}
			return nil
		})
	}
}

// reloadFields are the Model's, kept here with what uses them.
type reloadFields struct {
	reloading, restorePane, rebuiltSaid bool
	restore                             string
	exeAt                               time.Time
}
