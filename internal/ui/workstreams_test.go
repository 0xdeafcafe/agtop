package ui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/proc"
	"github.com/0xdeafcafe/agtop/internal/state"
)

func workModel(agents ...*fleet.Agent) *Model {
	m := &Model{store: &state.Store{}, previews: map[string]previewEntry{}, w: 140, h: 40, lastState: map[string]string{}}
	m.snap = &fleet.Snapshot{At: time.Now(), Agents: agents}
	m.setView(placeWork)
	return m
}

func workAgent(key, repo, st string, ago time.Duration) *fleet.Agent {
	a := &fleet.Agent{Key: key, DisplayName: key, PID: 7, Acct: claude.DefaultAccount().Profile(), Repo: repo}
	a.ID, a.State, a.UpdatedAt, a.Cwd = key, st, time.Now().Add(-ago), repo
	return a
}

var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

func workText(m *Model) string {
	return ansiRe.ReplaceAllString(strings.Join(m.workBody(), "\n"), "")
}

// workTranscript writes a session that planned two tasks, ticked one,
// started a subagent that is still at work, committed, and ended a turn.
func workTranscript(t *testing.T) string {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	at := func(ago time.Duration) string { return time.Now().Add(-ago).UTC().Format(time.RFC3339Nano) }
	lines := []string{
		`{"type":"user","timestamp":"` + at(50*time.Minute) + `","message":{"role":"user","content":"tidy the auth screens"}}`,
		`{"type":"assistant","timestamp":"` + at(49*time.Minute) + `","message":{"content":[{"type":"tool_use","id":"c1","name":"TaskCreate","input":{"subject":"Unify the auth screen"}}]}}`,
		`{"type":"user","timestamp":"` + at(49*time.Minute) + `","message":{"content":[{"type":"tool_result","tool_use_id":"c1","content":"Task #1 created successfully: Unify the auth screen"}]},"toolUseResult":{"task":{"id":"1","subject":"Unify the auth screen"}}}`,
		`{"type":"assistant","timestamp":"` + at(49*time.Minute) + `","message":{"content":[{"type":"tool_use","id":"c2","name":"TaskCreate","input":{"subject":"Fix the email logo"}}]}}`,
		`{"type":"user","timestamp":"` + at(49*time.Minute) + `","message":{"content":[{"type":"tool_result","tool_use_id":"c2","content":"Task #2 created successfully: Fix the email logo"}]},"toolUseResult":{"task":{"id":"2","subject":"Fix the email logo"}}}`,
		`{"type":"assistant","timestamp":"` + at(40*time.Minute) + `","message":{"content":[{"type":"tool_use","id":"a1","name":"Agent","input":{"description":"Email logo lane","prompt":"…"}}]}}`,
		`{"type":"assistant","timestamp":"` + at(30*time.Minute) + `","message":{"content":[{"type":"tool_use","id":"u1","name":"TaskUpdate","input":{"taskId":"1","status":"completed"}}]}}`,
		`{"type":"user","timestamp":"` + at(20*time.Minute) + `","message":{"content":[{"type":"tool_result","tool_use_id":"b1","content":"[main 1a2b3c4d] feat(auth): one screen for everyone\n 3 files changed"}]}}`,
		`{"type":"assistant","timestamp":"` + at(10*time.Minute) + `","message":{"stop_reason":"end_turn","content":[{"type":"text","text":"**The auth screen is unified.** The logo lane is still going.\n\nMore detail."}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "s", "subagents")
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(sub, "agent-x1.meta.json"), []byte(`{"agentType":"lane","description":"Email logo lane","toolUseId":"a1","spawnDepth":1}`), 0o644)
	os.WriteFile(filepath.Join(sub, "agent-x1.jsonl"), []byte(
		`{"type":"assistant","isSidechain":true,"timestamp":"`+at(time.Minute)+`","message":{"content":[{"type":"text","text":"Now the mail template."},{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/x/logo.tsx"}}]}}`+"\n"), 0o644)
	return path
}

// The overview says what happened, newest first, and what each subagent
// working now last said.
func TestOverviewTellsWhatHappened(t *testing.T) {
	a := workAgent("auth", "/src/lw", "working", 0)
	a.TranscriptPath = workTranscript(t)
	m := workModel(a)
	m.onWorkTimelines(m.workLoad()().(workTimelinesMsg))
	body := workText(m)
	for _, want := range []string{"Now", "Email logo lane", "Now the mail template.", "✓ 1/2",
		"What happened", "you: tidy the auth screens", "planned 2 tasks", "✓ Unify the auth screen",
		"started Email logo lane", "⎇ 1a2b3c4 feat(auth): one screen for everyone", "The auth screen is unified."} {
		if !strings.Contains(body, want) {
			t.Errorf("no %q in\n%s", want, body)
		}
	}
	if strings.Index(body, "The auth screen is unified.") > strings.Index(body, "you: tidy the auth screens") {
		t.Errorf("not newest first:\n%s", body)
	}
	if strings.Contains(body, "More detail") {
		t.Errorf("a turn shows past its first paragraph:\n%s", body)
	}
}

// ↓ skips headings; f keeps only the picked session's history; enter
// opens it in Agents.
func TestOverviewMovesAndFilters(t *testing.T) {
	a := workAgent("auth", "/src/lw", "working", 0)
	a.TranscriptPath = workTranscript(t)
	b := workAgent("other", "/src/x", "done", 2*time.Hour)
	b.PID = 0
	b.TranscriptPath = workTranscript(t)
	m := workModel(a, b)
	m.onWorkTimelines(m.workLoad()().(workTimelinesMsg))
	for i := 0; i < 40 && m.workSelected().Key != "other"; i++ {
		m.workKey("down")
	}
	if m.workSelected().Key != "other" {
		t.Fatal("↓ never reaches the other session")
	}
	m.workKey("f")
	if body := workText(m); strings.Count(body, "you: tidy") != 1 {
		t.Fatalf("f still shows every session:\n%s", body)
	}
	m.workKey("enter")
	if m.view != placeAgents || m.sel != "other" {
		t.Fatalf("enter: view %d, sel %q", m.view, m.sel)
	}
}

// Heavy work under a live session is named from its command line, once
// per process; a real process table must not trip it up.
func TestOverviewHeavyWorkReadsTheRealTable(t *testing.T) {
	a := workAgent("me", "/src/x", "working", 0)
	a.PID = os.Getppid()
	m := workModel(a)
	m.snap.Table = proc.Snapshot(nil)
	_ = m.workHeavy(a)
	m.work.args = map[string]args{"5@0": {line: "node /opt/pnpm/bin/pnpm install --frozen-lockfile"}, "6@0": {line: "node node_modules/.bin/vitest run"}, "8@0": {line: "node server.js"}}
	for pid, want := range map[int]string{5: "pnpm install", 6: "vitest", 8: ""} {
		if got := m.heavyWhat(pid, time.Unix(0, 0), "node"); got != want {
			t.Errorf("pid %d: %q want %q", pid, got, want)
		}
	}
}
