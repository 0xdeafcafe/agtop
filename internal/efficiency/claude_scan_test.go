package efficiency_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/0xdeafcafe/rush/internal/adapters/claude"
	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/usage"
	. "github.com/0xdeafcafe/rush/internal/efficiency"
)

// These read Claude Code's own transcripts through its adapter:
// efficiency's figures as they come from a real agent's files.

func source(t *testing.T) Source {
	t.Helper()
	src, ok := agent.As[Source](Agent)
	if !ok {
		t.Fatal("no Source for", Agent)
	}
	return src
}

func price(t *testing.T, model string, u usage.TokenUsage) float64 {
	t.Helper()
	pr, _ := agent.As[agent.Pricer](Agent)
	c, _ := pr.Cost(model, u)
	return c
}

// Lines as Claude Code writes them, cut to what the scan reads.
var transcript = []string{
	`{"type":"attachment","timestamp":"2026-09-20T10:00:00Z","sessionId":"s1","cwd":"/work/repo","attachment":{"type":"hook_success","hookEvent":"SessionStart","command":"node \"${CLAUDE_PLUGIN_ROOT}/src/hooks/caveman-activate.js\""}}`,
	`{"type":"user","timestamp":"2026-09-20T10:00:01Z","message":{"role":"user","content":"<command-name>/caveman</command-name>\n<command-args>ultra</command-args>"}}`,
	// One message written as two lines: its usage is counted once, the last.
	`{"type":"assistant","timestamp":"2026-09-20T10:00:02Z","message":{"id":"m1","model":"claude-opus-5-5","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":10,"cache_read_input_tokens":0,"cache_creation_input_tokens":50000,"output_tokens":5}}}`,
	`{"type":"assistant","timestamp":"2026-09-20T10:00:03Z","message":{"id":"m1","model":"claude-opus-5-5","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"FOO=1 rtk git status"}},{"type":"tool_use","id":"t2","name":"Read","input":{"file_path":"/work/repo/a.go"}}],"usage":{"input_tokens":10,"cache_read_input_tokens":0,"cache_creation_input_tokens":50000,"output_tokens":40,"output_tokens_details":{"thinking_tokens":30}}}}`,
	`{"type":"user","timestamp":"2026-09-20T10:00:04Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"` + strings.Repeat("x", 1000) + `"},{"type":"tool_result","tool_use_id":"t2","content":[{"type":"text","text":"` + strings.Repeat("y", 500) + `"}]}]}}`,
	`{"type":"attachment","timestamp":"2026-09-20T10:00:05Z","attachment":{"type":"hook_success","hookEvent":"PreToolUse","command":"rtk hook claude"}}`,
	`{"type":"assistant","timestamp":"2026-09-20T11:30:00Z","message":{"id":"m2","model":"claude-opus-5-5","content":[{"type":"tool_use","id":"t3","name":"Bash","input":{"command":"rtk gain -d"}},{"type":"tool_use","id":"t4","name":"Skill","input":{"skill":"caveman:caveman"}},{"type":"tool_use","id":"t5","name":"mcp__serena__find_symbol","input":{}}],"usage":{"input_tokens":1,"cache_read_input_tokens":50000,"cache_creation_input_tokens":100,"output_tokens":20}}}`,
	// A tool result mentioning compact_boundary is still a tool result.
	`{"type":"user","timestamp":"2026-09-20T11:30:01Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t3","content":"\"compact_boundary\""}]}}`,
	`{"type":"system","subtype":"compact_boundary","timestamp":"2026-09-20T11:31:00Z","compactMetadata":{"trigger":"auto","preTokens":500000,"postTokens":20000}}`,
	`{"type":"attachment","timestamp":"2026-09-20T11:32:00Z","attachment":{"type":"invoked_skills","skills":[{"name":"graphify","path":"userSettings:graphify"}]}}`,
}

func writeTranscript(t *testing.T, lines []string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "s1.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestScan(t *testing.T) {
	p := writeTranscript(t, transcript)
	f := &File{}
	if _, err := Scan(source(t), p, f, nil); err != nil {
		t.Fatal(err)
	}
	if f.Session != "s1" || f.Project != "/work/repo" {
		t.Fatalf("session %q project %q", f.Session, f.Project)
	}
	tot := f.Totals()
	if tot.Req != 2 {
		t.Fatalf("two messages are two requests, got %d", tot.Req)
	}
	if tot.Out != 60 || tot.Think != 30 || tot.CW != 50100 || tot.CR != 50000 {
		t.Fatalf("tokens counted wrong: %+v", tot)
	}
	if f.Start() != 50010 || f.Peak() != 50101 {
		t.Fatalf("start %d peak %d", f.Start(), f.Peak())
	}
	if tot.Calls[ToolBash] != 2 || tot.Bytes[ToolBash] != int64(1000+len(`\"compact_boundary\"`)) || tot.Calls[ToolRead] != 1 || tot.Bytes[ToolRead] != 500 {
		t.Fatalf("tool results: calls %v bytes %v", tot.Calls, tot.Bytes)
	}
	if len(f.Compacts) != 1 || !f.Compacts[0].Auto || f.Compacts[0].Pre != 500000 {
		t.Fatalf("compactions: %+v", f.Compacts)
	}
	if f.Reads["/work/repo/a.go"] != 1 {
		t.Fatalf("reads: %v", f.Reads)
	}
	for _, k := range []string{"bash:rtk", "bash:rtk/admin", "hook:rtk hook claude", "skill:caveman:caveman", "cmd:/caveman", "mcp:serena", "skill:graphify"} {
		if f.Uses[k] == nil {
			t.Errorf("didn't see %q; saw %v", k, keys(f.Uses))
		}
	}
	if len(f.Hours) != 2 {
		t.Fatalf("two hours of use, got %d", len(f.Hours))
	}
	cost := tot.Cost()
	want := price(t, "claude-opus-5-5", usage.TokenUsage{Input: 11, Output: 60, CacheRead: 50000, CacheWrite5m: 50100})
	if d := cost - want; d > 1e-9 || d < -1e-9 {
		t.Fatalf("cost %f, want %f", cost, want)
	}
}

func keys(m map[string]*Use) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// Reading a transcript in pieces, as it's written, comes to the same as
// reading it whole; a half-written line waits.
func TestScanIncremental(t *testing.T) {
	whole := &File{}
	p := writeTranscript(t, transcript)
	if _, err := Scan(source(t), p, whole, nil); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	q := filepath.Join(dir, "s1.jsonl")
	f := &File{}
	var written string
	for _, l := range transcript {
		half := len(l) / 2
		written += l[:half]
		_ = os.WriteFile(q, []byte(written), 0o600)
		if _, err := Scan(source(t), q, f, nil); err != nil {
			t.Fatal(err)
		}
		written += l[half:] + "\n"
		_ = os.WriteFile(q, []byte(written), 0o600)
		if _, err := Scan(source(t), q, f, nil); err != nil {
			t.Fatal(err)
		}
		f = f.Clone() // as the store does between runs
	}
	a, b := whole.Totals(), f.Totals()
	if a != b {
		t.Fatalf("pieces %+v\nwhole %+v", b, a)
	}
}

func TestViewAndFindings(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	p := writeTranscript(t, transcript)
	f := &File{Account: "/acct"}
	if _, err := Scan(source(t), p, f, nil); err != nil {
		t.Fatal(err)
	}
	s := StoreOf(map[string]*File{p: f})
	at := time.Date(2026, 9, 20, 23, 0, 0, 0, time.UTC)
	v := s.View(NewQuery(Ranges[0], at))
	if v.Total.Req != 2 || v.Sessions != 1 || v.Compacts != 1 || v.Autos != 1 {
		t.Fatalf("view: req %d sessions %d compacts %d", v.Total.Req, v.Sessions, v.Compacts)
	}
	if v.Uses["rtk"] == nil || v.Uses["caveman"] == nil || v.Uses["serena"] == nil || v.Uses["graphify"] == nil {
		t.Fatalf("saver uses: %v", v.Uses)
	}
	if v.FirstUse["caveman"].IsZero() {
		t.Fatal("caveman's first use isn't known")
	}
	if n := len(v.Series(MetricCtx).Values); n != len(v.Points) {
		t.Fatalf("series has %d points for %d", n, len(v.Points))
	}
	// Scoped to another account or folder, nothing.
	q := NewQuery(Ranges[0], at)
	q.Accounts = []string{"/other"}
	if s.View(q).Total.Req != 0 {
		t.Fatal("another account's view counted this one")
	}
	q = NewQuery(Ranges[0], at)
	q.Project = "/work/rep"
	if s.View(q).Total.Req != 0 {
		t.Fatal("a folder's prefix isn't the folder")
	}
	q.Project = "/work"
	if s.View(q).Total.Req != 2 {
		t.Fatal("a parent folder should include its sessions")
	}

	found := map[string]Found{"rtk": {Status: Partial, Wants: "its hook isn't in settings.json"}}
	fs := Findings(v, found)
	if len(fs) == 0 || fs[0].Fix != "rtk" {
		t.Fatalf("a half set up saver should come first: %+v", fs)
	}
}

func TestRetire(t *testing.T) {
	p := writeTranscript(t, transcript)
	f := &File{Account: "/acct"}
	if _, err := Scan(source(t), p, f, nil); err != nil {
		t.Fatal(err)
	}
	s := StoreOf(map[string]*File{})
	s.Retire(f)
	at := time.Date(2026, 9, 20, 23, 0, 0, 0, time.UTC)
	v := s.View(NewQuery(Ranges[0], at))
	if v.Total.Req != 2 || v.FirstUse["rtk"].IsZero() {
		t.Fatalf("a deleted transcript's totals should stay: req %d first %v", v.Total.Req, v.FirstUse)
	}
}
