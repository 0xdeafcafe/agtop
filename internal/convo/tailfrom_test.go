package convo

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// A transcript read from part way through a turn, whose tool result's call
// was cut off, draws its last turn as the whole transcript does.
func TestTailFromMidTurn(t *testing.T) {
	lines := []string{
		`{"type":"user","timestamp":"2026-09-23T20:00:00Z","message":{"role":"user","content":"first question"}}`,
		`{"type":"assistant","timestamp":"2026-09-23T20:00:01Z","message":{"id":"m1","role":"assistant","content":[{"type":"text","text":"first answer"}]}}`,
		`{"type":"user","timestamp":"2026-09-23T20:01:00Z","message":{"role":"user","content":"second question"}}`,
		`{"type":"assistant","timestamp":"2026-09-23T20:01:01Z","message":{"id":"m2","role":"assistant","content":[{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"go vet ./..."}}]}}`,
		`{"type":"user","timestamp":"2026-09-23T20:01:05Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t2","content":"ok"}]},"toolUseResult":{"stdout":"ok","stderr":""}}`,
		`{"type":"assistant","timestamp":"2026-09-23T20:01:06Z","message":{"id":"m3","role":"assistant","content":[{"type":"text","text":"second answer"}]}}`,
		`{"type":"user","timestamp":"2026-09-23T20:02:00Z","message":{"role":"user","content":"third question"}}`,
		`{"type":"assistant","timestamp":"2026-09-23T20:02:01Z","message":{"id":"m4","role":"assistant","content":[{"type":"tool_use","id":"t4","name":"Bash","input":{"command":"go test ./..."}}]}}`,
		`{"type":"user","timestamp":"2026-09-23T20:02:09Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t4","content":"PASS"}]},"toolUseResult":{"stdout":"PASS","stderr":""}}`,
		`{"type":"assistant","timestamp":"2026-09-23T20:02:10Z","message":{"id":"m5","role":"assistant","content":[{"type":"text","text":"third answer"}]}}`,
	}
	path := t.TempDir() + "/s.jsonl"
	all := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(all), 0o644); err != nil {
		t.Fatal(err)
	}
	o := Options{Width: 100, Open: map[string]bool{}, Now: time.Now()}
	whole := NewTail(path)
	_, _ = whole.Read()
	want := whole.Sess.Render(o)

	// Cut inside t2's call: its result arrives with nothing to land on.
	cut := strings.Index(all, `"tool_use","id":"t2"`)
	tl := NewTailFrom(path, int64(len(all)-cut))
	if _, err := tl.Read(); err != nil {
		t.Fatal(err)
	}
	s := tl.Sess
	if !s.Partial || len(s.Turns) != 2 || s.Turns[0].Live || s.Turns[1].Prompt != "third question" {
		t.Fatalf("partial=%v turns=%d", s.Partial, len(s.Turns))
	}
	got := s.Render(o)
	if ansi.Strip(got[0].Text) != "  reading earlier…" {
		t.Errorf("top: %q", got[0].Text)
	}
	last := len(whole.Sess.Turns[len(whole.Sess.Turns)-1].Items) + 1
	for i := 1; i <= last; i++ {
		if g, w := ansi.Strip(got[len(got)-i].Text), ansi.Strip(want[len(want)-i].Text); g != w {
			t.Errorf("%d up: %q, want %q", i, g, w)
		}
	}

	// A cut that falls between two lines keeps the one after it.
	at := strings.Index(all, `{"type":"user","timestamp":"2026-09-23T20:02:00Z"`)
	tl = NewTailFrom(path, int64(len(all)-at))
	_, _ = tl.Read()
	if len(tl.Sess.Turns) != 1 || tl.Sess.Turns[0].Prompt != "third question" {
		t.Errorf("at a line's start: %d turns", len(tl.Sess.Turns))
	}
}
