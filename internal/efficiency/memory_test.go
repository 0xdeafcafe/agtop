package efficiency

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xdeafcafe/agtop/internal/claude"
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

	got := MemoryFindings(cfg, cwd)
	if len(got) != 1 {
		t.Fatalf("findings: %+v", got)
	}
	f := got[0]
	if !strings.Contains(f.Title, "doesn't exist") || !strings.HasPrefix(f.Detail, "In shop: ") || f.Open != filepath.Join(mem, "MEMORY.md") || f.Fix != "" {
		t.Fatalf("finding: %+v", f)
	}
	if MemoryFindings(cfg, "") != nil {
		t.Fatal("no folder, no findings")
	}
}
