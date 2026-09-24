package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/0xdeafcafe/agtop/internal/host"
	"github.com/0xdeafcafe/agtop/internal/state"
)

// writeSession leaves a stopped agtop-mode session behind, as a host that
// ended would.
func writeSession(t *testing.T, id, name string) {
	t.Helper()
	d := filepath.Join(host.Root(), id)
	if err := os.MkdirAll(d, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	b, _ := json.Marshal(host.Info{ID: id, SessionID: id + "-0000-4000-8000-000000000000", Cwd: t.TempDir(), Name: name,
		State: "stopped", StartedAt: now, UpdatedAt: now})
	if err := os.WriteFile(filepath.Join(d, "info.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func soloPress(m *Model, s string) tea.Cmd {
	k := tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
	switch s {
	case "tab":
		k = tea.KeyPressMsg{Code: tea.KeyTab}
	case "esc":
		k = tea.KeyPressMsg{Code: tea.KeyEscape}
	case "left":
		k = tea.KeyPressMsg{Code: tea.KeyLeft}
	case "ctrl+n", "ctrl+z", "ctrl+\\", "ctrl+k":
		k = tea.KeyPressMsg{Code: rune(s[5]), Mod: tea.ModCtrl}
	}
	_, cmd := m.Update(k)
	return cmd
}

// Solo shows the one session full width: no list, no header, and the keys
// that go to other agents or places do nothing.
func TestSoloShowsOneSession(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	writeSession(t, "aaaa1111", "the solo session")
	writeSession(t, "bbbb2222", "another session")

	m := NewSolo(state.Load(), "test", "aaaa1111")
	if len(m.snap.Agents) != 1 || m.soloKey == "" {
		t.Fatalf("solo snapshot: %d agents, key %q", len(m.snap.Agents), m.soloKey)
	}
	out := m.Frame(160, 45)
	for _, not := range []string{"another session", "finished", "Agents", "Efficiency", "esc back to the list", "describe a task"} {
		if strings.Contains(out, not) {
			t.Errorf("solo frame shows %q:\n%s", not, out)
		}
	}
	if l, p, _ := m.layout(); l != 0 || p != 160 || m.topH() != 0 {
		t.Fatalf("solo layout: list %d pane %d top %d", l, p, m.topH())
	}

	c := &hostConn{key: m.soloKey, sess: convo.New(), open: map[string]bool{}}
	m.host = c
	for _, k := range []string{"tab", "ctrl+n", ",", ".", "<", ">", "ctrl+\\", "ctrl+z", "left", "ctrl+k"} {
		soloPress(m, k)
		if m.sel != m.soloKey || m.view != placeAgents || m.zen || m.mode != modeList || !m.paneFocus || !m.full || m.bar != nil {
			t.Fatalf("after %s: sel %q view %d zen %v mode %d focus %v bar %v", k, m.sel, m.view, m.zen, m.mode, m.paneFocus, m.bar != nil)
		}
	}
	if out := m.render(); strings.Contains(out, "another session") || strings.Contains(out, "Agents") {
		t.Fatalf("solo frame after keys:\n%s", out)
	}
	// , typed after text is text.
	soloPress(m, "a")
	soloPress(m, ",")
	if got := string(c.input); got != "a," {
		t.Fatalf("typed %q, want a,", got)
	}
	soloPress(m, "esc") // clears the box
	if len(c.input) != 0 {
		t.Fatalf("esc left %q", string(c.input))
	}
	cmd := m.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil || !isQuit(cmd) {
		t.Fatal("esc at the top level should quit")
	}
}

// Outside solo the same keys do move: the test above means something.
func TestPlacesMoveOutsideSolo(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	writeSession(t, "aaaa1111", "the solo session")
	m := New(state.Load(), "test")
	m.Frame(160, 45)
	soloPress(m, ".")
	if m.view == placeAgents {
		t.Fatal(". should move to the next place outside solo")
	}
}

// isQuit runs a command, batches included, looking for tea.Quit.
func isQuit(cmd tea.Cmd) bool {
	switch msg := cmd().(type) {
	case tea.QuitMsg:
		return true
	case tea.BatchMsg:
		for _, c := range msg {
			if c != nil && isQuit(c) {
				return true
			}
		}
	}
	return false
}
