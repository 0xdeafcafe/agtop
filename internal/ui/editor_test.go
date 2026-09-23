package ui

import (
	"os"
	"path/filepath"
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

func TestSelection(t *testing.T) {
	type st struct {
		buf         string
		pos, anchor int
	}
	run := func(in st, keys ...string) (st, string) {
		b, p, a := []rune(in.buf), in.pos, in.anchor
		var copied string
		for _, k := range keys {
			msg := tea.KeyPressMsg{}
			s := k
			if len(k) > 1 && k[0] == '"' {
				msg.Text, s = k[1:len(k)-1], k[1:len(k)-1]
			}
			var c string
			b, p, a, c, _ = editSel(b, p, a, msg, s)
			if c != "" {
				copied = c
			}
		}
		return st{string(b), p, a}, copied
	}
	got, _ := run(st{"hello world", 11, -1}, "shift+left", "shift+left", "shift+left", "shift+left", "shift+left")
	if got.pos != 6 || got.anchor != 11 {
		t.Fatalf("select back: %+v", got)
	}
	got, _ = run(got, `"there"`)
	if got.buf != "hello there" || got.anchor != -1 {
		t.Fatalf("typing replaces the selection: %+v", got)
	}
	got, copied := run(st{"run the tests", 0, -1}, "ctrl+shift+right", "ctrl+c")
	if copied != "run" || got.buf != "run the tests" {
		t.Fatalf("copy: %q %+v", copied, got)
	}
	got, _ = run(st{"run the tests", 13, -1}, "shift+home", "backspace")
	if got.buf != "" {
		t.Fatalf("delete selection: %+v", got)
	}
	got, _ = run(st{"abc", 1, -1}, "shift+right", "left")
	if got.anchor != -1 || got.pos != 1 {
		t.Fatalf("moving clears the selection: %+v", got)
	}
}

func TestBoxClickMapsToText(t *testing.T) {
	b := box{w: 24, text: []rune("one two three four five six"), lead: "❯ ", maxRows: 6}
	// Inner width 20, lead 2: rows of 18 cells, word wrapped.
	segs := wrapSegs(b.text, b.w-4-b.leadW())
	if len(segs) != 2 || string(b.text[segs[0].from:segs[0].to]) != "one two three " {
		t.Fatalf("wrap: %+v", segs)
	}
	// Col 0-1 is "│ ", then the lead "❯ ": col 4 is the first character.
	if p := b.at(0, 4); p != 0 {
		t.Errorf("first char: %d", p)
	}
	if p := b.at(0, 8); p != 4 {
		t.Errorf("start of 'two': %d", p)
	}
	if p := b.at(1, 4); p != segs[1].from {
		t.Errorf("second row start: %d", p)
	}
	if p := b.at(1, 99); p != len(b.text) {
		t.Errorf("past the end: %d", p)
	}
}

func TestCleanPaste(t *testing.T) {
	in := "goroutine 1:\r\n\tmain.go:78 +0x5ec\n\x1b[1mab\tc"
	want := "goroutine 1:\n    main.go:78 +0x5ec\n[1mab   c"
	if got := cleanPaste(in); got != want {
		t.Fatalf("cleanPaste = %q, want %q", got, want)
	}
}

func TestImagePathsOddNames(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "Screenshot 2026-09-23 at 23.38.19 PM.png")
	if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	esc := strings.ReplaceAll(name, " ", `\ `)
	if got := imagePaths(esc); len(got) != 1 || got[0] != name {
		t.Fatalf("escaped path with U+202F: %v", got)
	}
	u := "file://" + strings.ReplaceAll(strings.ReplaceAll(name, " ", "%20"), " ", "%E2%80%AF")
	if got := imagePaths(u); len(got) != 1 || got[0] != name {
		t.Fatalf("file URL: %v", got)
	}
}

func TestExtractImages(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "Screenshot 2026-09-24 at 00.03.09.png")
	_ = os.WriteFile(a, []byte("x"), 0o644)
	in := strings.ReplaceAll(a, " ", `\ `) + " still no image detection?\nand /var/nope.png stays"
	rest, imgs := extractImages(in)
	if len(imgs) != 1 || imgs[0] != a {
		t.Fatalf("imgs = %v", imgs)
	}
	if rest != "still no image detection?\nand /var/nope.png stays" {
		t.Fatalf("rest = %q", rest)
	}
	if r, imgs := extractImages("no images here"); imgs != nil || r != "no images here" {
		t.Fatal("plain text changed")
	}
}
