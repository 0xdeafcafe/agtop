package convo

import (
	"math/rand"
	"regexp"
	"strings"
	"testing"
)

var (
	boldRe = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	codeRe = regexp.MustCompile("`([^`]+)`")
	urlRe  = regexp.MustCompile(`https?://[^\s)>\]"'` + "`" + `]+`)
	ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)
)

// The byte scans draw exactly what the regular expressions they replace did.
func TestInlineMatchesRegexp(t *testing.T) {
	old := func(s, base string) string {
		s = boldRe.ReplaceAllString(s, bold+"$1"+reset+base)
		s = codeRe.ReplaceAllString(s, cWhite+"$1"+reset+base)
		return urlRe.ReplaceAllStringFunc(s, func(u string) string { return reset + link(u) + base })
	}
	pieces := []string{"**", "*", "`", "a", "bc", " ", "\n", "\t", "\v", "http", "https", "://", "s", ":", "/", "x.com", ")", ">", "]", "\"", "'",
		"\x1b[1m", "\x1b[0m", "\x1b[38;2;1;2;3m", "\x1b", "[", "m", ";", "9", "é", "中", "\xff", "__"}
	r := rand.New(rand.NewSource(2))
	for n := 0; n < 300000; n++ {
		s := ""
		for k := r.Intn(14); k >= 0; k-- {
			s += pieces[r.Intn(len(pieces))]
		}
		if got, want := inline(s, cSub), old(s, cSub); got != want {
			t.Fatalf("inline %q:\n got %q\nwant %q", s, got, want)
		}
		if got, want := stripANSI(s), ansiRe.ReplaceAllString(s, ""); got != want {
			t.Fatalf("stripANSI %q: got %q want %q", s, got, want)
		}
	}
}

func TestShortcutsMatch(t *testing.T) {
	pieces := []string{"**", "*", "`", "__", "#", ">", "-", " ", "  ", "\n", "\t", "\r", "a", "word", "é", " ", " ", "\v"}
	r := rand.New(rand.NewSource(4))
	for n := 0; n < 300000; n++ {
		s := ""
		for k := r.Intn(12); k >= 0; k-- {
			s += pieces[r.Intn(len(pieces))]
		}
		if got, want := firstPlain(s), firstLine(stripMarkdown(s)); got != want {
			t.Fatalf("firstPlain %q: %q want %q", s, got, want)
		}
		if got, want := oneLine(s), strings.Join(strings.Fields(s), " "); got != want {
			t.Fatalf("oneLine %q: %q want %q", s, got, want)
		}
	}
}
