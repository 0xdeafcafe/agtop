package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/cellw"
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

// Solo shows the one session at the whole width under agtop's header: no
// list, and the keys that go to other agents do nothing.
func TestSoloShowsOneSession(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	writeSession(t, "aaaa1111", "the solo session")
	writeSession(t, "bbbb2222", "another session")

	m := NewSolo(state.Load(), "test", "aaaa1111")
	if len(m.snap.Agents) != 1 || m.soloKey == "" {
		t.Fatalf("solo snapshot: %d agents, key %q", len(m.snap.Agents), m.soloKey)
	}
	out := m.Frame(160, 45)
	for _, not := range []string{"another session", "esc back to the list", "describe a task", "ctrl+z zen", "  , ."} {
		if strings.Contains(out, not) {
			t.Errorf("solo frame shows %q:\n%s", not, out)
		}
	}
	// The header is agtop's, counting every agent, not only this one.
	for _, want := range []string{"agtop", "finished", "Agents", "Efficiency", "Machine", "Settings", "ctrl+\\"} {
		if !strings.Contains(out, want) {
			t.Errorf("solo frame lacks %q:\n%s", want, out)
		}
	}
	if l, p, _ := m.layout(); l != 0 || p != 160 || m.topH() != m.headH()+1 {
		t.Fatalf("solo layout: list %d pane %d top %d", l, p, m.topH())
	}

	c := &hostConn{key: m.soloKey, sess: convo.New(), open: map[string]bool{}}
	m.host = c
	for _, k := range []string{"tab", "ctrl+n", "ctrl+z", "left", "ctrl+k"} {
		soloPress(m, k)
		if m.sel != m.soloKey || m.view != placeAgents || m.zen || m.mode != modeList || !m.paneFocus || !m.full || m.bar != nil {
			t.Fatalf("after %s: sel %q view %d zen %v mode %d focus %v bar %v", k, m.sel, m.view, m.zen, m.mode, m.paneFocus, m.bar != nil)
		}
	}
	if out := m.render(); strings.Contains(out, "another session") {
		t.Fatalf("solo frame after keys:\n%s", out)
	}
	// , . < > are text, first or not.
	for _, k := range []string{">", ",", ".", "<", "a"} {
		soloPress(m, k)
	}
	if got := string(c.input); got != ">,.<a" || m.view != placeAgents {
		t.Fatalf("typed %q in view %d, want >,.<a", got, m.view)
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

// In solo, ctrl+\\ opens the other places as usual, and Agents is the one
// session again, never the list.
func TestSoloPlaces(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	writeSession(t, "aaaa1111", "the solo session")
	writeSession(t, "bbbb2222", "another session")
	m := NewSolo(state.Load(), "test", "aaaa1111")
	m.Frame(160, 45)
	m.host = &hostConn{key: m.soloKey, sess: convo.New(), open: map[string]bool{}}

	want := []int{placeEff, placeMachine, placeSettings, placeAgents}
	for i, place := range want {
		soloPress(m, "ctrl+\\")
		if m.view != place {
			t.Fatalf("ctrl+\\ %d: view %d, want %d", i+1, m.view, place)
		}
		if place == placeMachine {
			soloPress(m, "tab") // Processes to Cleanup, as usual
			if m.mode != modeCleanup {
				t.Fatalf("tab in Machine: mode %d", m.mode)
			}
		}
	}
	if m.sel != m.soloKey || !m.paneFocus || !m.full || m.mode != modeList {
		t.Fatalf("back in Agents: sel %q focus %v full %v mode %d", m.sel, m.paneFocus, m.full, m.mode)
	}
	soloPress(m, "ctrl+\\")
	soloPress(m, "esc")
	if m.view != placeAgents || m.sel != m.soloKey {
		t.Fatalf("esc from Efficiency: view %d sel %q", m.view, m.sel)
	}
	if out := m.render(); strings.Contains(out, "another session") {
		t.Fatalf("the list showed in solo:\n%s", out)
	}
}

// Solo's Session runs to the right edge of a wide screen, past the width
// a Session beside the list or alone in the full view stops at.
func TestSoloFillsTheWidth(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	writeSession(t, "aaaa1111", "the solo session")
	m := NewSolo(state.Load(), "test", "aaaa1111")
	m.Frame(208, 45)
	m.host = &hostConn{key: m.soloKey, sess: convo.New(), open: map[string]bool{}}
	out := m.Frame(208, 45)
	var head string
	for _, l := range strings.Split(out, "\n") {
		// The header's second row ends with the connection, on the right.
		if strings.Contains(ansi.Strip(l), "a message resumes it") {
			head = ansi.Strip(l)
			break
		}
	}
	if head == "" {
		t.Fatalf("no session header:\n%s", out)
	}
	if w := cellw.String(strings.TrimRight(head, " ")); w < 200 {
		t.Fatalf("the session header stops at column %d of 208: %q", w, head)
	}
	if l, p, _ := m.layout(); l != 0 || p != 208 {
		t.Fatalf("layout: list %d pane %d", l, p)
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
