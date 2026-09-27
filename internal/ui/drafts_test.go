package ui

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

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
	if len(ds) != 1 || ds[0].Text != "refactor the parser" || ds[0].Kind != state.KindCleared || ds[0].Name != "fix the tests" {
		t.Fatalf("drafts kept: %+v", ds)
	}
	m.openDrafts(c)
	m.sheet.key(m, tea.KeyPressMsg{}, "enter")
	if m.sheet != nil || string(c.input) != "refactor the parser" {
		t.Fatalf("enter should put the draft back: %q", string(c.input))
	}
}

// alt+s keeps the box as a draft and clears it; alt+p brings the latest
// back, then older ones, round to the newest again.
func TestSaveAndRecallDrafts(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	m, c := draftModel()
	m.paneKey(tea.KeyPressMsg{}, keyRecallDraft)
	if len(c.input) != 0 {
		t.Fatalf("no drafts: the box should stay empty, got %q", string(c.input))
	}
	for _, text := range []string{"first idea", "second idea", "third idea"} {
		typeInBox(m, text)
		m.paneKey(tea.KeyPressMsg{}, keySaveDraft)
		if len(c.input) != 0 {
			t.Fatalf("alt+s should clear the box: %q", string(c.input))
		}
	}
	if n := state.DraftCount(state.KindDraft); n != 3 {
		t.Fatalf("drafts kept: %d, want 3", n)
	}
	if h := draftsHolder("a message", ""); !strings.Contains(h, "3 drafts, alt+p") {
		t.Fatalf("the empty box should say the drafts wait: %q", h)
	}
	for _, want := range []string{"third idea", "second idea", "first idea", "third idea"} {
		m.paneKey(tea.KeyPressMsg{}, keyRecallDraft)
		if got := string(c.input); got != want {
			t.Fatalf("alt+p: %q, want %q", got, want)
		}
	}
	// Changed, the box's text is kept as a draft of its own, and alt+p
	// starts again from the newest.
	typeInBox(m, " now")
	m.paneKey(tea.KeyPressMsg{}, keyRecallDraft)
	if got := string(c.input); got != "third idea" {
		t.Fatalf("alt+p after an edit: %q", got)
	}
	waitFor(t, func() bool { return state.DraftCount(state.KindDraft) == 4 })
	// Undo gives back what alt+p replaced.
	m.paneKey(tea.KeyPressMsg{}, "super+z")
	if got := string(c.input); got != "third idea now" {
		t.Fatalf("undo after alt+p: %q", got)
	}
}

// The drafts sheet has a tab for each kind, with counts; enter from any
// of them puts one back in the box, and alt+s keeps a sent one as a draft.
func TestDraftSheetKinds(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	m, c := draftModel()
	old := time.Now().Add(-time.Hour)
	for _, d := range []state.Draft{
		{Text: "wiped", Kind: state.KindCleared, At: old},
		{Text: "sent one", Kind: state.KindSent, At: old},
		{Text: "sent two", Kind: state.KindSent, At: old},
		{Text: "kept", Kind: state.KindDraft, At: old},
	} {
		if err := state.AddDraft(d); err != nil {
			t.Fatal(err)
		}
	}
	m.openDrafts(c)
	d := m.sheet.(*draftSheet)
	body := strings.Join(d.body(m, 112, 30), "\n")
	for _, want := range []string{"Drafts 1", "Sent 2", "Cleared 1"} {
		if !strings.Contains(ansi.Strip(body), want) {
			t.Fatalf("the sheet should show %q:\n%s", want, ansi.Strip(body))
		}
	}
	if d.kindOf() != state.KindDraft {
		t.Fatalf("with drafts kept, the sheet opens on them, not %s", d.kindOf())
	}
	d.key(m, tea.KeyPressMsg{}, "tab")
	if got := d.shown(); len(got) != 2 || got[0].Text != "sent two" {
		t.Fatalf("Sent: %+v", got)
	}
	d.key(m, tea.KeyPressMsg{}, keySaveDraft)
	if n := state.DraftCount(state.KindDraft); n != 2 {
		t.Fatalf("alt+s on a sent one should keep it as a draft: %d drafts", n)
	}
	d.key(m, tea.KeyPressMsg{}, "tab")
	d.key(m, tea.KeyPressMsg{}, "enter")
	if m.sheet != nil || string(c.input) != "wiped" {
		t.Fatalf("enter on Cleared should put it back: %q", string(c.input))
	}
}

// A hint after a strip of tabs goes first when the row is too narrow.
func TestTabHintDropsFirst(t *testing.T) {
	if got := ansi.Strip(withTabHint("  a b", "[ ]", "views", "", 40)); got != "  a b   [ ] views" {
		t.Fatalf("wide: %q", got)
	}
	if got := ansi.Strip(withTabHint("  a b", "[ ]", "views", " zen", 12)); got != "  a b zen" {
		t.Fatalf("narrow: %q", got)
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatal("timed out")
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
