package ui

import (
	"os"
	"strings"
	"syscall"
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

// draftHome gives the test a home of its own, and lets no draft be
// written there once the test is over.
func draftHome(t *testing.T) {
	t.Helper()
	t.Setenv("AGTOP_HOME", t.TempDir())
	t.Cleanup(func() {
		pendingDrafts.Clear()
		draftWrites.Wait()
	})
}

func openBox(key string) (*Model, *hostConn) {
	// Not in the list, so sending sends nothing anywhere.
	m := &Model{snap: &fleet.Snapshot{}, store: &state.Store{}, hostOpening: key}
	c := &hostConn{key: key, sess: convo.New(), open: map[string]bool{}}
	m.onHostOpen(hostOpenMsg{key: key, c: c})
	return m, c
}

// What's typed in a session's box is kept on disk: written once typing
// pauses, or at once when agtop quits or is told to end, and back in the
// box, cursor and images too, when the session opens again. Sending
// clears it.
func TestBoxDraftSurvivesARestart(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	const key = "acct/a:dddd4444"
	m, c := openBox(key)
	m.paneFocus = true
	typeInBox(m, "look at")
	m.attachImages([]string{"/shots/a.png"})
	typeInBox(m, " please")
	c.back = 3
	if m.noteDraft() == nil {
		t.Fatal("a changed box wasn't noted")
	}
	if m.noteDraft() != nil {
		t.Fatal("an unchanged box was noted again")
	}
	if _, ok := state.ReadBoxDraft(key); ok {
		t.Fatal("written before typing paused")
	}
	// More typing before the pause is over: the first tick writes nothing.
	typeInBox(m, "!")
	m.noteDraft()
	m.draftSave(draftSaveMsg{key: key, gen: c.draft.gen - 1})
	draftWrite.Lock() // a write, if one started, is over
	draftWrite.Unlock()
	if _, ok := state.ReadBoxDraft(key); ok {
		t.Fatal("written while still typing")
	}

	FlushDrafts() // as quitting, SIGTERM and SIGHUP do
	want, back := string(c.input), c.back
	if want != "look at [Image #1] ple!ase" {
		t.Fatalf("box %q", want)
	}

	// A new agtop opens the same session: the box is as it was.
	m2, c2 := openBox(key)
	if string(c2.input) != want || c2.back != back || c2.imgs.Path[1] != "/shots/a.png" || c2.imgs.N != 1 || !m2.paneFocus {
		t.Fatalf("restored %q back %d images %+v focus %v", string(c2.input), c2.back, c2.imgs, m2.paneFocus)
	}
	if m2.noteDraft() != nil {
		t.Fatal("the restored box was noted as a change")
	}
	// Sent, the draft goes.
	m2.sendPane(c2, false)
	m2.noteDraft()
	FlushDrafts()
	if _, ok := state.ReadBoxDraft(key); ok || len(c2.input) != 0 {
		t.Fatalf("the draft outlived the send: box %q", string(c2.input))
	}
	if _, c3 := openBox(key); len(c3.input) != 0 {
		t.Fatalf("a sent message came back: %q", string(c3.input))
	}
}

// The draft is written a moment after typing stops, by the tick noteDraft
// asks for.
func TestBoxDraftWrittenAfterAPause(t *testing.T) {
	draftHome(t)
	m, c := openBox("k2")
	m.paneFocus = true
	typeInBox(m, "hello")
	cmd := m.noteDraft()
	msg := cmd()
	m.update(msg)
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if d, ok := state.ReadBoxDraft(c.key); ok && d.Text == "hello" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("not written after the pause")
		}
	}
}

// SIGHUP, as when the terminal agtop is in closes, writes the drafts and
// ends the program.
func TestEndOnSignalsFlushes(t *testing.T) {
	draftHome(t)
	m, _ := openBox("k3")
	m.paneFocus = true
	typeInBox(m, "unsent")
	m.noteDraft()
	killed := make(chan struct{})
	stop := EndOnSignals(func() { close(killed) })
	defer stop()
	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	select {
	case <-killed:
	case <-time.After(2 * time.Second):
		t.Fatal("not ended on SIGHUP")
	}
	if d, ok := state.ReadBoxDraft("k3"); !ok || d.Text != "unsent" {
		t.Fatalf("kept %+v", d)
	}
}

func stashModel(t *testing.T, key, mode string) (*Model, *hostConn) {
	t.Helper()
	m, c := openBox(key)
	m.store.Config.CtrlSInBox = mode
	m.paneFocus = true
	return m, c
}

// With ctrl+s set to stash, as in Claude Code: the message goes aside, the
// box is free for another, and once that's sent the stashed one is back,
// its image and cursor with it.
func TestStashComesBackAfterTheNextSend(t *testing.T) {
	draftHome(t)
	m, c := stashModel(t, "s1", "stash")
	typeInBox(m, "the long one")
	m.attachImages([]string{"/shots/long.png"})
	c.back = 2
	m.paneKey(tea.KeyPressMsg{}, "ctrl+s")
	if len(c.input) != 0 || c.stash == nil || c.stash.Text != "the long one [Image #1]" || c.imgs.N != 0 {
		t.Fatalf("after stash: box %q stash %+v imgs %+v", string(c.input), c.stash, c.imgs)
	}
	b := m.paneDock(&fleet.Agent{Key: "s1", DisplayName: "s1"}, c, 80, 40)
	if !strings.Contains(ansi.Strip(strings.Join(b, "\n")), "stashed · ctrl+s to restore") {
		t.Fatalf("no stash mark:\n%s", ansi.Strip(strings.Join(b, "\n")))
	}
	typeInBox(m, "quick one")
	m.sendPane(c, false)
	if got := string(c.input); got != "the long one [Image #1]" || c.back != 2 || c.imgs.Path[1] != "/shots/long.png" || c.stash != nil {
		t.Fatalf("after send: box %q back %d imgs %+v stash %+v", got, c.back, c.imgs, c.stash)
	}
}

// ctrl+s on an empty box brings the stash back; on a full one with
// something stashed, the two change places.
func TestStashRestoresByKey(t *testing.T) {
	draftHome(t)
	m, c := stashModel(t, "s2", "stash")
	typeInBox(m, "first")
	m.paneKey(tea.KeyPressMsg{}, "ctrl+s")
	typeInBox(m, "second")
	m.paneKey(tea.KeyPressMsg{}, "ctrl+s")
	if string(c.input) != "first" || c.stash == nil || c.stash.Text != "second" {
		t.Fatalf("swap: box %q stash %+v", string(c.input), c.stash)
	}
	m.wipeBox(c)
	m.paneKey(tea.KeyPressMsg{}, "ctrl+s")
	if string(c.input) != "second" || c.stash != nil {
		t.Fatalf("restore: box %q stash %+v", string(c.input), c.stash)
	}
}

// The stash is kept on disk with the box's draft, so a new agtop has it.
func TestStashSurvivesARestart(t *testing.T) {
	draftHome(t)
	m, _ := stashModel(t, "s3", "stash")
	typeInBox(m, "aside")
	m.attachImages([]string{"/shots/a.png"})
	m.paneKey(tea.KeyPressMsg{}, "ctrl+s")
	typeInBox(m, "half")
	m.noteDraft()
	FlushDrafts()
	m2, c2 := stashModel(t, "s3", "stash")
	if string(c2.input) != "half" || c2.stash == nil || c2.stash.Text != "aside [Image #1]" || c2.stash.Images[1] != "/shots/a.png" {
		t.Fatalf("restored box %q stash %+v", string(c2.input), c2.stash)
	}
	// Only the stash: the file stays for it.
	m2.wipeBox(c2)
	m2.noteDraft()
	FlushDrafts()
	if d, ok := state.ReadBoxDraft("s3"); !ok || d.Stash == nil || d.Text != "" {
		t.Fatalf("kept %+v", d)
	}
	m2.paneKey(tea.KeyPressMsg{}, "ctrl+s")
	m2.noteDraft()
	FlushDrafts()
	if string(c2.input) != "aside [Image #1]" {
		t.Fatalf("box %q", string(c2.input))
	}
	if d, _ := state.ReadBoxDraft("s3"); d.Stash != nil {
		t.Fatalf("stash still kept: %+v", d)
	}
}

// Each setting's keys: ctrl+s sends now by default and alt+s stashes; set
// to stash, they change places.
func TestStashKeysFollowTheSetting(t *testing.T) {
	draftHome(t)
	for _, tc := range []struct{ mode, send, stash string }{
		{"", "ctrl+s", "alt+s"},
		{"stash", "alt+s", "ctrl+s"},
	} {
		m, c := stashModel(t, "k-"+tc.mode, tc.mode)
		if send, stash := m.boxKeys(); send != tc.send || stash != tc.stash {
			t.Fatalf("%q: keys %s %s", tc.mode, send, stash)
		}
		typeInBox(m, "keep me")
		m.paneKey(tea.KeyPressMsg{}, tc.stash)
		if c.stash == nil || len(c.input) != 0 {
			t.Fatalf("%q: %s didn't stash", tc.mode, tc.stash)
		}
		typeInBox(m, "go now")
		m.paneKey(tea.KeyPressMsg{}, tc.send)
		if string(c.input) != "keep me" || c.stash != nil {
			t.Fatalf("%q: %s didn't send: box %q", tc.mode, tc.send, string(c.input))
		}
		send, stash := m.boxKeys()
		m.helpPage = 0
		guide := ansi.Strip(strings.Join(m.helpBody(), "\n"))
		if !strings.Contains(guide, send) || !strings.Contains(guide, stash) || strings.Contains(guide, "\x00") {
			t.Fatalf("%q guide:\n%s", tc.mode, guide)
		}
		hint := ansi.Strip(strings.Join(m.paneDock(&fleet.Agent{Key: c.key, DisplayName: "x"}, c, 120, 40), "\n"))
		if !strings.Contains(hint, stash+" stash") {
			t.Fatalf("%q hint:\n%s", tc.mode, hint)
		}
	}
}
