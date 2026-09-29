package ui

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// statLook asks the disk at once, as tests of what a path is want.
func statLook(p string) pathFact { return readPath(p) }

// A paste naming files is taken in only once the disk has said what they
// are, off the UI goroutine: a dropped image then becomes an attachment.
func TestPasteAsksTheDiskFirst(t *testing.T) {
	p := filepath.Join(t.TempDir(), "shot.png")
	_ = os.WriteFile(p, []byte("x"), 0o644)
	m, _ := benchModel(120, 40)
	m.paneFocus = false
	_, cmd := m.update(tea.PasteMsg{Content: p})
	if len(m.images) != 0 || len(m.input) != 0 || cmd == nil {
		t.Fatalf("the paste was taken in before its path was looked at: %q %v", string(m.input), m.images)
	}
	apply, ok := cmd().(applyMsg)
	if !ok {
		t.Fatal("the look should land as an applyMsg")
	}
	again := apply.applyTo(m)()
	m.update(again)
	if len(m.images) != 1 || m.images[0] != p {
		t.Fatalf("the dropped image wasn't attached: %v", m.images)
	}
}
