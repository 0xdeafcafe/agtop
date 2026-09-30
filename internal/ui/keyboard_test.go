package ui

import (
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

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

// Typing poppers or alex on Keys bursts the keyboard, and the letters that
// would add, clear or reset a key only spell the word.
func TestKeyboardBursts(t *testing.T) {
	for _, word := range kbWords {
		t.Setenv("RUSH_HOME", t.TempDir())
		m, _ := benchModel(130, 64)
		m.setView(placeSettings)
		m.setSettingsPage(pageKeys)
		id := m.keyRows()[m.dialog.cursor].ID
		m.setKeys(keymap.File{Bindings: map[string][]string{id: {"ctrl+x z"}}})
		pressKeys(m, strings.Split(word, "")...)
		if m.dialog.boom.IsZero() || m.keys.page.taking != "" || m.keys.capture != nil {
			t.Fatalf("%s: boom %v, taking %q", word, m.dialog.boom, m.keys.page.taking)
		}
		if got := m.keys.file.Bindings[id]; len(got) != 1 || got[0] != "ctrl+x z" {
			t.Errorf("%s changed %s's keys: %v", word, id, got)
		}
		for _, at := range []time.Duration{0, 300 * time.Millisecond, time.Second, 2 * time.Second, 3 * time.Second} {
			m.dialog.boom = time.Now().Add(-at)
			body := m.dialogBody(m.w - 6)
			for _, l := range body[len(body)-9:] { // the keyboard, its cheer and the keys line
				if ansi.StringWidth(l) > m.w-6 {
					t.Fatalf("%s at %v: a line past the page: %q", word, at, ansi.Strip(l))
				}
			}
			if !strings.Contains(ansi.Strip(strings.Join(body, "\n")), strings.ToUpper(word[:1])+" ") {
				t.Errorf("%s at %v: no cheer", word, at)
			}
		}
		m.dialog.boom = time.Now().Add(-kbBoomLen)
		if out := ansi.Strip(strings.Join(m.dialogBody(m.w-6), "\n")); !strings.Contains(out, "q   w   e   r") || !strings.Contains(out, "bound here") {
			t.Errorf("%s: the keyboard isn't back together:\n%s", word, out)
		}
	}
}
