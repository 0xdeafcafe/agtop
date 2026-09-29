package drafts

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/plugin"
)

var t0 = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

func box(s string) plugin.Box { return plugin.Box{Text: s, Cursor: len([]rune(s))} }

// A box is kept until it's sent, and comes back into an empty box.
func TestBoxKeptUntilSent(t *testing.T) {
	b := NewBook(Store{}, 0)
	chip := plugin.Box{Text: "fix [Pasted text #1 +4 lines]", Cursor: 3, Pastes: map[int]string{1: "a\nb\nc\nd"}}
	b.Changed("s1", chip)
	set, ok := b.Opened("s1")
	if !ok || set.If != "" || set.Box.Cursor != 3 || set.Box.Pastes[1] == "" {
		t.Fatalf("opened: %+v %v", set, ok)
	}
	b.Sent("s1", "one", "fix a b c d", t0)
	if _, ok := b.Opened("s1"); ok {
		t.Fatal("a sent box shouldn't come back")
	}
	if len(b.S.History) != 1 || b.S.History[0].Kind != Sent || b.S.History[0].Box.Pastes[1] == "" {
		t.Fatalf("sent should be kept with its chips: %+v", b.S.History)
	}
	b.Changed("s1", box("  "))
	if _, ok := b.Opened("s1"); ok {
		t.Fatal("an emptied box shouldn't come back")
	}
}

// The stash key sets a message aside, brings it back, or swaps; sending
// brings it back by itself.
func TestStash(t *testing.T) {
	b := NewBook(Store{}, 0)
	if r := b.Stash("s1", "one", box(""), t0); r.Set != nil {
		t.Fatal("nothing to stash")
	}
	r := b.Stash("s1", "one", box("first"), t0)
	if r.Set == nil || r.Set.Box.Text != "" || r.Set.If != "first" || b.Note("s1") == "" {
		t.Fatalf("stash: %+v", r)
	}
	r = b.Stash("s1", "one", box("second"), t0)
	if r.Set == nil || r.Set.Box.Text != "first" || r.Set.If != "second" || b.S.Stashes["s1"].Box.Text != "second" {
		t.Fatalf("swap: %+v", r)
	}
	b.Changed("s1", box("third"))
	set, ok := b.Sent("s1", "one", "third", t0)
	if !ok || set.Box.Text != "second" || set.If != "" || b.Note("s1") != "" {
		t.Fatalf("after sending: %+v %v", set, ok)
	}
	b.Stash("s1", "one", box("fourth"), t0)
	r = b.Stash("s1", "one", box(""), t0)
	if r.Set == nil || r.Set.Box.Text != "fourth" || len(b.S.Stashes) != 0 {
		t.Fatalf("back on the key: %+v", r)
	}
}

// Putting one back keeps what the box held; forgetting drops it.
func TestPutBackAndForget(t *testing.T) {
	b := NewBook(Store{}, 0)
	b.Changed("s1", box("wiped"))
	b.Cleared("s1", "one", "wiped", t0)
	b.Stash("s2", "two", box("aside"), t0)
	p := b.Pick("s1", t0.Add(10*time.Second))
	if p.Tab != 3 || len(p.Items) != 2 || p.Items[0].ID != "stash:s2" || p.Items[1].Tab != 3 {
		t.Fatalf("pick: %+v", p)
	}
	if _, err := plugin.CleanPick(p); err != nil {
		t.Fatalf("the pick should be one rush takes: %v", err)
	}
	cur := box("half typed")
	set, ok := b.PutBack(p.Items[1].ID, "s1", "one", &cur, t0)
	if !ok || set.Box.Text != "wiped" || set.If != "half typed" || b.S.History[0].Kind != Kept {
		t.Fatalf("put back: %+v, %+v", set, b.S.History)
	}
	set, ok = b.PutBack("stash:s2", "s1", "one", nil, t0)
	if !ok || set.Box.Text != "aside" || set.If != "" || len(b.S.Stashes) != 0 {
		t.Fatalf("a stash from another box: %+v", set)
	}
	b.Forget(b.S.History[0].ID)
	if len(b.S.History) != 1 {
		t.Fatalf("forget: %+v", b.S.History)
	}
}

// The same text sent twice is kept once; each kind keeps its newest.
func TestKeepsNewest(t *testing.T) {
	b := NewBook(Store{}, 2)
	for i, s := range []string{"a", "b", "a", "c"} {
		b.Changed("s1", box(s))
		b.Sent("s1", "", s, t0.Add(time.Duration(i)*time.Minute))
	}
	if len(b.S.History) != 2 || b.S.History[0].Box.Text != "c" || b.S.History[1].Box.Text != "a" {
		t.Fatalf("history: %+v", b.S.History)
	}
}

// The first run takes in the drafts rush kept itself.
func TestLoadTakesInOldDrafts(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "plugin-data", "drafts")
	_ = os.MkdirAll(data, 0o700)
	old := `[{"text":"older","at":"2026-09-01T00:00:00Z","kind":"draft"},{"text":"newer","at":"2026-09-02T00:00:00Z","sent":true}]`
	_ = os.WriteFile(filepath.Join(root, "drafts.json"), []byte(old), 0o600)
	s := load(filepath.Join(data, "drafts.json"), data)
	if len(s.History) != 2 || s.History[0].Box.Text != "newer" || s.History[0].Kind != Sent || s.History[1].Kind != Kept {
		t.Fatalf("history: %+v", s.History)
	}
}
