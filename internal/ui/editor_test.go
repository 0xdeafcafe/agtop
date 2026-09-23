package ui

import (
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
