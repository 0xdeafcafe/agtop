package ui

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/rush/internal/keymap"
)

// A binding lights the keys it's pressed with: its modifiers, a shifted
// character's shift and base key, and every key of a chord.
func TestKeyParts(t *testing.T) {
	for in, want := range map[string]string{
		"ctrl+enter":    "ctrl enter",
		"shift+super+z": "shift super z",
		"{":             "[ shift",
		"G":             "g shift",
		"ctrl++":        "= ctrl shift",
		"ctrl+x p":      "ctrl p x",
		"alt+down":      "alt down",
	} {
		got := slices.Sorted(maps.Keys(keyParts(keymap.Seq(strings.Fields(in)))))
		if w := strings.Fields(want); !slices.Equal(got, slices.Sorted(slices.Values(w))) {
			t.Errorf("%s lights %v, want %v", in, got, w)
		}
	}
}

// Keys draws the keyboard under its table when there's room, and the row
// under the pointer is the one drawn at that line.
func TestKeysKeyboardAndHover(t *testing.T) {
	m, _ := benchModel(130, 64)
	m.setView(placeSettings)
	m.setSettingsPage(pageKeys)
	lines := strings.Split(m.frame(m.dialogBody(m.w-6), ""), "\n")
	if !slices.ContainsFunc(lines, func(l string) bool { return strings.Contains(ansi.Strip(l), "q   w   e   r") }) {
		t.Fatal("no keyboard on a tall screen")
	}
	rows := m.keyRows()
	for y, l := range lines {
		if strings.Contains(ansi.Strip(l), rows[2].Title) {
			m.keysHover(y)
		}
	}
	if m.dialog.keyHover != 3 {
		t.Errorf("hovering %q's line found row %d", rows[2].Title, m.dialog.keyHover-1)
	}
	m.keysHover(0)
	if m.dialog.keyHover != 0 {
		t.Error("the header hovered a row")
	}
	m, _ = benchModel(130, 30)
	m.setView(placeSettings)
	m.setSettingsPage(pageKeys)
	if strings.Contains(ansi.Strip(m.frame(m.dialogBody(m.w-6), "")), "q   w   e   r") {
		t.Error("a keyboard on a short screen, where the rows need the room")
	}
}
