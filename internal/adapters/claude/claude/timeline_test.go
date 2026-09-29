package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A run's end reads as what it reported, once, however many times the
// notice of it comes; a background command's end says what it was.
func TestTimelineRunEndsWithItsReport(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	now := time.Now().UTC()
	at := func(m int) string { return now.Add(time.Duration(m-60) * time.Minute).Format(time.RFC3339Nano) }
	notice := `<task-notification><task-id>x1</task-id><tool-use-id>a1</tool-use-id><status>completed</status><summary>Agent \"Logo lane\" finished</summary><result>I've sent the report.</result></task-notification>`
	os.WriteFile(path, []byte(strings.Join([]string{
		`{"type":"assistant","timestamp":"` + at(1) + `","message":{"content":[{"type":"tool_use","id":"a1","name":"Agent","input":{"description":"Logo lane"}}]}}`,
		`{"type":"user","timestamp":"` + at(5) + `","message":{"content":"` + notice + `"}}`,
		`{"type":"user","timestamp":"` + at(6) + `","message":{"content":"` + notice + `"}}`,
		`{"type":"user","timestamp":"` + at(7) + `","message":{"content":"<task-notification><task-id>b9</task-id><status>completed</status><summary>Background command \"pnpm test\" completed (exit code 0)</summary></task-notification>"}}`,
		`{"type":"assistant","timestamp":"` + at(8) + `","message":{"stop_reason":"end_turn","content":[{"type":"text","text":"No response requested."}]}}`,
	}, "\n")+"\n"), 0o644)
	sub := filepath.Join(dir, "s", "subagents")
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(sub, "agent-x1.meta.json"), []byte(`{"agentType":"lane","toolUseId":"a1"}`), 0o644)
	os.WriteFile(filepath.Join(sub, "agent-x1.jsonl"), []byte(strings.Join([]string{
		`{"type":"assistant","timestamp":"` + at(3) + `","message":{"content":[{"type":"tool_use","id":"h","name":"SubagentHandback","input":{"message":"## The logo is fixed\n\nDetails."}}]}}`,
		`{"type":"assistant","timestamp":"` + at(4) + `","message":{"stop_reason":"end_turn","content":[{"type":"text","text":"I've sent the report."}]}}`,
	}, "\n")+"\n"), 0o644)

	var tl Timeline
	tl.Update(path, now.Add(-time.Hour-time.Minute))
	v := tl.View(now.Add(-2 * time.Hour))
	var ends []string
	for _, e := range v.Events {
		if e.Kind == EvEnd {
			ends = append(ends, e.Run+"|"+e.Text)
		}
		if e.Kind == EvTurn {
			t.Errorf("a synthetic turn shows: %q", e.Text)
		}
	}
	want := []string{"x1|The logo is fixed", `|Background command "pnpm test" completed (exit code 0)`}
	if strings.Join(ends, "\n") != strings.Join(want, "\n") {
		t.Errorf("ends:\n%s\nwant:\n%s", strings.Join(ends, "\n"), strings.Join(want, "\n"))
	}
	if len(v.Runs) != 1 || v.Runs[0].Name != "Logo lane" || !v.Runs[0].Ended || v.Runs[0].Said != "The logo is fixed" {
		t.Errorf("runs: %+v", v.Runs)
	}
}
