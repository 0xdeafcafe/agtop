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
// registry (agent.Supports, an adapter interface, agent.ProgramOf).
var namedYet = map[string]string{
	// The efficiency savers are Claude Code plugins and MCP servers, and
	// their install recipes run its own CLI: `claude plugin install`,
	// `claude mcp add`. They stay named for as long as the savers are
	// Claude Code's.
	"internal/efficiency/catalog.go": "savers are Claude Code plugins, installed with its CLI",
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
