package ui

import (
	"os"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/state"
)

// TestMain keeps what the tests save (drafts, config) out of your own
// home. It's HOME rather than AGTOP_HOME so a test can still have a home
// of its own with t.Setenv("HOME", …).
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "agtop-ui-test")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func draftModel() (*Model, *hostConn) {
	a := &fleet.Agent{Key: "k", DisplayName: "fix the tests"}
	m := &Model{snap: &fleet.Snapshot{Agents: []*fleet.Agent{a}}, store: &state.Store{}, paneFocus: true}
	c := &hostConn{key: "k", sess: convo.New(), open: map[string]bool{}}
	c.box = box{w: 40, lead: "❯ "}
	m.host = c
	return m, c
}

func typeInBox(m *Model, text string) {
	for _, r := range text {
		s := string(r)
		if r == ' ' {
			s = "space"
		}
		m.paneKey(tea.KeyPressMsg{Text: string(r), Code: r}, s)
	}
}

// A wiped box comes back with undo, and is kept in the drafts.
func TestWipeUndoAndDrafts(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	m, c := draftModel()
	typeInBox(m, "refactor the parser")
	m.paneKey(tea.KeyPressMsg{}, "esc")
	if len(c.input) != 0 {
		t.Fatalf("esc should clear the box: %q", string(c.input))
	}
	m.paneKey(tea.KeyPressMsg{}, "super+z")
	if got := string(c.input); got != "refactor the parser" {
		t.Fatalf("undo after esc: %q", got)
	}
	// Typing undoes a word at a time, not a letter.
	m.paneKey(tea.KeyPressMsg{}, "ctrl+_")
	if got := string(c.input); got != "refactor the " {
		t.Fatalf("undo a word: %q", got)
	}
	m.paneKey(tea.KeyPressMsg{}, "super+shift+z")
	if got := string(c.input); got != "refactor the parser" {
		t.Fatalf("redo: %q", got)
	}
	m.paneKey(tea.KeyPressMsg{}, "ctrl+c")
	var ds []state.Draft
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if ds = state.Drafts(); len(ds) > 0 {
			break
		}
	}
	if len(ds) != 1 || ds[0].Text != "refactor the parser" || ds[0].Sent || ds[0].Name != "fix the tests" {
		t.Fatalf("drafts kept: %+v", ds)
	}
	m.openDrafts(c)
	m.sheet.key(m, tea.KeyPressMsg{}, "enter")
	if m.sheet != nil || string(c.input) != "refactor the parser" {
		t.Fatalf("enter should put the draft back: %q", string(c.input))
	}
}

// ↑ and ↓ move through a message's rows, keeping the column.
func TestBoxVert(t *testing.T) {
	m, c := draftModel()
	c.input = []rune("first line\nsecond line\nthird")
	c.back = len("third") - 2 // on "th|ird"
	m.paneKey(tea.KeyPressMsg{}, "up")
	if pos := len(c.input) - c.back; pos != len("first line\nse") {
		t.Fatalf("up: at %d", pos)
	}
	m.paneKey(tea.KeyPressMsg{}, "up")
	m.paneKey(tea.KeyPressMsg{}, "up")
	if c.back != len(c.input) {
		t.Fatalf("up past the first row should go to the start, back=%d", c.back)
	}
	m.paneKey(tea.KeyPressMsg{}, "super+down")
	if c.back != 0 {
		t.Fatalf("cmd+↓ should go to the end, back=%d", c.back)
	}
}

// cmd+← and → reach the line's ends however the terminal sends cmd.
func TestCmdArrows(t *testing.T) {
	for _, s := range []string{"super+left", "ctrl+a", "home"} {
		if _, pos := press("one\ntwo three", 13, s); pos != 4 {
			t.Errorf("%s: at %d, want 4", s, pos)
		}
	}
	m, c := draftModel()
	c.input, c.back = []rune("hello"), 2
	m.key(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModMeta})
	if c.back != 5 {
		t.Errorf("meta+left should go to the start, back=%d", c.back)
	}
}
