package termimg

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/rush/internal/cellw"
)

func TestDetect(t *testing.T) {
	for _, c := range []struct {
		env  map[string]string
		want Protocol
	}{
		{map[string]string{"TERM_PROGRAM": "ghostty", "TERM": "xterm-ghostty"}, Kitty},
		{map[string]string{"TERM": "xterm-kitty"}, Kitty},
		{map[string]string{"TERM_PROGRAM": "ghostty", "TMUX": "/tmp/x"}, Blocks},
		{map[string]string{"TERM_PROGRAM": "iTerm.app"}, ITerm2},
		{map[string]string{"TERM": "foot"}, Sixel},
		{map[string]string{"TERM": "xterm-256color"}, Blocks},
		{map[string]string{"TERM": "xterm-256color", "RUSH_GRAPHICS": "kitty"}, Kitty},
	} {
		if got := Detect(func(k string) string { return c.env[k] }); got != c.want {
			t.Errorf("%v: %v, want %v", c.env, got, c.want)
		}
	}
}

// A picture keeps its shape in cells of any size, and isn't blown up.
func TestFit(t *testing.T) {
	for _, c := range []struct{ pw, ph, mc, mr, cw, ch, cols, rows int }{
		{1600, 900, 100, 40, 10, 20, 100, 28}, // wide: fills the width
		{900, 1600, 100, 20, 10, 20, 23, 20},  // tall: fills the height
		{1600, 900, 100, 40, 9, 18, 100, 28},  // the same, smaller cells
		{200, 100, 100, 40, 10, 20, 20, 5},    // small: its own size
		{1000, 1000, 48, 6, 10, 20, 12, 6},    // a thumbnail: square in cells
	} {
		cols, rows := Fit(c.pw, c.ph, c.mc, c.mr, c.cw, c.ch)
		if cols != c.cols || rows != c.rows {
			t.Errorf("%dx%d in %dx%d cells of %dx%d: %dx%d, want %dx%d", c.pw, c.ph, c.mc, c.mr, c.cw, c.ch, cols, rows, c.cols, c.rows)
		}
	}
}

// A placeholder row is as wide as its cells, measured as rush and the
// renderer measure, and carries its id as a colour.
func TestPlaceholders(t *testing.T) {
	rows := Placeholders(0x010203, 7, 3)
	if len(rows) != 3 {
		t.Fatalf("%d rows", len(rows))
	}
	for _, r := range rows {
		if w, cw := ansi.StringWidth(r), cellw.String(r); w != 7 || cw != 7 {
			t.Fatalf("width %d (cellw %d): %q", w, cw, r)
		}
		if !strings.HasPrefix(r, "\x1b[38;2;1;2;3m") {
			t.Fatalf("colour: %q", r)
		}
	}
	if ID([]byte("a"), 1, 1) == ID([]byte("a"), 2, 1) || ID(nil, 0, 0) == 0 {
		t.Fatal("ids")
	}
}

func TestBlank(t *testing.T) {
	row := ansi.Strip(Placeholders(7, 3, 1)[0])
	if got := Blank("a" + row + "b"); got != "a   b" {
		t.Fatalf("%q", got)
	}
}
