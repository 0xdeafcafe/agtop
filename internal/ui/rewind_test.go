package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/host"
	"github.com/0xdeafcafe/agtop/internal/state"
)

// The rewind sheet lists your messages newest first, then the paths you
// left; it says what's forgotten, and the code and note are yours to pick.
func TestRewindSheet(t *testing.T) {
	m := &Model{snap: &fleet.Snapshot{}, store: &state.Store{}, w: 140, h: 44}
	f := &rewindSheet{
		agent: "fixer", previews: map[string]*filesPreview{},
		turns: []convo.TurnStart{
			{N: 3, Prompt: "try caching it", UUID: "u3", Offset: 900},
			{N: 2, Prompt: "why is it slow", UUID: "u2", Offset: 400},
			{N: 1, Prompt: "fix the bug", UUID: "u1"},
		},
		branches: []host.Branch{{SessionID: "b1", From: 2, Turns: 4, Last: "rewrite the parser", Left: time.Now().Add(-time.Hour)}},
	}
	m.sheet = f
	text := func() string { return ansi.Strip(strings.Join(f.body(m, 120, 40), "\n")) }
	press := func(s string) { f.key(m, tea.KeyPressMsg{}, s) }

	got := text()
	for _, want := range []string{"turn 3 · the last", "why is it slow", "rewrite the parser", "It forgets turn 3.", "keep it as it is now", "nothing"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	press("down")
	if got := text(); !strings.Contains(got, "It forgets turns 2–3 (2 of your messages)") {
		t.Fatalf("second message:\n%s", got)
	}
	press("n")
	f.previews["u2"] = &filesPreview{}
	f.previews["u2"].r.CanRewind, f.previews["u2"].r.Files, f.previews["u2"].r.Insertions, f.previews["u2"].r.Deletions = true, []string{"/x/cache.go"}, 12, 30
	press("c")
	got = text()
	if !f.putBack || !f.recap || !strings.Contains(got, "put back as they were before turn 2 · 1 file +12 −30") || !strings.Contains(got, "a note of what was learned") {
		t.Fatalf("code and note:\n%s", got)
	}
	// A path left earlier: only switching, the files stay.
	press("end")
	if f.branch() == nil || !strings.Contains(text(), "Files stay as they are now") {
		t.Fatalf("branch:\n%s", text())
	}
	press("c") // nothing to put back on a branch
	press("esc")
	if m.sheet != nil {
		t.Fatal("esc closes it")
	}
}

// The message /rewind put back lands in the box once the pane reconnects.
func TestRewoundMessageFillsTheBox(t *testing.T) {
	m := &Model{snap: &fleet.Snapshot{}, store: &state.Store{}, w: 140, h: 44}
	m.onRewound(rewoundMsg{key: "k", draft: "try it another way", text: "rewound"})
	m.hostOpening = "k"
	c := &hostConn{key: "k", client: &host.Client{Lines: make(chan []byte)}, sess: convo.New(), open: map[string]bool{}}
	m.onHostOpen(hostOpenMsg{key: "k", c: c})
	if string(c.input) != "try it another way" || !m.paneFocus || len(m.rewound) != 0 {
		t.Fatalf("input %q focus %v left %v", string(c.input), m.paneFocus, m.rewound)
	}
}

// alt+r on a turn in the history rewinds to just after it; alt+f forks
// remembering up to it, and says whether the fork starts warm.
func TestRewindAndForkFromHistory(t *testing.T) {
	m := &Model{snap: &fleet.Snapshot{}, store: &state.Store{}, w: 140, h: 44}
	a := &fleet.Agent{Key: "k", DisplayName: "fixer", SessionID: "s1", Cwd: t.TempDir()}
	m.snap.Agents = []*fleet.Agent{a}
	c := &hostConn{key: "k", sess: convo.New(), open: map[string]bool{}}
	now := time.Now()
	for i, p := range []string{"fix the bug", "why is it slow", "try caching it"} {
		c.sess.Turns = append(c.sess.Turns, &convo.Turn{N: i + 1, Prompt: p, End: now.Add(time.Duration(i-3) * time.Minute)})
	}
	c.sess.Info.CacheWarm = now.Add(50 * time.Minute)
	c.sel = "t3"
	if selTurn(c) == nil || selTurn(c).N != 3 {
		t.Fatal("picked turn")
	}
	c.sel = "t2:s:toolu_1" // a step of turn 2 counts as turn 2
	m.openForkAt(c, a, selTurn(c))
	f, ok := m.sheet.(*forkSheet)
	if !ok || f.turns[f.upTo].n != 2 {
		t.Fatalf("fork from turn 2: %+v", m.sheet)
	}
	text := ansi.Strip(strings.Join(f.body(m, 120, 40), "\n"))
	if !strings.Contains(text, "remembers turns 1–2") || !strings.Contains(text, "added after the cache") {
		t.Fatalf("fork sheet:\n%s", text)
	}
	f.model = 4 // haiku, not what it runs
	if !strings.Contains(ansi.Strip(strings.Join(f.body(m, 120, 40), "\n")), "breaks the cache: another model") {
		t.Fatal("a different model starts cold")
	}
	f.model, f.effort = 0, 1 // low: another effort breaks it too
	if !strings.Contains(ansi.Strip(strings.Join(f.body(m, 120, 40), "\n")), "breaks the cache: another effort") {
		t.Fatal("a different effort starts cold")
	}
	f.effort, f.perm = 0, 3 // plan: permissions keep it
	if !strings.Contains(ansi.Strip(strings.Join(f.body(m, 120, 40), "\n")), "added after the cache") {
		t.Fatal("permissions don't break the cache")
	}
	m.sheet = nil
	// Rewind to the last turn: there's nothing after it.
	m.openRewindTo(c, a, c.sess.Turns[2])
	if m.sheet != nil {
		t.Fatal("nothing to rewind after the last turn")
	}
}
