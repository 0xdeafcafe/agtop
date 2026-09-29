package host

import (
	"testing"

	"github.com/0xdeafcafe/agtop/internal/agent/event"
	"github.com/0xdeafcafe/agtop/internal/agent/tool"
)

// Claude Code's own lines read as agtop's events, a result by the call it
// answers; the host's own lines read as they always have.
func TestDecoderReadsClaudeAsAgtop(t *testing.T) {
	var d Decoder
	lines := []string{
		`{"type":"assistant","message":{"id":"m1","role":"assistant","content":[{"type":"tool_use","id":"b1","name":"Bash","input":{"command":"ls"}}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"b1","content":"Exit code 2\nno","is_error":true}]}}`,
		`{"type":"agtop_answered","request_id":"r1"}`,
	}
	var got []any
	for _, l := range lines {
		evs, err := d.Decode([]byte(l))
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, evs...)
	}
	if len(got) != 3 {
		t.Fatalf("got %d events: %+v", len(got), got)
	}
	if m, ok := got[0].(event.Message); !ok || m.Parts[0].Call.Kind != tool.Shell || m.Parts[0].Call.Input.Command != "ls" {
		t.Errorf("call: %+v", got[0])
	}
	if m, ok := got[1].(event.Message); !ok || m.Parts[0].Output.Exit == nil || *m.Parts[0].Output.Exit != 2 {
		t.Errorf("result: %+v", got[1])
	}
	if a, ok := got[2].(Answered); !ok || a.ID != "r1" {
		t.Errorf("host line: %+v", got[2])
	}
}
