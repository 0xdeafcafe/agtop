package convo

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// A link wrapped over rows is closed at each row's end and opened again
// at the next's, so no row leaves the terminal half a link.
func TestWrapCarriesLinks(t *testing.T) {
	s := inline("The UI is https://app.visualdiff-check.langwatch.localhost, the API http://127.0.0.1:61371 for haven", cText)
	rows := wrap(s, 40)
	if len(rows) < 3 {
		t.Fatalf("want the link wrapped, got %q", rows)
	}
	for _, r := range rows {
		if openLink("", r) != "" {
			t.Errorf("row ends inside a link: %q", r)
		}
	}
	for _, r := range rows {
		if strings.HasPrefix(ansi.Strip(r), "check.langwatch") && !strings.HasPrefix(r, "\x1b]8;;https://app.visualdiff-check.langwatch.localhost\x1b\\") {
			t.Errorf("the link's second row should open it again: %q", r)
		}
	}
}

// Each agent spins as its own program does, Ollama in any harness as
// Ollama, and a stretch of thinking keeps one word for it.
func TestSpinAndMusing(t *testing.T) {
	if Spin("", 3) != spinner[3] || Spin("codex", 0) != "⠋" || Spin("ollama-codex", 1) != "◓" {
		t.Errorf("spinners: %q %q %q", Spin("", 3), Spin("codex", 0), Spin("ollama-codex", 1))
	}
	t0 := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	if a, b := pick(musings, t0), pick(musings, t0); a != b || a == "" {
		t.Error("the word should hold for the stretch")
	}
}

// A URL runs to a space, and takes a bracket it opened: a <port> or a
// wiki's _(film) is the URL's, a <…> or (…) round the URL isn't.
func TestURLLen(t *testing.T) {
	for in, want := range map[string]string{
		"http://127.0.0.1:<port> -scenario": "http://127.0.0.1:<port>",
		"<https://x.dev>":                   "https://x.dev",
		"(see https://x.dev/a_(b))":         "https://x.dev/a_(b)",
		"(see https://x.dev).":              "https://x.dev",
		"go to https://x.dev.":              "https://x.dev",
	} {
		if got := URLIn(in); got != want {
			t.Errorf("URLIn(%q) = %q, want %q", in, got, want)
		}
	}
}
