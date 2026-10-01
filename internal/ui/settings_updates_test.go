package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/rush/internal/harnessup"
)

// Settings › Updates lists each harness with its versions and how it
// stands, and the key line counts what has an update.
func TestUpdatesPage(t *testing.T) {
	m, _ := benchModel(140, 50)
	m.upd.checked = true
	m.upd.rows = []harnessup.Status{
		{Kind: "codex", Name: "Codex", Installed: "0.155.1", Latest: "0.159.2", Update: harnessup.Update{Label: "brew upgrade --cask codex", Argv: []string{"brew"}}},
		{Kind: "gemini", Name: "Gemini CLI", Installed: "0.62.0", Latest: "0.62.0"},
		{Kind: "vibe", Name: "Vibe", Installed: "2.25.0"},
	}
	m.upd.stale = 2
	if n := m.updatesCount(); n != 1 {
		t.Fatalf("updatesCount = %d, want 1 (Codex)", n)
	}
	m.setView(placeSettings)
	m.setSettingsPage(pageUpdates)
	body := ansi.Strip(strings.Join(m.dialogBody(120), "\n"))
	for _, want := range []string{"update available", "up to date", "unknown", "2 sessions on an older rush"} {
		if !strings.Contains(body, want) {
			t.Errorf("Updates page lacks %q:\n%s", want, body)
		}
	}
}
