package fleet

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent"
)

func TestCompactions(t *testing.T) {
	t.Setenv("CLAUDE_CODE_AUTO_COMPACT_WINDOW", "")
	cfg, cwd := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(cfg, "settings.json"), []byte(`{"env":{"CLAUDE_CODE_AUTO_COMPACT_WINDOW":"400000"}}`), 0o600)
	os.MkdirAll(filepath.Join(cwd, ".claude"), 0o700)
	a := &Agent{Key: "k", Kind: "claude", Past: true, Acct: agent.Profile{Kind: "claude", Dir: cfg}}
	a.Cwd = cwd
	l := &Loader{}
	now := time.Now()
	l.compactions([]*Agent{a}, nil, now)
	if a.Compaction != (agent.Compaction{Window: 400_000, Headroom: 33_000}) {
		t.Fatalf("from your settings: %+v", a.Compaction)
	}
	// The project's just-yours file wins, once the last reading is old.
	os.WriteFile(filepath.Join(cwd, ".claude", "settings.local.json"), []byte(`{"env":{"CLAUDE_CODE_AUTO_COMPACT_WINDOW":"300000"}}`), 0o600)
	b := &Agent{Key: "k", Kind: "claude", Past: true, Acct: a.Acct}
	b.Cwd = cwd
	l.compactions([]*Agent{b}, nil, now.Add(time.Second))
	if b.Compaction.Window != 400_000 {
		t.Fatalf("kept for a while: %+v", b.Compaction)
	}
	l.compactions([]*Agent{b}, nil, now.Add(compactEvery))
	if b.Compaction.Window != 300_000 {
		t.Fatalf("read again: %+v", b.Compaction)
	}
	l.compactions(nil, nil, now)
	if len(l.compact) != 0 {
		t.Fatal("a row no longer listed keeps nothing")
	}
}
