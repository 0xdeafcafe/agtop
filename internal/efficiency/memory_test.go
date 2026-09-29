package efficiency_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/0xdeafcafe/rush/internal/adapters/claude"
	"github.com/0xdeafcafe/rush/internal/adapters/claude/claude"
	"github.com/0xdeafcafe/rush/internal/agent"
	. "github.com/0xdeafcafe/rush/internal/efficiency"
)

func TestMemoryFindings(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	cfg, cwd := filepath.Join(root, "cfg"), filepath.Join(root, "src", "shop")
	mem := filepath.Join(cfg, "projects", claude.ProjectSlug(cwd), "memory")
	if err := os.MkdirAll(mem, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(mem, "MEMORY.md"), []byte("- [Gone](gone.md) — deleted\n"), 0o644)

	got := MemoryFindings(agent.Profile{Kind: Agent, Dir: cfg}, cwd)
	if len(got) != 1 {
		t.Fatalf("findings: %+v", got)
	}
	f := got[0]
	if !strings.Contains(f.Title, "doesn't exist") || !strings.HasPrefix(f.Detail, "In shop: ") || f.Open != filepath.Join(mem, "MEMORY.md") || f.Fix != "" {
		t.Fatalf("finding: %+v", f)
	}
	if MemoryFindings(agent.Profile{Kind: Agent, Dir: cfg}, "") != nil {
		t.Fatal("no folder, no findings")
	}
}
