package convo

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestWrapCarriesStyleOntoEveryRow(t *testing.T) {
	s := paint(cText, "one two three "+paint(cBlue, "code")+cText+" four five six seven eight nine ten")
	rows := wrap(s, 12)
	if len(rows) < 3 {
		t.Fatalf("want several rows, got %q", rows)
	}
	for i, r := range rows[1:] {
		if !strings.HasPrefix(r, cText) {
			t.Errorf("row %d %q doesn't open in the paragraph's colour", i+1, r)
		}
	}
	if got, want := ansi.Strip(strings.Join(rows, " ")), ansi.Strip(s); strings.Join(strings.Fields(got), " ") != want {
		t.Errorf("text changed: %q", got)
	}
}

func TestOpenStyle(t *testing.T) {
	for _, c := range []struct{ open, s, want string }{
		{"", "plain", ""},
		{"", "\x1b[1mx", "\x1b[1m"},
		{"\x1b[1m", "x\x1b[0my", ""},
		{"\x1b[1m", "x\x1b[my", ""},
		{"\x1b[1m", "\x1b[0;2mx", "\x1b[0;2m"},
		{"", "\x1b[1m\x1b[38;2;1;2;3mx", "\x1b[1m\x1b[38;2;1;2;3m"},
		{"", "\x1b]8;;http://x\x1b\\x", ""},
	} {
		if got := openStyle(c.open, c.s); got != c.want {
			t.Errorf("openStyle(%q, %q) = %q, want %q", c.open, c.s, got, c.want)
		}
	}
}
