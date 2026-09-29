package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)

func TestSettleWaitsForTheBoxToStopChanging(t *testing.T) {
	b := NewBook(Store{}, 50)
	b.Changed("s1", "hel", t0)
	b.Changed("s1", "hello", t0.Add(time.Second))
	if b.Settle(t0.Add(2 * time.Second)) {
		t.Fatal("settled 1s after the last change; want to wait for Debounce")
	}
	if got := b.NextSettle(); !got.Equal(t0.Add(time.Second + Debounce)) {
		t.Fatalf("NextSettle = %v", got)
	}
	if !b.Settle(t0.Add(time.Second + Debounce)) {
		t.Fatal("didn't settle after Debounce")
	}
	if b.Autosave["s1"].Text != "hello" {
		t.Fatalf("autosave = %q, want the latest text", b.Autosave["s1"].Text)
	}
	if !b.NextSettle().IsZero() {
		t.Fatal("nothing should be pending once settled")
	}
	// The same text settling again changes nothing to write.
	b.Changed("s1", "hello", t0.Add(5*time.Second))
	if b.Settle(t0.Add(10 * time.Second)) {
		t.Fatal("rewrote an unchanged autosave")
	}
}

func TestEmptiedBoxDropsItsAutosave(t *testing.T) {
	b := NewBook(Store{}, 50)
	b.Changed("", "draft in the prompt", t0)
	b.Settle(t0.Add(Debounce))
	b.Changed("", "", t0.Add(3*time.Second))
	if !b.Settle(t0.Add(3*time.Second + Debounce)) {
		t.Fatal("emptying the box should drop the autosave")
	}
	if _, ok := b.Autosave[""]; ok {
		t.Fatal("autosave kept for an empty box")
	}
}

func TestLeavingKeepsUnsettledText(t *testing.T) {
	b := NewBook(Store{}, 50)
	b.Changed("s1", "half a thought", t0)
	// Left before it settled: the pending text is kept anyway.
	if !b.Keep("s1", "", t0.Add(100*time.Millisecond)) {
		t.Fatal("leaving with text typed kept nothing")
	}
	if b.Count("s1") != 1 || b.Drafts["s1"][0].Text != "half a thought" {
		t.Fatalf("drafts = %+v", b.Drafts["s1"])
	}
	if !b.NextSettle().IsZero() || len(b.Autosave) != 0 {
		t.Fatal("a kept box should leave nothing pending or autosaved")
	}
	// Leaving an empty box keeps nothing.
	if b.Keep("s2", "", t0) {
		t.Fatal("kept a draft for a box never typed in")
	}
}

func TestClearedTextWinsOverPending(t *testing.T) {
	b := NewBook(Store{}, 50)
	b.Changed("s1", "old", t0)
	b.Keep("s1", "what was cleared", t0.Add(time.Second))
	if got := b.Drafts["s1"][0].Text; got != "what was cleared" {
		t.Fatalf("draft = %q", got)
	}
}

func TestSentDropsThePendingAutosave(t *testing.T) {
	b := NewBook(Store{}, 50)
	b.Changed("s1", "going now", t0)
	b.Settle(t0.Add(Debounce))
	b.Changed("s1", "going now!", t0.Add(2*time.Second))
	if !b.Sent("s1") {
		t.Fatal("sending should drop the autosave")
	}
	if b.Settle(t0.Add(time.Hour)) || len(b.Autosave) != 0 {
		t.Fatal("a sent box came back as an autosave")
	}
	if b.Keep("s1", "", t0.Add(time.Hour)) {
		t.Fatal("leaving after sending kept a draft")
	}
}

func TestDraftsAreCappedPerBox(t *testing.T) {
	b := NewBook(Store{}, 10)
	for i := range 15 {
		b.Keep("s1", string(rune('a'+i)), t0.Add(time.Duration(i)*time.Second))
	}
	b.Keep("s2", "other box", t0)
	if b.Count("s1") != 10 {
		t.Fatalf("kept %d, want 10", b.Count("s1"))
	}
	if b.Drafts["s1"][0].Text != "f" || b.Drafts["s1"][9].Text != "o" {
		t.Fatalf("want the newest ten, got %q..%q", b.Drafts["s1"][0].Text, b.Drafts["s1"][9].Text)
	}
	if b.Count("s2") != 1 {
		t.Fatal("one box's cap reached into another")
	}
	// Lowering the setting trims what's kept; raising it trims nothing.
	if b.SetKeep(50) {
		t.Fatal("raising keep dropped drafts")
	}
	b2 := NewBook(b.Store, 10)
	for i := range 5 {
		b2.Keep("s1", string(rune('A'+i)), t0)
	}
	if !b2.SetKeep(3) || b2.Count("s1") != 3 || b2.Drafts["s1"][2].Text != "E" {
		t.Fatalf("after SetKeep(3): %+v", b2.Drafts["s1"])
	}
}

func TestSameDraftTwiceIsKeptOnce(t *testing.T) {
	b := NewBook(Store{}, 50)
	b.Keep("s1", "same", t0)
	b.Keep("s1", "same", t0.Add(time.Minute))
	if b.Count("s1") != 1 || !b.Drafts["s1"][0].At.Equal(t0.Add(time.Minute)) {
		t.Fatalf("drafts = %+v", b.Drafts["s1"])
	}
}

func TestRestoreWalksBackThenFallsBackToAutosave(t *testing.T) {
	b := NewBook(Store{}, 50)
	b.Keep("s1", "first", t0)
	b.Keep("s1", "second", t0.Add(time.Second))
	b.Changed("s1", "typing when rush quit", t0.Add(2*time.Second))
	b.Settle(t0.Add(time.Minute))
	for _, want := range []string{"second", "first", "typing when rush quit"} {
		got, ok := b.Restore("s1")
		if !ok || got != want {
			t.Fatalf("Restore = %q, %v; want %q", got, ok, want)
		}
	}
	if _, ok := b.Restore("s1"); ok {
		t.Fatal("restored from an empty box")
	}
	if _, ok := b.Restore(""); ok {
		t.Fatal("the Prompt has no drafts of s1's")
	}
}

func TestParseKeep(t *testing.T) {
	for in, want := range map[string]int{"10": 10, "200": 200, "": DefaultKeep, "x": DefaultKeep, "-3": DefaultKeep} {
		if got := ParseKeep(in); got != want {
			t.Errorf("ParseKeep(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "drafts.json")
	if s := load(path); len(s.Drafts) != 0 {
		t.Fatal("a missing file should load empty")
	}
	b := NewBook(Store{}, 50)
	b.Keep("", "prompt draft", t0)
	if err := save(path, b.Store); err != nil {
		t.Fatal(err)
	}
	got := NewBook(load(path), 50)
	if d, _ := got.Restore(""); d != "prompt draft" {
		t.Fatalf("after load, Restore = %q", d)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, %v; want 0600", fi.Mode().Perm(), err)
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Fatalf("left %d files behind; want only drafts.json", len(ents))
	}
	os.WriteFile(path, []byte("{broken"), 0o600)
	if s := load(path); len(s.Drafts) != 0 {
		t.Fatal("a broken file should load empty")
	}
}
