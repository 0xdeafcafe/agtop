package advisor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrief(t *testing.T) {
	dir := t.TempDir()
	big := strings.Repeat("x", 300_000)
	lines := []string{
		`{"type":"user","timestamp":"2026-09-28T10:00:00Z","message":{"content":"fix the build"}}`,
		`{"type":"assistant","timestamp":"2026-09-28T10:01:00Z","message":{"usage":{"input_tokens":10,"cache_read_input_tokens":40000},"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"go test ./..."}}]}}`,
		`{"type":"user","timestamp":"2026-09-28T10:02:00Z","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"` + big + `"}]}}`,
		`{"type":"assistant","timestamp":"2026-09-28T10:03:00Z","message":{"content":[{"type":"tool_use","id":"t2","name":"Agent","input":{"subagent_type":"Explore","model":"opus","description":"find it"}}]}}`,
		`{"type":"system","subtype":"compact_boundary","timestamp":"2026-09-28T10:04:00Z","compactMetadata":{"trigger":"auto","preTokens":400000}}`,
		`{"type":"progress","data":"` + big + `"}`,
	}
	p := filepath.Join(dir, "s.jsonl")
	_ = os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
	out := Briefs(filepath.Join(dir, "briefs"), []string{p, filepath.Join(dir, "missing.jsonl")})
	if out[1] != "" {
		t.Fatal("a brief of a missing transcript")
	}
	b, err := os.ReadFile(out[0])
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{"you (ctx 0): fix the build", "Bash: go test ./... → 292 KB", "Agent: opus, Explore, find it", "compacted (auto) at 400k"} {
		if !strings.Contains(got, want) {
			t.Errorf("brief lacks %q:\n%s", want, got)
		}
	}
	if len(b) > 2000 {
		t.Errorf("brief is %d bytes for a few lines", len(b))
	}
}

func TestBriefCapped(t *testing.T) {
	dir := t.TempDir()
	var lines []string
	for i := range 2000 {
		lines = append(lines, fmt.Sprintf(`{"type":"assistant","timestamp":"2026-09-28T10:00:00Z","message":{"content":[{"type":"tool_use","id":"t%d","name":"Read","input":{"file_path":"/f%d.go"}}]}}`, i, i))
	}
	p := filepath.Join(dir, "s.jsonl")
	_ = os.WriteFile(p, []byte(strings.Join(lines, "\n")), 0o600) // no final newline
	b, _ := os.ReadFile(Briefs(filepath.Join(dir, "b"), []string{p})[0])
	got := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(got) != briefHead+briefTail+1 || !strings.Contains(string(b), "1500 lines skipped") || !strings.Contains(got[len(got)-1], "/f1999.go") {
		t.Fatalf("%d lines; want head, a skip marker, and the tail through the last line", len(got))
	}
}
