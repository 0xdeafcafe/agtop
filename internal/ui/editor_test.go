package ui

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// press types s as keys: "ctrl+left", or plain text wrapped in quotes.
func press(buf string, pos int, keys ...string) (string, int) {
	b := []rune(buf)
	for _, k := range keys {
		msg := tea.KeyPressMsg{}
		s := k
		if len(k) > 1 && k[0] == '"' {
			msg.Text, s = k[1:len(k)-1], k[1:len(k)-1]
		}
		b, pos, _ = edit(b, pos, msg, s)
	}
	return string(b), pos
}

func TestEdit(t *testing.T) {
	cases := []struct {
		name    string
		buf     string
		pos     int
		keys    []string
		want    string
		wantPos int
	}{
		{"type in the middle", "helo", 3, []string{`"l"`}, "hello", 4},
		{"word left", "run the tests", 13, []string{"ctrl+left"}, "run the tests", 8},
		{"word left twice", "run the tests", 13, []string{"alt+left", "alt+left"}, "run the tests", 4},
		{"word right", "run the tests", 0, []string{"ctrl+right"}, "run the tests", 3},
		{"delete word back", "run the tests", 13, []string{"alt+backspace"}, "run the ", 8},
		{"delete word back over spaces", "run the   ", 10, []string{"ctrl+w"}, "run ", 4},
		{"delete word forward", "run the tests", 4, []string{"alt+delete"}, "run  tests", 4},
		{"cmd+backspace clears to line start", "one\ntwo three", 13, []string{"super+backspace"}, "one\n", 4},
		{"ctrl+u at a line start joins upward", "one\ntwo", 4, []string{"ctrl+u"}, "two", 0},
		{"ctrl+k clears to the end", "run the tests", 4, []string{"ctrl+k"}, "run ", 4},
		{"home and end", "abc", 1, []string{"home", `"x"`, "end", `"y"`}, "xabcy", 5},
		{"new line", "ab", 1, []string{"shift+enter"}, "a\nb", 2},
		{"backspace at start is a no-op", "ab", 0, []string{"backspace"}, "ab", 0},
	}
	for _, c := range cases {
		got, pos := press(c.buf, c.pos, c.keys...)
		if got != c.want || pos != c.wantPos {
			t.Errorf("%s: got %q@%d, want %q@%d", c.name, got, pos, c.want, c.wantPos)
		}
	}
}

func TestCursorSurvivesReplacement(t *testing.T) {
	m := &Model{input: []rune("hello")}
	m.setCursor(2)
	if m.cursorPos() != 2 {
		t.Fatalf("pos %d", m.cursorPos())
	}
	m.input, m.back = []rune("a new name"), 0 // what rename does
	if m.cursorPos() != len(m.input) {
		t.Fatalf("cursor should be at the end after a replacement, got %d", m.cursorPos())
	}
}

func TestImagePaths(t *testing.T) {
	dir := t.TempDir()
	a := dir + "/Screen Shot 1.png"
	b := dir + "/b.jpg"
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	escaped := strings.ReplaceAll(a, " ", `\ `)
	cases := []struct {
		paste string
		want  int
	}{
		{escaped, 1},
		{"'" + a + "'", 1},
		{escaped + " " + b, 2},
		{escaped + "\n" + b + "\n", 2},
		{"look at " + b, 0},       // ordinary text stays text
		{dir + "/missing.png", 0}, // must exist
		{dir + "/notes.txt", 0},   // must be an image
	}
	for _, c := range cases {
		if got := imagePaths(c.paste); len(got) != c.want {
			t.Errorf("%q: got %v", c.paste, got)
		}
	}
}
