package convo

import (
	"os"
	"strings"
	"testing"
	"time"
)

// What a start hook handed the agent is one dim line, never a message of
// yours, and opens to the whole text.
func TestStartHooksAreOneLine(t *testing.T) {
	path := t.TempDir() + "/s.jsonl"
	lines := []string{
		`{"type":"attachment","attachment":{"type":"hook_success","hookName":"SessionStart:startup","hookEvent":"SessionStart","content":"PONYTAIL MODE ACTIVE — level: full\n\nYou are a lazy senior developer."}}`,
		`{"type":"attachment","attachment":{"type":"hook_success","hookName":"SessionStart:startup","hookEvent":"SessionStart","content":"","stdout":"{\"hookSpecificOutput\":{}}"}}`,
		`{"type":"attachment","attachment":{"type":"hook_success","hookName":"PreToolUse:Bash","hookEvent":"PreToolUse","content":"not a start hook"}}`,
		`{"type":"user","timestamp":"2026-09-23T20:00:00Z","message":{"role":"user","content":"fix the test"}}`,
	}
	_ = os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	tl := NewTail(path)
	if _, err := tl.Read(); err != nil {
		t.Fatal(err)
	}
	s := tl.Sess
	if len(s.Hooks) != 1 || len(s.Turns) != 1 || s.Turns[0].Prompt != "fix the test" {
		t.Fatalf("hooks %+v turns %d", s.Hooks, len(s.Turns))
	}
	o := Options{Width: 80, Now: time.Unix(0, 0), Open: map[string]bool{}}
	out := plain(s.Render(o))
	if !strings.Contains(out, "▸ ◇ SessionStart · PONYTAIL MODE ACTIVE — level: full") || strings.Contains(out, "lazy senior") {
		t.Errorf("a hook is its first line:\n%s", out)
	}
	o.Open["t1:hook:0"] = true
	out = plain(s.Render(o))
	if !strings.Contains(out, "▾ ◇ SessionStart") || !strings.Contains(out, "You are a lazy senior developer.") {
		t.Errorf("opened it's the whole text:\n%s", out)
	}
	if strings.Count(out, "╭") != 0 {
		t.Errorf("nothing is boxed:\n%s", out)
	}
}
