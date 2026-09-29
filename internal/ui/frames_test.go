package ui

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/headless"
)

// TestDumpFrames writes whole frames in a few states to $AGTOP_FRAMES,
// digits masked so the clock doesn't show: diff two dumps to check a
// change drew nothing differently. A frame that isn't the shell at all
// (the wrong size, a row past the edge, the header gone) fails it there, so
// a broken build is found before a diff is read.
func TestDumpFrames(t *testing.T) {
	out := os.Getenv("AGTOP_FRAMES")
	if out == "" {
		t.Skip("set AGTOP_FRAMES=<file> to dump frames")
	}
	// Digits in the text, not in the escapes: a colour that changed shows.
	digits := regexp.MustCompile(`\x1b\[[0-9;:?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|[0-9]+`)
	mask := func(s string) string {
		return digits.ReplaceAllStringFunc(s, func(m string) string {
			if m[0] == 0x1b {
				return m
			}
			return "#"
		})
	}
	var b strings.Builder
	frame := func(name string, m *Model) {
		m.Update(hostLinesMsg{key: m.host.key, lines: [][]byte{deltaLine}})
		v := m.View().Content
		rows := strings.Split(v, "\n")
		if len(rows) != m.h {
			t.Fatalf("%s: %d rows, want %d", name, len(rows), m.h)
		}
		for i, r := range rows {
			if w := ansi.StringWidth(r); w > m.w {
				t.Fatalf("%s: row %d is %d wide, past %d:\n%s", name, i, w, m.w, ansi.Strip(r))
			}
		}
		if plain := ansi.Strip(v); !m.zen && !strings.Contains(plain, "Agents   Efficiency") {
			t.Fatalf("%s: the header is gone:\n%s", name, plain)
		}
		fmt.Fprintf(&b, "=== %s\n%s\n", name, mask(v))
	}
	for _, sz := range [][2]int{{80, 30}, {120, 40}, {170, 50}, {250, 70}} {
		name := fmt.Sprintf("%dx%d", sz[0], sz[1])
		m, _ := benchModel(sz[0], sz[1])
		frame(name, m)
		m.paneFocus = false
		frame(name+" list focus", m)
		m.input = []rune("#re")
		frame(name+" command", m)
		m.input = []rune(strings.Repeat("a long message that wraps ", 30))
		frame(name+" long input", m)
		m.images = []string{"/tmp/shot.png"}
		frame(name+" images", m)
		m.paneFocus, m.images = true, nil
		m.host.input = nil
		frame(name+" empty box", m)
		m.host.sel = "t300"
		frame(name+" selected", m)
		m.host.view = 1
		frame(name+" overview", m)
		m.host.view = 2
		frame(name+" changes", m)
		m.host.view = 0
		m.host.verbose = true
		frame(name+" verbose", m)
		m.host.verbose, m.host.sel = false, ""
		ask(m)
		frame(name+" question", m)
		m.host.cardFocus = true
		frame(name+" question focused", m)
		m.questionKey(m.host, m.host.sess.Pending()[0].Approval.Question, "1", true) // on to the multi-select
		m.questionKey(m.host, m.host.sess.Pending()[0].Approval.Question, "2", true)
		frame(name+" question multi", m)
		m.host.qCursor = 4 // past the options and "Something else"
		frame(name+" question continue", m)
		m.zen = true
		frame(name+" zen", m)
	}
	if err := os.WriteFile(out, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ask has the live turn wait on Claude's questions: one single choice, one
// multi-select.
func ask(m *Model) {
	in := benchJSON(map[string]any{"questions": []map[string]any{
		{"question": "Which rules first?", "header": "Lint", "options": []map[string]any{
			{"label": "no-floating-promises", "description": "Catches the awaits we forget"}, {"label": "explicit return types"}}},
		{"question": "Which packages?", "header": "Scope", "multiSelect": true, "options": []map[string]any{
			{"label": "mcp"}, {"label": "skills"}, {"label": "web"}}},
	}})
	s := m.host.sess
	s.Apply(headless.Message{Role: "assistant", Blocks: []headless.Block{{Type: "tool_use", ID: "ask1", Name: "AskUserQuestion", Input: in}}}, time.Now())
	s.Apply(headless.PermissionRequest{ID: "q1", Tool: "AskUserQuestion", Input: in, ToolUseID: "ask1"}, time.Now())
}
