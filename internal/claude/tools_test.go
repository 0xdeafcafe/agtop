package claude

import (
	"testing"

	"github.com/0xdeafcafe/agtop/internal/agent/tool"
)

func TestCallKindsAndInputs(t *testing.T) {
	c := Call("t1", "MultiEdit", []byte(`{"file_path":"/a/x.go","edits":[{"old_string":"a","new_string":"b"},{"old_string":"c","new_string":"d","replace_all":true}]}`))
	if c.Kind != tool.Edit || c.Input.Path != "/a/x.go" || len(c.Input.Edits) != 2 || !c.Input.Edits[1].All {
		t.Errorf("MultiEdit = %+v", c)
	}
	c = Call("t2", "Bash", []byte(`{"command":"go test ./...","run_in_background":true}`))
	if c.Kind != tool.Shell || c.Input.Command != "go test ./..." || !c.Input.Background {
		t.Errorf("Bash = %+v", c)
	}
	c = Call("t3", "mcp__claude_ai_Linear__get_issue", []byte(`{}`))
	if c.Kind != tool.MCP || c.Input.Server != "Linear" || c.Input.Tool != "get_issue" {
		t.Errorf("mcp = %+v", c)
	}
	c = Call("t4", "TodoWrite", []byte(`{"todos":[{"content":"Test","activeForm":"Testing","status":"in_progress"}]}`))
	if c.Kind != tool.Todo || len(c.Input.Todos) != 1 || c.Input.Todos[0].Active != "Testing" {
		t.Errorf("TodoWrite = %+v", c)
	}
	if c = Call("t5", "ScheduleWakeup", []byte(`{}`)); c.Kind != tool.Other {
		t.Errorf("ScheduleWakeup kind = %v", c.Kind)
	}
}

func TestOutput(t *testing.T) {
	bash := Call("t1", "Bash", []byte(`{"command":"false"}`))
	o := Output(bash, "Error: Exit code 2\nboom", true, []byte(`{"stdout":"","stderr":"boom"}`))
	if o.Exit == nil || *o.Exit != 2 || o.Stderr != "boom" {
		t.Errorf("failed Bash = %+v", o)
	}
	if o = Output(bash, "Exit code 3 is fine", false, nil); o.Exit == nil || *o.Exit != 0 {
		t.Errorf("a success's exit = %v, want 0", o.Exit)
	}
	w := Call("t2", "Write", []byte(`{"file_path":"/a/new.go"}`))
	o = Output(w, "", false, []byte(`{"type":"create","structuredPatch":[{"oldStart":0,"oldLines":0,"newStart":1,"newLines":1,"lines":["+x"]}]}`))
	if !o.Created || len(o.Patches) != 1 || o.Patches[0].Lines[0] != "+x" {
		t.Errorf("Write = %+v", o)
	}
	r := Call("t3", "Read", []byte(`{"file_path":"/a/x.go"}`))
	o = Output(r, "", false, []byte(`{"file":{"numLines":10,"startLine":5,"totalLines":90}}`))
	if o.Lines == nil || *o.Lines != (tool.Span{Start: 5, Count: 10, Total: 90}) {
		t.Errorf("Read = %+v", o.Lines)
	}
}
