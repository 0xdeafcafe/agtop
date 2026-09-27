package ui

import (
	"os"
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
	a := &fleet.Agent{Key: key, DisplayName: key, PID: 7, Acct: claude.DefaultAccount(), Repo: repo}
	a.ID, a.State, a.UpdatedAt, a.Cwd = key, st, time.Now().Add(-ago), repo
	return a
}

var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

// What's waiting on you comes first, the most stuck first; every session
// then shows under its repo, with the count it last reported moving.
func TestWorkstreamsSaysWhatWaitsAndWhere(t *testing.T) {
	broke := workAgent("broke", "/src/lw", "done", 5*time.Minute)
	broke.Spend.Halt = &claude.Halt{Kind: "rate_limit", Text: "You've hit your session limit · resets 5am (Europe/London)"}
	broke.Spend.Progress, broke.Spend.ProgressAt = "Lint went 11,065 → 9,052", time.Now()
	asks := workAgent("asks", "/src/lw", "blocked", 2*time.Minute)
	asks.Needs = "which way?"
	busy := workAgent("busy", "/src/agtop", "working", time.Minute)
	gone := workAgent("gone", "/src/agtop", "done", time.Hour)
	gone.Done = true
	m := workModel(broke, asks, busy, gone)
	body := ansiRe.ReplaceAllString(strings.Join(m.workBody(), "\n"), "")
	wait := strings.Index(body, "Waiting on you")
	for _, want := range []string{"✗ broke", "? asks", "which way?", "session limit · resets 5am", "Lint went 11,065 → 9,052", "lw", "agtop"} {
		if !strings.Contains(body, want) {
			t.Errorf("no %q in\n%s", want, body)
		}
	}
	if !(wait < strings.Index(body, "✗ broke") && strings.Index(body, "✗ broke") < strings.Index(body, "? asks")) {
		t.Errorf("waiting rows out of order:\n%s", body)
	}
	if strings.Contains(body, "gone") {
		t.Errorf("a session put away still shows:\n%s", body)
	}
}

// An agent both waiting and in its repo is two rows; ↓ goes through both,
// and enter opens it in Agents.
func TestWorkstreamsMovesByRow(t *testing.T) {
	asks := workAgent("asks", "/src/lw", "blocked", 2*time.Minute)
	asks.Needs = "which way?"
	busy := workAgent("busy", "/src/lw", "working", time.Minute)
	m := workModel(asks, busy)
	var seen []string
	for i := 0; i < 3; i++ {
		seen = append(seen, m.workSelected().Key)
		m.workKey("down")
	}
	if strings.Join(seen, ",") != "asks,busy,asks" && strings.Join(seen, ",") != "asks,asks,busy" {
		t.Fatalf("↓ went %v", seen)
	}
	m.work.pos, m.work.sel = 0, "asks"
	m.workKey("enter")
	if m.view != placeAgents || m.sel != "asks" {
		t.Fatalf("enter: view %d, sel %q", m.view, m.sel)
	}
}

// Heavy work under a live session is named from its command line, once
// per process; a real process table must not trip it up.
func TestWorkstreamsHeavyWorkReadsTheRealTable(t *testing.T) {
	a := workAgent("me", "/src/x", "working", 0)
	a.PID = os.Getppid()
	m := workModel(a)
	m.snap.Table = proc.Snapshot(nil)
	_ = m.workHeavy(workRepo{agents: []*fleet.Agent{a}})
	m.work.args = map[string]args{"5@0": {line: "node /opt/pnpm/bin/pnpm install --frozen-lockfile"}, "6@0": {line: "node node_modules/.bin/vitest run"}, "8@0": {line: "node server.js"}}
	for pid, want := range map[int]string{5: "pnpm install", 6: "vitest", 8: ""} {
		if got := m.heavyWhat(pid, time.Unix(0, 0), "node"); got != want {
			t.Errorf("pid %d: %q want %q", pid, got, want)
		}
	}
}
