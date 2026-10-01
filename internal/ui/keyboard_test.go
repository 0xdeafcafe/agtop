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

// The action table stays primary at all terminal sizes and its hit targets
// follow its actual rendered rows, including when the list is scrolled.
func TestKeysPageResponsive(t *testing.T) {
	for _, size := range [][2]int{{130, 64}, {100, 30}, {80, 24}, {58, 24}, {64, 24}, {44, 24}} {
		m, _ := benchModel(size[0], size[1])
		m.setView(placeSettings)
		m.setSettingsPage(pageKeys)
		m.showKey("session.last")
		body := m.dialogBody(m.w - 6)
		text := ansi.Strip(strings.Join(body, "\n"))
		for _, want := range []string{"ACTION", "BINDING", "Edit binding", "Add alternative", "Unbind", "Reset default", "change context"} {
			if !strings.Contains(text, want) {
				t.Errorf("%v missing %q:\n%s", size, want, text)
			}
		}
		if strings.Contains(text, "q   w   e   r") {
			t.Fatal("keyboard diagram should not consume action rows")
		}
		for _, line := range body {
			if ansi.StringWidth(line) > m.w-6 {
				t.Fatalf("%v overflow: %q", size, ansi.Strip(line))
			}
		}
		if len(body) > m.h-len(m.header())-4 {
			t.Fatalf("%v clips footer: %d lines", size, len(body))
		}
		lines := strings.Split(m.frame(body, ""), "\n")
		selected := m.keyRows()[m.dialog.cursor]
		found := false
		for y, l := range lines {
			if strings.Contains(l, selBG) {
				found = true
				m.keysHover(y)
				if m.dialog.keyHover != m.dialog.cursor+1 {
					t.Fatalf("%v hover %d != selected %d", size, m.dialog.keyHover, m.dialog.cursor+1)
				}
			}
		}
		if !found {
			t.Fatalf("%v selected action not visible: %s", size, selected.Title)
		}
		m.keysHover(0)
		if m.dialog.keyHover != 0 {
			t.Fatal("header hovered an action")
		}
	}
}

// Previously the hidden animation intercepted chord letters. Recording a
// shortcut now treats every letter literally, while practice still finds it.
func TestKeysCaptureAndPracticeWithoutAnimation(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	m, _ := benchModel(100, 30)
	m.setView(placeSettings)
	m.setSettingsPage(pageKeys)
	m.showKey("list.pr")
	pressKeys(m, "enter", "ctrl+alt+z", "a", "l")
	if got := m.keyMap().KeyText("list.pr"); got != "ctrl+alt+z a l" {
		t.Fatalf("chord capture lost letters: %q", got)
	}
	m.showKey("session.last")
	m.practiceKey("ctrl+alt+z")
	m.practiceKey("a")
	m.practiceKey("l")
	if got := m.keyRows()[m.dialog.cursor].ID; got != "list.pr" {
		t.Fatalf("practice selected %s", got)
	}
}

func TestKeysPageShowsFullSelectionAndProblem(t *testing.T) {
	m, _ := benchModel(58, 30)
	m.setView(placeSettings)
	m.setSettingsPage(pageKeys)
	m.showKey("session.send")
	text := ansi.Strip(strings.Join(m.keysBody(m.w-6), " "))
	text = strings.Join(strings.Fields(text), " ")
	a := m.keyRows()[m.dialog.cursor]
	if !strings.Contains(text, a.Title) {
		t.Fatalf("full action is missing: %s", text)
	}
	for _, seq := range m.keyMap().Keys(a.ID) {
		if !strings.Contains(text, seq.String()) {
			t.Fatalf("alternative %s is missing: %s", seq.String(), text)
		}
	}
	m.setKeys(keymap.File{Bindings: map[string][]string{"missing.action": {"ctrl+z"}}})
	text = ansi.Strip(strings.Join(m.keysBody(m.w-6), " "))
	text = strings.Join(strings.Fields(text), " ")
	problem := strings.Join(strings.Fields(m.keyMap().Problems()[0].String()), " ")
	if !strings.Contains(text, problem) {
		t.Fatalf("invalid binding explanation is missing: %s", text)
	}
}
