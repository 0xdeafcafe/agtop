package convo

import (
	"os"
	"path/filepath"
	"testing"
)

// Each turn start carries its message's uuid, which Claude Code keeps file
// checkpoints under, and says when you didn't send it.
func TestTurnStartsCarryTheMessageID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	lines := `{"type":"user","uuid":"u1","timestamp":"2026-09-01T10:00:00Z","message":{"role":"user","content":"fix the bug"}}
{"type":"assistant","uuid":"a1","timestamp":"2026-09-01T10:00:10Z","message":{"id":"m1","role":"assistant","content":[{"type":"text","text":"fixed"}],"usage":{"input_tokens":1,"output_tokens":1}}}
{"type":"user","uuid":"u2","timestamp":"2026-09-01T10:01:00Z","message":{"role":"user","content":"now the other one"}}
`
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	starts, err := TurnStarts(path)
	if err != nil || len(starts) != 2 {
		t.Fatalf("starts %+v %v", starts, err)
	}
	if starts[0].UUID != "u1" || starts[1].UUID != "u2" || starts[0].Offset != 0 || starts[1].From != "" {
		t.Fatalf("starts %+v", starts)
	}
}
