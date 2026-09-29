package ui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/rush/internal/update"
)

// updateMsg is a newer rush being out; updatedMsg is #update finishing.
type updateMsg struct{ newer update.Info }
type updatedMsg struct {
	to  update.Info
	err error
}

// checkUpdate asks, in the background, whether a newer rush is out.
// Offline (--soak) never asks.
func (m *Model) checkUpdate() tea.Cmd {
	if m.offline {
		return nil
	}
	return func() tea.Msg {
		if l, newer := update.Check(context.Background()); newer {
			return updateMsg{newer: l}
		}
		return nil
	}
}

// installUpdate is #update: the newest rush over this one, with go install.
func (m *Model) installUpdate() tea.Cmd {
	if m.updating {
		m.flash("already updating…", false)
		return nil
	}
	m.updating = true
	m.flash("updating rush…", false)
	return func() tea.Msg {
		to, err := update.Install(context.Background())
		return updatedMsg{to: to, err: err}
	}
}
