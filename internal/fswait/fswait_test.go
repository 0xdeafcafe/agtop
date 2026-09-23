package fswait

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGrown(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	_ = os.WriteFile(p, []byte("a\n"), 0o644)
	stop := make(chan struct{})
	got := make(chan bool, 1)
	go func() { got <- Grown(stop, []File{{p, 2}}) }()
	time.Sleep(50 * time.Millisecond)
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString("b\n")
	f.Close()
	select {
	case ok := <-got:
		if !ok {
			t.Fatal("reported no change")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a write wasn't noticed")
	}

	// Already grown before the watch: seen at once.
	if !Grown(stop, []File{{p, 2}}) {
		t.Fatal("missed growth before watching")
	}

	// Stop ends it.
	go func() { got <- Grown(stop, []File{{p, 4}}) }()
	close(stop)
	select {
	case ok := <-got:
		if ok {
			t.Fatal("reported a change on stop")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stop wasn't noticed")
	}
}
