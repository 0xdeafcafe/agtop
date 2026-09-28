package fswait

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWatcher(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("kqueue only")
	}
	dir := t.TempDir()
	f := filepath.Join(dir, "t.jsonl")
	_ = os.WriteFile(f, []byte("a\n"), 0o644)
	w := NewWatcher()
	defer w.Close()
	w.Watch([]string{dir, f})
	if !w.Changed() {
		t.Fatal("the first ask should say to look")
	}
	if w.Changed() {
		t.Fatal("nothing happened, but it said something changed")
	}
	fh, _ := os.OpenFile(f, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = fh.WriteString("b\n")
	fh.Close()
	if !w.Changed() {
		t.Fatal("an append wasn't noticed")
	}
	if w.Changed() {
		t.Fatal("one append counted twice")
	}
	// A file replaced by rename, as info.json is: the folder says so.
	tmp := filepath.Join(dir, "info.json.tmp")
	_ = os.WriteFile(tmp, []byte("{}"), 0o644)
	_ = os.Rename(tmp, filepath.Join(dir, "info.json"))
	if !w.Changed() {
		t.Fatal("a rename into the folder wasn't noticed")
	}
	// A watched file removed and written again is watched afresh.
	_ = os.Remove(f)
	if !w.Changed() {
		t.Fatal("removal wasn't noticed")
	}
	_ = os.WriteFile(f, []byte("c\n"), 0o644)
	w.Watch([]string{dir, f})
	w.Changed()
	fh, _ = os.OpenFile(f, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = fh.WriteString("d\n")
	fh.Close()
	if !w.Changed() {
		t.Fatal("the new file's append wasn't noticed")
	}
}
