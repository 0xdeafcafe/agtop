package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	_ "github.com/0xdeafcafe/agtop/internal/adapters/claude"
	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/headless"
	"github.com/0xdeafcafe/agtop/internal/host"
)

// A claude -p a session's shell runs is found by its transcript (begun
// while the command ran, asked the same), drawn under the command's row,
// and listed with the subagents, where it opens like one.
func TestSpawnFollowed(t *testing.T) {
	cfg := t.TempDir()
	acct := claude.Account{Name: "t", ConfigDir: cfg}
	cwd := "/work/app"
	now := time.Now().UTC().Truncate(time.Second)
	dir := filepath.Join(acct.ProjectsDir(), claude.ProjectSlug(cwd))
	os.MkdirAll(dir, 0o755)
	ts := now.Format(time.RFC3339)
	// Another conversation begun at the same time, asked something else.
	os.WriteFile(filepath.Join(dir, "other.jsonl"), []byte(`{"type":"user","timestamp":"`+ts+`","message":{"role":"user","content":"something else"}}`+"\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "kid.jsonl"), []byte(strings.Join([]string{
		`{"type":"user","timestamp":"` + ts + `","cwd":"/work/app","message":{"role":"user","content":"check the parser for off-by-ones"}}`,
		`{"type":"assistant","timestamp":"` + ts + `","message":{"id":"m1","role":"assistant","model":"claude-sonnet-5","content":[{"type":"tool_use","id":"r1","name":"Read","input":{"file_path":"/work/app/parse.go"}}]}}`,
		`{"type":"user","timestamp":"` + ts + `","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"r1","content":"…"}]}}`,
	}, "\n")+"\n"), 0o644)

	s := convo.New()
	s.Info.Cwd = cwd
	s.Apply(host.Sent{Text: "ask claude"}, now.Add(-time.Second))
	s.Apply(headless.Message{Role: "assistant", Blocks: []headless.Block{{Type: "tool_use", ID: "b1", Name: "Bash",
		Input: []byte(`{"command":"claude -p \"check the parser for off-by-ones\""}`)}}}, now.Add(-time.Second))
	c := &hostConn{kind: "claude", key: "k", client: &host.Client{}, sess: s, open: map[string]bool{}}
	m := &Model{snap: &fleet.Snapshot{Agents: []*fleet.Agent{{Key: "k", Acct: acct.Profile()}}}, host: c}

	cmd := m.refreshSpawns()
	if cmd == nil {
		t.Fatal("nothing looked for")
	}
	m.onSpawnFound(cmd().(spawnFoundMsg))
	m.drain(m.refreshSubs()) // what it wrote is read in the background
	r := c.spawns["b1"]
	if r == nil || filepath.Base(r.path) != "kid.jsonl" {
		t.Fatalf("found %+v", r)
	}
	out := ansi.Strip(joinLines(s.Render(convo.Options{Width: 110, Now: now})))
	for _, w := range []string{"Claude Code  check the parser for off-by-ones", "1 step", "parse.go"} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in\n%s", w, out)
		}
	}
	m.drain(m.refreshSubs())
	if len(c.subs) != 1 || c.subs[0].ID != spawnPrefix+"b1" || c.subs[0].Type != "Claude Code" {
		t.Fatalf("subs %+v", c.subs)
	}
	if _, live := c.subState(c.subs[0]); !live {
		t.Error("a spawn whose command runs isn't working")
	}
	m.openSub(c, c.subs[0].ID)
	if c.subTail == nil || len(c.subTail.Sess.Turns) == 0 {
		t.Fatal("opening it shows nothing")
	}
}
