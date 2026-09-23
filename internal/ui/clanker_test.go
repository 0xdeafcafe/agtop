package ui

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestClankerShape(t *testing.T) {
	for md := moodIdle; md <= moodSleepy; md++ {
		for tick := 0; tick < 12; tick++ {
			lines := clanker(md, tick)
			if len(lines) < 3 || len(lines) > 4 {
				t.Fatalf("mood %d tick %d: %d lines", md, tick, len(lines))
			}
			w := ansi.StringWidth(lines[0])
			if w > 12 {
				t.Fatalf("mood %d tick %d: width %d > 12", md, tick, w)
			}
			for i, l := range lines {
				if got := ansi.StringWidth(l); got != w {
					t.Fatalf("mood %d tick %d line %d: width %d, want %d", md, tick, i, got, w)
				}
			}
		}
	}
}

func TestClankerSheet(t *testing.T) {
	out := os.Getenv("CLK_SHEET")
	if out == "" {
		t.Skip("set CLK_SHEET to a file path to render the contact sheet")
	}
	names := []string{"idle", "working", "needs you", "spendy", "sleepy"}
	var sb strings.Builder
	{
		for md := moodIdle; md <= moodSleepy; md++ {
			frames := make([][]string, 6)
			for tick := range frames {
				frames[tick] = clanker(md, tick)
			}
			for row := range frames[0] {
				label := "          "
				if row == 1 {
					label = fit(dim(names[md]), 10)
				}
				sb.WriteString(label)
				for _, f := range frames {
					sb.WriteString(f[row] + "   ")
				}
				sb.WriteString("\n")
			}
			sb.WriteString("\n")
		}
	}
	if err := os.WriteFile(out, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}
