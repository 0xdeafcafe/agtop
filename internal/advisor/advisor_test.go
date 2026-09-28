package advisor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/efficiency"
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
	return log
}

func TestPass(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	log := fakeClaude(t,
		`{"type":"result","is_error":false,"total_cost_usd":0.01,"structured_output":{"findings":[`+
			`{"title":"Big one","detail":"d","evidence":["/p/s.jsonl"],"weeklyCost":5,"fix":"rtk","open":""},`+
			`{"title":"Small one","detail":"d","evidence":[],"weeklyCost":0.2,"fix":"nope","open":""},`+
			`{"title":"Second big","detail":"d","evidence":[],"weeklyCost":3,"fix":"","open":""}]}}`,
		`{"type":"result","is_error":false,"total_cost_usd":0.5,"structured_output":{"confirmed":true,"note":"seen in s.jsonl","title":"Checked one","detail":"exact","evidence":[],"weeklyCost":4,"fix":"rtk","open":""}}`)
	now := time.Now()
	in := Input{View: &efficiency.View{Q: efficiency.Query{From: now.Add(-time.Hour), To: now}, Total: efficiency.Bucket{Req: 1}}}
	in.Pending = []Finding{{ID: "old", Title: "Left from before", Weekly: 2, Status: Candidate}}
	res := Pass(context.Background(), claude.Account{ConfigDir: t.TempDir()}, in, 1)
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
	for _, want := range []string{"--no-session-persistence", "--tools Read,Grep,Glob", "--max-budget-usd 0.15", "--max-budget-usd 1.50", "--model opus"} {
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
	t.Setenv("AGTOP_HOME", t.TempDir())
	fakeClaude(t, `{"type":"result","is_error":true,"subtype":"error_max_budget_usd","total_cost_usd":0.15,"result":"over budget"}`, `{}`)
	now := time.Now()
	in := Input{View: &efficiency.View{Q: efficiency.Query{From: now, To: now}}}
	res := Pass(context.Background(), claude.Account{ConfigDir: t.TempDir()}, in, 3)
	if res.Err == nil || !strings.Contains(res.Err.Error(), "budget") || res.Spent != 0.15 {
		t.Fatalf("err %v spent %v: want the failure said, and its cost counted", res.Err, res.Spent)
	}
}
