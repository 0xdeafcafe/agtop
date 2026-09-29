package agent

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// namedOK are the packages that are Claude Code's own, and may call it by
// name: its adapter, and what it wraps until that moves into the adapter.
var namedOK = []string{"internal/claude/", "internal/adapters/claude/", "internal/headless/", "internal/daemon/"}

// namedYet are files that still name Claude Code, and why. The list only
// shrinks: a new "claude" belongs behind the Claude adapter, or asks the
// registry (agent.Supports, agent.IsBuiltin, agent.KindOf).
var namedYet = map[string]string{
	// Accounts and state are being reworked into providers and profiles.
	"internal/ui/settings_claude.go": "Claude Code's own page of Settings",
	"internal/state/state.go":        "providers",
	// Search's word for what the agent said: who:claude.
	"internal/convo/search.go": "search syntax",
	// The efficiency savers are Claude Code plugins, installed with its CLI.
	"internal/efficiency/catalog.go": "savers",
	// The advisor runs Claude Code headless to read your figures.
	"internal/advisor/run.go": "advisor",
}

// The core names no agent: "claude" appears only in Claude Code's own
// packages, in lines marked as a migration, and in namedYet.
func TestCoreNamesNoAgent(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, top := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			rel := filepath.ToSlash(strings.TrimPrefix(path, root+string(filepath.Separator)))
			for _, ok := range namedOK {
				if strings.HasPrefix(rel, ok) {
					return nil
				}
			}
			if namedYet[rel] != "" {
				return nil
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if err != nil {
				return err
			}
			migration := map[int]bool{}
			for _, cg := range f.Comments {
				for _, c := range cg.List {
					if strings.Contains(c.Text, "migration") {
						migration[fset.Position(c.Pos()).Line] = true
					}
				}
			}
			ast.Inspect(f, func(n ast.Node) bool {
				if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING && lit.Value == `"claude"` {
					if line := fset.Position(lit.Pos()).Line; !migration[line] {
						t.Errorf("%s:%d names Claude Code: ask the registry, or move it behind the Claude adapter", rel, line)
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
