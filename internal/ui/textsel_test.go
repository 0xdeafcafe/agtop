package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/0xdeafcafe/agtop/internal/headless"
	"github.com/0xdeafcafe/agtop/internal/host"
	"github.com/charmbracelet/x/ansi"
)

const selAnswer = "The header logo is now the invader, and every 45 seconds it turns into the monogram and back.\n\n" +
	"- Idle: he blinks every 9s and his antennae twitch every 13s. Otherwise he stays still.\n" +
	"- Working: his legs march on alternate seconds and his eyes glance left and right."

func selSession() *convo.Session {
	s := convo.New()
	now := time.Now()
	s.Apply(host.Sent{Text: "make the logo move"}, now)
	s.Apply(headless.Message{Role: "assistant", Blocks: []headless.Block{{Type: "text", Text: selAnswer}}}, now)
	s.Apply(headless.Result{Subtype: "success"}, now)
	return s
}

// A drag over a wrapped answer copies it as written: its paragraphs whole,
// without the pane's indent or the rows it was wrapped into.
func TestSelectedTextJoinsWrappedRows(t *testing.T) {
	const w = 50
	lines := selSession().Render(convo.Options{Width: w, Now: time.Now(), Open: map[string]bool{}})
	first, last := -1, -1
	for i, l := range lines {
		p := ansi.Strip(l.Text)
		if first < 0 && strings.Contains(p, "The header logo") {
			first = i
		}
		if strings.Contains(p, "right.") {
			last = i
		}
	}
	if first < 0 || last < first {
		t.Fatalf("answer not drawn:\n%s", dump(lines))
	}
	got := selectedText(lines, cell{row: first, col: 0}, cell{row: last, col: w - 1}, w)
	want := "The header logo is now the invader, and every 45 seconds it turns into the monogram and back.\n\n" +
		"• Idle: he blinks every 9s and his antennae twitch every 13s. Otherwise he stays still.\n" +
		"• Working: his legs march on alternate seconds and his eyes glance left and right."
	if got != want {
		t.Errorf("got\n%s\nwant\n%s\ndrawn\n%s", got, want, dump(lines))
	}

	// Starting mid-row takes from there, dragging backwards the same.
	col := strings.Index(ansi.Strip(lines[first].Text), "logo")
	got = selectedText(lines, cell{row: first + 1, col: 3}, cell{row: first, col: col}, w)
	if !strings.HasPrefix(got, "logo is now") || strings.Contains(got, "\n") {
		t.Errorf("mid-row drag: %q", got)
	}
}

func TestPaintColsKeepsTheRest(t *testing.T) {
	s := "\x1b[31mhello world\x1b[0m tail"
	got := paintCols(s, 6, 11)
	if ansi.Strip(got) != "hello world tail" {
		t.Errorf("text changed: %q", ansi.Strip(got))
	}
	if !strings.Contains(got, selBlue+"world") {
		t.Errorf("selection not painted: %q", got)
	}
	if got := ansi.Strip(paintCols("ab", 0, 5)); got != "ab   " {
		t.Errorf("short row not selected out to the drag: %q", got)
	}
}

func dump(lines []convo.Line) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(ansi.Strip(l.Text))
		if l.Wrap {
			b.WriteString("  ↩")
		}
		b.WriteByte('\n')
	}
	return b.String()
}
