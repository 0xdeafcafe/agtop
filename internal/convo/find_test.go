package convo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTranscript(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

var findLines = []string{
	`{"type":"user","timestamp":"2026-09-23T20:00:00Z","cwd":"/work","message":{"role":"user","content":"fix the Flaky login test"},"origin":{"kind":"human"}}`,
	`{"type":"assistant","timestamp":"2026-09-23T20:00:02Z","message":{"id":"m1","role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"go test ./auth"}}]}}`,
	`{"type":"user","timestamp":"2026-09-23T20:00:09Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"Exit code 1\nFAIL TestLogin","is_error":true}]},"toolUseResult":{"stdout":"FAIL TestLogin","stderr":""}}`,
	`{"type":"assistant","timestamp":"2026-09-23T20:00:12Z","message":{"id":"m2","role":"assistant","content":[{"type":"text","text":"The session cookie expired too early."}]}}`,
}

func TestSearchFile(t *testing.T) {
	p := writeTranscript(t, findLines...)
	hits, total, err := SearchFile(p, "LOGIN flaky", 10)
	if err != nil || total != 1 || hits[0].Who != "you" || hits[0].Turn != 1 {
		t.Fatalf("got %+v %d %v", hits, total, err)
	}
	hits, total, _ = SearchFile(p, "cookie", 10)
	if total != 1 || hits[0].Who != "claude" {
		t.Fatalf("cookie: %+v", hits)
	}
	if _, total, _ = SearchFile(p, "testlogin is:failed", 10); total != 1 {
		t.Fatalf("failed step not found: %d", total)
	}
	if _, total, _ = SearchFile(p, "login nowhere", 10); total != 0 {
		t.Fatalf("a word the file lacks still matched")
	}
	if hits, _, _ = SearchFile(p, "is:failed", 10); hits != nil {
		t.Fatalf("a query without words reads every file: %+v", hits)
	}
}

func TestHoldsAllAcrossChunks(t *testing.T) {
	// The word straddles the 64 KiB read buffer's edge.
	s := strings.Repeat("x", 64<<10-3) + "NeedleY"
	ok, err := holdsAll(strings.NewReader(s), []string{"needley"})
	if !ok || err != nil {
		t.Fatalf("missed a word across chunks: %v %v", ok, err)
	}
	ok, _ = holdsAll(strings.NewReader(s), []string{"needley", "absent"})
	if ok {
		t.Fatal("found a word that isn't there")
	}
	// Words raw JSON can't be searched for are left to the full search.
	ok, _ = holdsAll(strings.NewReader("plain"), []string{`"quoted"`, "über"})
	if !ok {
		t.Fatal("unsearchable words should count as present")
	}
}
