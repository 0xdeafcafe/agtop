package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Lines as Claude Code writes them: a run the turn waits on, two launched
// in the background (one of which starts a run of its own), one notice
// telling of two ending, and a message that wakes one again.
func TestSubagentRunsFromTheTranscripts(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "s.jsonl")
	subs := filepath.Join(dir, "s", "subagents")
	os.MkdirAll(subs, 0o755)
	now := time.Now()
	run := func(id, call string, depth int, quiet time.Duration, lines ...string) {
		meta := `{"agentType":"general-purpose","toolUseId":"` + call + `","spawnDepth":` + string(rune('0'+depth)) + `}`
		os.WriteFile(filepath.Join(subs, "agent-"+id+".meta.json"), []byte(meta), 0o644)
		p := filepath.Join(subs, "agent-"+id+".jsonl")
		os.WriteFile(p, []byte(strings.Join(append([]string{`{"type":"user","isSidechain":true}`}, lines...), "\n")+"\n"), 0o644)
		os.Chtimes(p, now.Add(-quiet), now.Add(-quiet))
	}
	use := func(id, name, input string) string {
		return `{"type":"assistant","timestamp":"2026-09-27T20:00:00Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"` + id + `","name":"` + name + `","input":` + input + `}]}}`
	}
	async := func(id, agent string) string {
		return `{"type":"user","timestamp":"2026-09-27T20:00:01Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"` + id + `","content":[{"type":"text","text":"Async agent launched successfully.\nagentId: ` + agent + `"}]}],"toolUseResult":{"isAsync":true,"status":"async_launched","agentId":"` + agent + `"}}`
	}
	write := func(lines ...string) {
		f, _ := os.OpenFile(main, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		f.WriteString(strings.Join(lines, "\n") + "\n")
		f.Close()
	}
	run("fg", "t1", 1, 10*time.Minute)
	run("bg1", "t2", 1, 5*time.Minute, use("t4", "Agent", `{"description":"deeper"}`))
	run("bg2", "t3", 1, 5*time.Minute)
	run("deep", "t4", 2, 5*time.Minute)
	run("old", "t9", 1, time.Hour)
	write(`{"type":"user","message":{"role":"user","content":"go"}}`,
		use("t1", "Agent", `{"description":"waited on"}`),
		use("t2", "Agent", `{"description":"one","run_in_background":true}`), async("t2", "bg1"),
		use("t3", "Task", `{"description":"two","run_in_background":true}`), async("t3", "bg2"),
		use("t9", "Agent", `{"description":"forgotten"}`))

	var r SubagentRuns
	going := func(id string) (bool, string) {
		for _, x := range r.Update(main) {
			if x.ID == id {
				return r.Going(x.ID, x.ToolUseID, x.Mod, time.Now())
			}
		}
		t.Fatalf("no run %s", id)
		return false, ""
	}
	for _, id := range []string{"fg", "bg1", "bg2", "deep"} {
		if g, _ := going(id); !g {
			t.Errorf("%s: quiet for minutes with no word it ended, yet not running", id)
		}
	}
	if g, _ := going("old"); g {
		t.Error("a run silent past RunStale still counts as running")
	}
	if st := r.Stats(main, time.Now()); st.Spawned != 5 || st.Direct != 3 || st.Nested != 1 {
		t.Errorf("stats %+v", st)
	}

	// The deeper run answers the run that started it.
	f, _ := os.OpenFile(filepath.Join(subs, "agent-bg1.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"type":"user","isSidechain":true,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t4","content":"found it"}]}}` + "\n")
	f.Close()
	os.Chtimes(filepath.Join(subs, "agent-bg1.jsonl"), now.Add(-5*time.Minute), now.Add(-5*time.Minute))
	if g, how := going("deep"); g || how != "completed" {
		t.Errorf("deep after its answer: going %v, %q", g, how)
	}

	// The one waited on answers; one notice tells of both background runs.
	write(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"done"}]}}`,
		`{"type":"queue-operation","operation":"enqueue","timestamp":"2026-09-27T20:05:00Z","content":"<task-notification>\n<task-id>bg1</task-id>\n<task-id>bg2</task-id>\n<status>stopped</status>\n<summary>2 background agents didn't finish</summary>\n</task-notification>"}`)
	for _, id := range []string{"fg", "bg1", "bg2"} {
		if g, how := going(id); g || how == "" {
			t.Errorf("%s: after its end: going %v, %q", id, g, how)
		}
	}
	// A message wakes one; it runs until its next notice.
	write(use("t5", "SendMessage", `{"to":"bg2","message":"carry on"}`))
	if g, _ := going("bg2"); !g {
		t.Error("a woken run isn't running")
	}
	write(`{"type":"user","timestamp":"2026-09-27T20:09:00Z","message":{"role":"user","content":"<task-notification>\n<task-id>bg2</task-id>\n<tool-use-id>t3</tool-use-id>\n<status>completed</status>\n</task-notification>"}}`)
	if g, how := going("bg2"); g || how != "completed" {
		t.Errorf("after its second end: going %v, %q", g, how)
	}
}
