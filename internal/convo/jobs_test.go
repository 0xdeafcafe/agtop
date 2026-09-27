package convo

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/0xdeafcafe/agtop/internal/headless"
	"github.com/0xdeafcafe/agtop/internal/host"
)

// Claude Code's own lines, from a real session: a foreground command
// moved to the background and stopped, then one started in the background.
var jobLines = []string{
	`{"type":"system","subtype":"task_started","task_id":"bomth3m0o","tool_use_id":"toolu_01","description":"for i in $(seq 1 30); do echo tick $i; sleep 1; done","is_backgrounded":false,"task_type":"local_bash"}`,
	`{"type":"system","subtype":"background_tasks_changed","tasks":[{"task_id":"bomth3m0o","task_type":"local_bash","description":"for i in $(seq 1 30); do echo tick $i; sleep 1; done"}]}`,
	`{"type":"system","subtype":"task_updated","task_id":"bomth3m0o","patch":{"is_backgrounded":true}}`,
	`{"type":"system","subtype":"background_tasks_changed","tasks":[]}`,
	`{"type":"system","subtype":"task_updated","task_id":"bomth3m0o","patch":{"status":"killed","end_time":1790412912883}}`,
	`{"type":"system","subtype":"task_notification","task_id":"bomth3m0o","tool_use_id":"toolu_01","status":"stopped","output_file":"/tmp/x/tasks/bomth3m0o.output","summary":"for i in $(seq 1 30); do echo tick $i; sleep 1; done"}`,
	`{"type":"system","subtype":"background_tasks_changed","tasks":[{"task_id":"b5hybj1hn","task_type":"local_bash","description":"tick loop progress (1-30)"}]}`,
	`{"type":"system","subtype":"task_started","task_id":"b5hybj1hn","tool_use_id":"toolu_02","description":"tick loop progress (1-30)","is_backgrounded":true,"task_type":"local_bash"}`,
}

func TestJobs(t *testing.T) {
	s := New()
	now := time.Now()
	apply := func(i int) {
		ev, err := headless.Decode([]byte(jobLines[i]))
		if err != nil {
			t.Fatal(err)
		}
		s.Apply(ev, now)
	}
	apply(0)
	j := s.Job("bomth3m0o")
	if j == nil || !j.Running() || j.Background || j.Kind() != "shell" || j.ToolUseID != "toolu_01" {
		t.Fatalf("started: %+v", j)
	}
	apply(1)
	apply(2)
	if !j.Background || !j.Running() {
		t.Fatalf("backgrounded: %+v", j)
	}
	apply(3)
	apply(4)
	apply(5)
	if j.Status != "stopped" || j.OutputFile == "" || s.TaskStatus["bomth3m0o"] != "stopped" {
		t.Fatalf("stopped: %+v", j)
	}
	apply(6)
	apply(7)
	run := s.RunningJobs()
	if len(run) != 1 || run[0].ID != "b5hybj1hn" || !run[0].Background {
		t.Fatalf("running: %+v", run)
	}
	// A turn ending doesn't end what runs in the background.
	s.Apply(headless.Result{Subtype: "success"}, now)
	if len(s.RunningJobs()) != 1 {
		t.Fatal("a background job ended with the turn")
	}
	// The host says Claude Code is gone: so is everything it ran.
	s.Apply(host.InfoEvent{Info: host.Info{Proto: 3, State: "idle"}}, now)
	if len(s.RunningJobs()) != 0 || s.Job("b5hybj1hn").Status != "ended" {
		t.Fatalf("after exit: %+v", s.Job("b5hybj1hn"))
	}
}

// A replay that no longer reaches a task's start still has it, from the
// host's list.
func TestJobsFromInfo(t *testing.T) {
	s := New()
	at := time.Now().Add(-time.Hour)
	s.Apply(host.InfoEvent{Info: host.Info{Proto: 3, ClaudePID: 1, Background: []host.Task{{ID: "m1", Type: "monitor_mcp", Label: "watch CI", StartedAt: at}}}}, time.Now())
	j := s.Job("m1")
	if j == nil || !j.Running() || j.Kind() != "monitor" || !j.Start.Equal(at) {
		t.Fatalf("%+v", j)
	}
	// A foreground command the turn was waiting on ends with it.
	ev, _ := headless.Decode([]byte(`{"type":"system","subtype":"task_started","task_id":"f1","tool_use_id":"t","description":"make","task_type":"local_bash"}`))
	s.Apply(ev, time.Now())
	s.Apply(headless.Result{Subtype: "success"}, time.Now())
	if s.Job("f1").Running() || !j.Running() {
		t.Fatal("foreground job outlived its turn, or the background one didn't")
	}
}

// A subagent's run is a task to Claude Code too, but not background work:
// the background view's tasks leave it out, known by its type or, heard of
// only from its end, by the Agent call that started it.
func TestSubagentJobs(t *testing.T) {
	s := New()
	now := time.Now()
	s.Apply(host.Sent{Text: "go"}, now)
	s.Apply(headless.Message{Role: "assistant", Blocks: []headless.Block{
		{Type: "tool_use", ID: "tA", Name: "Agent", Input: json.RawMessage(`{"description":"look","subagent_type":"Explore"}`)},
		{Type: "tool_use", ID: "tB", Name: "Agent", Input: json.RawMessage(`{"description":"older"}`)},
	}}, now)
	for _, l := range []string{
		`{"type":"system","subtype":"task_started","task_id":"a1","tool_use_id":"tA","description":"look","subagent_type":"Explore","is_backgrounded":true,"task_type":"local_agent"}`,
		`{"type":"system","subtype":"task_started","task_id":"b1","tool_use_id":"tS","description":"npm run dev","is_backgrounded":true,"task_type":"local_bash"}`,
		`{"type":"system","subtype":"task_notification","task_id":"a0","tool_use_id":"tB","status":"completed","summary":"older"}`,
	} {
		ev, err := headless.Decode([]byte(l))
		if err != nil {
			t.Fatal(err)
		}
		s.Apply(ev, now)
	}
	if w := s.WorkJobs(); len(w) != 1 || w[0].ID != "b1" {
		t.Fatalf("background work: %+v", w)
	}
	if j := s.SubagentJob("a1", ""); j == nil || !j.Running() {
		t.Fatalf("by agent id: %+v", j)
	}
	if j := s.SubagentJob("", "tB"); j == nil || j.ID != "a0" || j.Running() {
		t.Fatalf("by its call: %+v", j)
	}
	if s.SubagentJob("b1", "tS") != nil {
		t.Fatal("a shell is no subagent")
	}
}

// A message sent to a subagent after it finished runs it again under the
// same id: Claude Code starts its task anew, and it's running again.
func TestWokenSubagentRunsAgain(t *testing.T) {
	s := New()
	now := time.Now()
	s.Apply(headless.TaskStarted{ID: "a1", ToolUseID: "tA", Type: "local_agent", Backgrounded: true}, now)
	s.Apply(headless.TaskDone{ID: "a1", ToolUseID: "tA", Status: "completed"}, now)
	if j := s.Job("a1"); j.Running() || s.TaskStatus["a1"] != "completed" {
		t.Fatalf("finished: %+v", j)
	}
	s.Apply(headless.TaskStarted{ID: "a1", Type: "local_agent", Backgrounded: true}, now.Add(time.Minute))
	if j := s.Job("a1"); !j.Running() || s.TaskStatus["a1"] != "" || !j.End.IsZero() {
		t.Fatalf("woken: %+v, status %q", j, s.TaskStatus["a1"])
	}
	s.Apply(headless.TaskDone{ID: "a1", Status: "completed"}, now.Add(2*time.Minute))
	// The host's list of what runs in the background says so too.
	s.Apply(headless.BackgroundTasks{Tasks: []headless.BackgroundTask{{ID: "a1", Type: "local_agent"}}}, now.Add(3*time.Minute))
	if !s.Job("a1").Running() {
		t.Fatal("listed as running in the background, yet not running")
	}
}
