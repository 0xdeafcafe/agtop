package cellw

import (
	"math/rand"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

var pieces = []string{"a", "Z", " ", "~", "\x7f", "\t", "\n", "\x1b[0m", "\x1b[38;2;1;2;3m", "\x1b]8;;http://x\x1b\\", "\x1b[4m",
	"▏", "✓", "✻", "…", "─", "é", "́", "👍", "👍🏽", "👨‍👩‍👧", "‍", "中", "1️⃣", "️", "🇬🇧", "؀", "é", "\r\n", "\x00"}

func TestMatchesAnsi(t *testing.T) {
	for c := 0; c < 0x80; c++ {
		for _, p := range pieces {
			for _, s := range []string{string(rune(c)) + p, p + string(rune(c)), "x" + string(rune(c))} {
				if got, want := String(s), ansi.StringWidth(s); got != want {
					t.Fatalf("%q: %d, want %d", s, got, want)
				}
			}
		}
	}
	r := rand.New(rand.NewSource(1))
	for n := 0; n < 200000; n++ {
		s := ""
		for k := r.Intn(12); k >= 0; k-- {
			s += pieces[r.Intn(len(pieces))]
		}
		if got, want := String(s), ansi.StringWidth(s); got != want {
			t.Fatalf("%q: %d, want %d", s, got, want)
		}
	}
}

func BenchmarkString(b *testing.B) {
	s := "\x1b[38;2;168;162;152m▏      Looking at how the \x1b[1mpane\x1b[0m\x1b[38;2;168;162;152m draws its rows and where the wrap happens; see render.go.\x1b[0m"
	b.Run("cellw", func(b *testing.B) {
		for b.Loop() {
			String(s)
		}
	})
	b.Run("ansi", func(b *testing.B) {
		for b.Loop() {
			ansi.StringWidth(s)
		}
	})
}
