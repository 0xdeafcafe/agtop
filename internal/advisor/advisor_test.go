package advisor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	_ "github.com/0xdeafcafe/rush/internal/adapters/claude"
	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/efficiency"
)

func TestDue(t *testing.T) {
	now := time.Now()
	r := &Record{}
	if r.Due(now, 0) {
		t.Fatal("a first pass with nothing to look at")
	}
	if !r.Due(now, 1) {
		t.Fatal("no first pass once there's work")
	}
	r.LastRun = now.Add(-time.Hour)
	if r.Due(now, 10_000) {
		t.Fatal("a pass inside the gap")
	}
	r.LastRun = now.Add(-Gap)
	if r.Due(now, MinRequests-1) {
		t.Fatal("a pass with too little new work")
	}
	if !r.Due(now, MinRequests) {
		t.Fatal("no pass once the gap and the work are both there")
	}
}

func TestReviewsLeft(t *testing.T) {
	now := time.Now()
	r := &Record{Reviews: []time.Time{now.Add(-25 * time.Hour), now.Add(-time.Hour), now}}
	if got := r.ReviewsLeft(now); got != ReviewsPerDay-2 {
		t.Fatalf("%d reviews left, want %d", got, ReviewsPerDay-2)
	}
}

func TestMerge(t *testing.T) {
	r := &Record{}
	at := time.Now()
	a := Finding{ID: idOf("Trim CLAUDE.md"), Title: "Trim CLAUDE.md", Status: Candidate, At: at}
	b := Finding{ID: idOf("Use sonnet for subagents"), Title: "Use sonnet for subagents", Status: Confirmed, Weekly: 4, At: at}
	c := Finding{ID: idOf("Read less"), Title: "Read less", Status: Rejected, At: at}
	fresh := r.Merge(Result{At: at, Findings: []Finding{a, b, c}, Spent: 0.2})
	if len(fresh) != 1 || fresh[0].ID != b.ID {
		t.Fatalf("newly confirmed %v, want just %q", fresh, b.Title)
	}
	shown := r.Shown()
	if len(shown) != 2 || shown[0].ID != b.ID {
		t.Fatalf("shown %v: want the confirmed one first and no rejected one", shown)
	}
	// Proposed again, reworded only in case: the same finding, not news.
	again := b
	again.Title, again.Status = "use Sonnet for subagents", Candidate
	again.ID = idOf(again.Title)
	if fresh := r.Merge(Result{At: at, Findings: []Finding{again}}); len(fresh) != 0 {
		t.Fatal("a confirmed finding came back as news")
	}
	if got := r.Shown(); got[0].Status != Confirmed {
		t.Fatal("a candidate replaced what Opus had confirmed")
	}
	r.Dismiss(b.ID)
	for _, f := range r.Shown() {
		if f.ID == b.ID {
			t.Fatal("a dismissed finding is still shown")
		}
	}
	if len(r.Known()) != 3 {
		t.Fatalf("known %v: rejected and dismissed ones must stay known", r.Known())
	}
	if r.Spent != 0.2 || r.Runs != 2 {
		t.Fatalf("spent %v over %d runs", r.Spent, r.Runs)
	}
}

func TestDigest(t *testing.T) {
	now := time.Now()
	v := &efficiency.View{Q: efficiency.Query{From: now.Add(-7 * 24 * time.Hour), To: now}}
	if d := Digest(Input{View: v}); !strings.Contains(d, "No requests") {
		t.Fatalf("empty digest: %q", d)
	}
	v.Total = efficiency.Bucket{Req: 100, In: 1000, CR: 9000, Out: 500, CIn: 1, CCR: 2}
	v.Total.Calls[efficiency.ToolBash], v.Total.Bytes[efficiency.ToolBash] = 40, 2<<20
	d := Digest(Input{
		View:  v,
		Top:   []efficiency.SessionCost{{Path: "/p/s.jsonl", Project: "/src/x", B: efficiency.Bucket{Req: 50, CIn: 2}, WorstRead: "a.go", WorstReads: 9}},
		Known: []string{"Trim CLAUDE.md"},
	})
	for _, want := range []string{"100 requests", "Cache hits 90%", "Bash 40 calls", "/p/s.jsonl", "a.go ×9", "Trim CLAUDE.md", "Savers"} {
		if !strings.Contains(d, want) {
			t.Errorf("digest lacks %q:\n%s", want, d)
		}
	}
	if len(d) > 8<<10 {
		t.Errorf("digest is %d bytes; it's meant to be cheap", len(d))
	}
}

// fakeClaude puts a claude on PATH that answers as Haiku or Opus would.
func fakeClaude(t *testing.T, haiku, opus string) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	script := `#!/bin/sh
echo "$@" >> ` + log + `
cat > /dev/null
case "$*" in
*"--model haiku"*) printf '%s' '` + haiku + `' ;;
*) printf '%s' '` + opus + `' ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	agent.Recheck() // the advisor runs the claude found installed
	t.Cleanup(agent.Recheck)
	return log
}

func TestPass(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	log := fakeClaude(t,
		`{"type":"result","is_error":false,"total_cost_usd":0.01,"structured_output":{"findings":[`+
			`{"title":"Big one","detail":"d","evidence":["/p/s.jsonl"],"weeklyCost":5,"fix":"rtk","open":""},`+
			`{"title":"Small one","detail":"d","evidence":[],"weeklyCost":0.2,"fix":"nope","open":""},`+
			`{"title":"Second big","detail":"d","evidence":[],"weeklyCost":3,"fix":"","open":""}]}}`,
		`{"type":"result","is_error":false,"total_cost_usd":0.5,"structured_output":{"confirmed":true,"note":"seen in s.jsonl","title":"Checked one","detail":"exact","evidence":[],"weeklyCost":4,"fix":"rtk","open":""}}`)
	now := time.Now()
	in := Input{View: &efficiency.View{Q: efficiency.Query{From: now.Add(-time.Hour), To: now}, Total: efficiency.Bucket{Req: 1}}}
	in.Pending = []Finding{{ID: "old", Title: "Left from before", Weekly: 2, Status: Candidate}}
	res := Pass(context.Background(), agent.Profile{Kind: efficiency.Agent, Dir: t.TempDir()}, in, 1)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if len(res.Findings) != 4 || len(res.Reviewed) != 1 {
		t.Fatalf("%d findings, %d reviews: want 4 (one left from before) and 1 (the cap)", len(res.Findings), len(res.Reviewed))
	}
	if res.Findings[2].ID != "old" || res.Findings[2].Status != Candidate {
		t.Fatalf("a pending candidate should wait its turn by what's at stake: %+v", res.Findings)
	}
	f := res.Findings[0]
	if f.Status != Confirmed || f.Title != "Checked one" || f.ID != idOf("Big one") || f.Note == "" {
		t.Fatalf("the biggest wasn't reviewed and kept under its first ID: %+v", f)
	}
	if res.Findings[1].Status != Candidate || res.Findings[3].Fix != "" {
		t.Fatalf("over the cap and under the bar stay candidates, unknown fixes dropped: %+v", res.Findings[1:])
	}
	if res.Spent != 0.51 {
		t.Fatalf("spent %v, want 0.51", res.Spent)
	}
	b, _ := os.ReadFile(log)
	calls := string(b)
	for _, want := range []string{"--no-session-persistence", "--tools Read,Grep,Glob", "--restricted", "--permission-prompts none", "Read(", "--max-budget-usd 0.30", "--max-budget-usd 1.50", "--model opus"} {
		if !strings.Contains(calls, want) {
			t.Errorf("no %q in the calls:\n%s", want, calls)
		}
	}
	for _, never := range []string{"Bash", "Edit", "Write"} {
		if strings.Contains(calls, never) {
			t.Errorf("the advisor was given %s", never)
		}
	}
}

func TestPassFails(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	fakeClaude(t, `{"type":"result","is_error":true,"subtype":"error_max_budget_usd","total_cost_usd":0.15,"result":"over budget"}`, `{}`)
	now := time.Now()
	in := Input{View: &efficiency.View{Q: efficiency.Query{From: now, To: now}}}
	res := Pass(context.Background(), agent.Profile{Kind: efficiency.Agent, Dir: t.TempDir()}, in, 3)
	if res.Err == nil || !strings.Contains(res.Err.Error(), "budget") || res.Spent != 0.15 {
		t.Fatalf("err %v spent %v: want the failure said, and its cost counted", res.Err, res.Spent)
	}
}

func TestSettledAndEviction(t *testing.T) {
	r := &Record{}
	old := time.Now().Add(-time.Hour)
	var fs []Finding
	for i := range Keep {
		fs = append(fs, Finding{ID: fmt.Sprint("c", i), Title: fmt.Sprint("confirmed ", i), Status: Confirmed, At: old})
	}
	fs = append(fs, Finding{ID: "r", Title: "rejected", Status: Rejected, At: time.Now()})
	r.Merge(Result{At: time.Now(), Findings: fs})
	if len(r.Findings) != Keep || slices.ContainsFunc(r.Findings, func(f Finding) bool { return f.ID == "r" }) {
		t.Fatal("over Keep, a rejected one should go before any confirmed one, however new")
	}
	r.Findings = append(r.Findings, Finding{ID: "x", Status: Rejected})
	r.Dismiss("d")
	if s := r.Settled(); !slices.Contains(s, "x") || !slices.Contains(s, "d") || !slices.Contains(s, "c0") {
		t.Fatalf("settled %v: want rejected, confirmed and put away", s)
	}
	// Re-proposed, a rejected one isn't reopened.
	r.Merge(Result{At: time.Now(), Findings: []Finding{{ID: "x", Status: Candidate}}})
	if i := slices.IndexFunc(r.Findings, func(f Finding) bool { return f.ID == "x" }); i >= 0 && r.Findings[i].Status != Rejected {
		t.Fatal("a candidate reopened what Opus rejected")
	}
}

func TestPassSkipsSettled(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	log := fakeClaude(t,
		`{"type":"result","total_cost_usd":0.01,"structured_output":{"findings":[{"title":"Old news","detail":"","evidence":[],"weeklyCost":9,"fix":"","open":""}]}}`,
		`{"type":"result","total_cost_usd":0.5,"structured_output":{"confirmed":true,"note":"","title":"x","detail":"","evidence":[],"weeklyCost":9,"fix":"","open":""}}`)
	now := time.Now()
	in := Input{View: &efficiency.View{Q: efficiency.Query{From: now, To: now}}, Settled: []string{idOf("old  NEWS")}}
	res := Pass(context.Background(), agent.Profile{Kind: efficiency.Agent, Dir: t.TempDir()}, in, 3)
	if len(res.Findings) != 0 || len(res.Reviewed) != 0 {
		t.Fatalf("a settled finding was proposed or reviewed again: %+v", res)
	}
	if b, _ := os.ReadFile(log); strings.Contains(string(b), "--model opus") {
		t.Fatal("Opus was paid to look at it again")
	}
}

func TestLock(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	unlock, ok := Lock()
	if !ok {
		t.Fatal("no lock")
	}
	if _, ok := Lock(); ok {
		t.Fatal("two rushes both hold the pass")
	}
	unlock()
	unlock2, ok := Lock()
	if !ok {
		t.Fatal("the lock wasn't let go")
	}
	unlock2()
	// A lock left by a process that died is taken over once stale.
	p := filepath.Join(Dir(), "pass.lock")
	_ = os.WriteFile(p, nil, 0o600)
	_ = os.Chtimes(p, time.Now(), time.Now().Add(-lockStale-time.Minute))
	if _, ok := Lock(); !ok {
		t.Fatal("a stale lock blocks for good")
	}
}

func TestActive(t *testing.T) {
	acct := agent.Profile{Kind: efficiency.Agent, Dir: t.TempDir()}
	since := time.Now().Add(-time.Minute)
	if Active(acct, since) {
		t.Fatal("active with no transcripts")
	}
	dir := filepath.Join(efficiency.TranscriptsDir(acct), "proj")
	_ = os.MkdirAll(dir, 0o700)
	p := filepath.Join(dir, "s.jsonl")
	_ = os.WriteFile(p, []byte("{}"), 0o600)
	_ = os.Chtimes(p, time.Now(), since.Add(-time.Hour))
	if Active(acct, since) {
		t.Fatal("an old transcript counts as new work")
	}
	_ = os.Chtimes(p, time.Now(), time.Now())
	if !Active(acct, since) {
		t.Fatal("a transcript written since isn't new work")
	}
}

func TestFailedReviews(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	fakeClaude(t,
		`{"type":"result","total_cost_usd":0.01,"structured_output":{"findings":[{"title":"Flaky","detail":"","evidence":[],"weeklyCost":9,"fix":"","open":""}]}}`,
		`not json`)
	now := time.Now()
	reserved := 0
	in := Input{View: &efficiency.View{Q: efficiency.Query{From: now, To: now}},
		Pending: []Finding{{ID: idOf("Flaky"), Title: "Flaky", Weekly: 9, Status: Candidate, Tries: Tries - 1}},
		Reserve: func() error { reserved++; return nil }}
	res := Pass(context.Background(), agent.Profile{Kind: efficiency.Agent, Dir: t.TempDir()}, in, 3)
	if reserved != 1 || len(res.Reviewed) != 1 {
		t.Fatalf("%d reserved, %d reviewed: want each review reserved before it runs", reserved, len(res.Reviewed))
	}
	if res.Findings[0].Tries != Tries {
		t.Fatalf("tries %d: a failed review must count, carried over from before", res.Findings[0].Tries)
	}
	if res.Spent < reviewBudget {
		t.Fatalf("spent %v: a review that broke counts what it may have spent", res.Spent)
	}
	r := &Record{}
	r.Merge(res)
	if len(r.Pending()) != 0 {
		t.Fatal("a candidate that keeps failing is reviewed again")
	}
}

func TestReserveAndBegin(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	now := time.Now()
	if err := Begin(now); err != nil {
		t.Fatal(err)
	}
	_ = Reserve(now)
	_ = Reserve(now)
	r := Load()
	if !r.LastRun.Equal(now) || r.ReviewsLeft(now) != ReviewsPerDay-2 {
		t.Fatalf("a pass and its reviews must be on disk before they spend: %+v", r)
	}
	r.Merge(Result{At: now, Reviewed: []time.Time{now, now}})
	if r.ReviewsLeft(now) != ReviewsPerDay-2 {
		t.Fatal("merging counted the reviews twice")
	}
}

func TestCorruptRecord(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	_ = os.MkdirAll(Dir(), 0o700)
	_ = os.WriteFile(recordPath(), []byte("{broken"), 0o600)
	r := Load()
	if r.Due(time.Now(), 1_000_000) {
		t.Fatal("a broken record lets a pass run at once")
	}
	if _, err := os.Stat(recordPath() + ".bad"); err != nil {
		t.Fatal("the broken record wasn't kept aside")
	}
}

func TestCheckOpen(t *testing.T) {
	root := t.TempDir()
	in := filepath.Join(root, "CLAUDE.md")
	_ = os.WriteFile(in, nil, 0o600)
	out := filepath.Join(t.TempDir(), "secret")
	_ = os.WriteFile(out, nil, 0o600)
	for p, want := range map[string]string{in: in, out: "", "CLAUDE.md": "", filepath.Join(root, "..", filepath.Base(root), "CLAUDE.md"): in, filepath.Join(root, "missing.md"): ""} {
		if got := checkOpen(p, []string{root}); got != want {
			t.Errorf("checkOpen(%q) = %q, want %q", p, got, want)
		}
	}
}

func TestEnabled(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	if Enabled() {
		t.Fatal("on with no config")
	}
	_ = os.WriteFile(filepath.Join(os.Getenv("RUSH_HOME"), "config.json"), []byte(`{"advisor":true}`), 0o600)
	if !Enabled() {
		t.Fatal("off with it on in the config")
	}
	t.Setenv("RUSH_ADVISOR", "off")
	if Enabled() {
		t.Fatal("on with the environment turning it off")
	}
}
