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
	case "ctrl+n", "ctrl+z", "ctrl+\\", "ctrl+k", "ctrl+6":
		k = tea.KeyPressMsg{Code: rune(s[5]), Mod: tea.ModCtrl}
	}
	_, cmd := m.Update(k)
	return cmd
}

// Solo shows the one session at the whole width under agtop's header: no
// list, and the keys that go to other agents do nothing.
func TestSoloShowsOneSession(t *testing.T) {
	draftHome(t)
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
	for _, k := range []string{"tab", "ctrl+n", "ctrl+z", "left"} {
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
	// esc never quits solo: it takes the keys off the box, and with no
	// turn running esc again does nothing.
	for i := 0; i < 3; i++ {
		if cmd := soloPress(m, "esc"); cmd != nil && isQuit(cmd) {
			t.Fatalf("esc %d quit solo", i+1)
		}
		if m.paneFocus || !m.soloAway || m.view != placeAgents {
			t.Fatalf("esc %d: focus %v away %v view %d", i+1, m.paneFocus, m.soloAway, m.view)
		}
	}
	if out := ansi.Strip(m.render()); !strings.Contains(out, "enter · → type here") || strings.Contains(out, "stop the turn") {
		t.Fatalf("hint away from the box:\n%s", out)
	}
	// Typing goes back to the box, the key with it.
	soloPress(m, "b")
	if !m.paneFocus || m.soloAway || string(c.input) != "b" {
		t.Fatalf("typing after esc: focus %v away %v box %q", m.paneFocus, m.soloAway, string(c.input))
	}
	if out := ansi.Strip(m.render()); !strings.Contains(out, "esc leave the box") || strings.Contains(out, "esc close") {
		t.Fatalf("solo hint:\n%s", out)
	}
}

// In solo, esc and esc again with a turn running stops it, as ctrl+x
// does, and agtop keeps running; only ctrl+q quits.
func TestSoloEscStopsTheTurn(t *testing.T) {
	draftHome(t)
	writeSession(t, "cccc3333", "a working session")
	m := NewSolo(state.Load(), "test", "cccc3333")
	m.Frame(160, 45)
	c := &hostConn{key: m.soloKey, sess: convo.New(), open: map[string]bool{}, client: &host.Client{}}
	c.sess.Apply(host.Sent{Text: "do the work"}, time.Now())
	if c.sess.Live() == nil {
		t.Fatal("no live turn")
	}
	m.host = c
	if cmd := soloPress(m, "esc"); cmd != nil && isQuit(cmd) || m.paneFocus {
		t.Fatalf("first esc: quit or kept the box (focus %v)", m.paneFocus)
	}
	if out := ansi.Strip(m.render()); !strings.Contains(out, "esc stop the turn") {
		t.Fatalf("hint:\n%s", out)
	}
	// The interrupt goes to the host (not run here: there's none).
	if cmd := soloPress(m, "esc"); cmd == nil || c.stopArmed.IsZero() || m.status != "stopping the turn" {
		t.Fatalf("second esc: cmd %v, stop armed %v, status %q", cmd != nil, c.stopArmed, m.status)
	}
	if m.paneFocus || m.view != placeAgents || m.solo == "" {
		t.Fatalf("after stopping: focus %v view %d", m.paneFocus, m.view)
	}
	if cmd := m.key(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl}); cmd == nil || !isQuit(cmd) {
		t.Fatal("ctrl+q should quit")
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

// ctrl+6 shows the list beside solo's session: every agent, to pick and
// read. Again, or esc from the list, and the view is solo's own session.
func TestSoloListToggle(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	writeSession(t, "aaaa1111", "the solo session")
	writeSession(t, "bbbb2222", "another session")
	m := NewSolo(state.Load(), "test", "aaaa1111")
	m.Frame(200, 45)
	m.host = &hostConn{key: m.soloKey, sess: convo.New(), open: map[string]bool{}}
	if out := m.render(); strings.Contains(out, "another session") || !strings.Contains(out, "ctrl+6") {
		t.Fatalf("solo alone shows the list, or not the key:\n%s", out)
	}
	for _, key := range []tea.KeyPressMsg{{Code: '6', Mod: tea.ModCtrl}, {Code: '^', Mod: tea.ModCtrl}} {
		if !listKey(key.String()) {
			t.Fatalf("%q isn't the list key", key.String())
		}
		m.Update(key)
		out := m.Frame(200, 45)
		if !m.soloList || m.listW == 0 || m.paneFocus {
			t.Fatalf("%s: list %v width %d focus %v", key.String(), m.soloList, m.listW, m.paneFocus)
		}
		if !strings.Contains(out, "another session") || !strings.Contains(out, "the solo session") || !strings.Contains(out, "ctrl+6") {
			t.Fatalf("the list isn't shown:\n%s", out)
		}
		// Another agent can be picked, and stays picked.
		other := m.keyOf(sid("bbbb2222"))
		m.sel = other
		m.Update(tickMsg(time.Now()))
		if m.sel != other || m.focused() == nil || m.focused().Key != other {
			t.Fatalf("picking another agent: sel %q", m.sel)
		}
		m.Update(key)
		if m.soloList || m.sel != m.soloKey || !m.paneFocus || len(m.snap.Agents) != 1 {
			t.Fatalf("hidden again: list %v sel %q focus %v agents %d", m.soloList, m.sel, m.paneFocus, len(m.snap.Agents))
		}
		if out := m.Frame(200, 45); strings.Contains(out, "another session") {
			t.Fatalf("the list stayed:\n%s", out)
		}
	}
	// esc from the list hides it too.
	soloPress(m, "ctrl+6")
	soloPress(m, "esc")
	if m.soloList || m.sel != m.soloKey {
		t.Fatalf("esc from the list: list %v sel %q", m.soloList, m.sel)
	}
}

// ctrl+k works in solo, over every agent: going to another one shows the
// list beside it, as ctrl+6 does, and ctrl+6 is the way back.
func TestSoloCommandBar(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	writeSession(t, "aaaa1111", "the solo session")
	writeSession(t, "bbbb2222", "another session")
	m := NewSolo(state.Load(), "test", "aaaa1111")
	m.Frame(200, 45)
	m.host = &hostConn{key: m.soloKey, sess: convo.New(), open: map[string]bool{}}
	m.Update(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	if m.bar == nil {
		t.Fatal("ctrl+k didn't open the bar in solo")
	}
	typeBar(m, "another session")
	var other *barItem
	for i, it := range m.bar.items {
		if it.section == "Agents" && it.title == "another session" {
			other = &m.bar.items[i]
			m.bar.cursor = i
		}
	}
	if other == nil {
		t.Fatalf("the bar doesn't offer the other agent: %+v", m.bar.items)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(tickMsg(time.Now()))
	if m.bar != nil || !m.soloList || m.sel != m.keyOf(sid("bbbb2222")) {
		t.Fatalf("going to it: bar %v list %v sel %q", m.bar != nil, m.soloList, m.sel)
	}
	if out := m.Frame(200, 45); !strings.Contains(out, "another session") {
		t.Fatalf("not shown:\n%s", out)
	}
	soloPress(m, "ctrl+6")
	if m.soloList || m.sel != m.soloKey || len(m.snap.Agents) != 1 {
		t.Fatalf("back: list %v sel %q", m.soloList, m.sel)
	}
	// A place from the bar opens as in solo's own places.
	m.Update(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	typeBar(m, "clean")
	if len(m.bar.items) == 0 || m.bar.items[0].title != "Machine › Cleanup" {
		t.Fatalf("clean in the bar: %+v", m.bar.items)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.view != placeMachine || m.mode != modeCleanup {
		t.Fatalf("clean from the bar: view %d mode %d", m.view, m.mode)
	}
}

// Outside solo, ctrl+6 hides the list beside an open Session, and shows it
// again.
func TestListToggle(t *testing.T) {
	m, _ := benchModel(200, 50)
	m.View()
	if m.listW == 0 {
		t.Fatal("the bench model should open split")
	}
	key := tea.KeyPressMsg{Code: '6', Mod: tea.ModCtrl}
	m.Update(key)
	m.View()
	if m.listW != 0 || !m.paneFocus {
		t.Fatalf("hiding: list %d focus %v", m.listW, m.paneFocus)
	}
	m.Update(key)
	m.View()
	if m.listW == 0 || m.paneFocus {
		t.Fatalf("showing: list %d focus %v", m.listW, m.paneFocus)
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
