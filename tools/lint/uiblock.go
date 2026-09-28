package lint

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// UIBlock finds work that blocks in agtop's UI: a ui.Model method that
// reads a file, runs a command or sleeps holds up every frame until it's
// done. That work belongs in a tea.Cmd (a func returning tea.Msg), which
// bubbletea runs off the UI goroutine, or a goroutine.
var UIBlock = &analysis.Analyzer{
	Name: "agtopui",
	Doc:  "reports blocking calls (files, commands, network, sleep) made directly on agtop's UI goroutine",
	Run:  runUIBlock,
}

var blocking = map[string]map[string]bool{
	"os": {"ReadFile": true, "WriteFile": true, "Open": true, "OpenFile": true, "Create": true, "Stat": true, "Lstat": true,
		"ReadDir": true, "Remove": true, "RemoveAll": true, "Rename": true, "Mkdir": true, "MkdirAll": true, "Chtimes": true,
		"Readlink": true, "Symlink": true, "CreateTemp": true, "MkdirTemp": true},
	"io":            {"ReadAll": true, "Copy": true},
	"path/filepath": {"Walk": true, "WalkDir": true, "Glob": true, "EvalSymlinks": true},
	"net/http":      {"Get": true, "Post": true, "Head": true, "Do": true},
	"time":          {"Sleep": true},
	"os/exec":       {"Run": true, "Output": true, "CombinedOutput": true, "Wait": true},
}

func runUIBlock(pass *analysis.Pass) (any, error) {
	if !strings.HasSuffix(pass.Pkg.Path(), "/internal/ui") {
		return nil, nil
	}
	for _, f := range pass.Files {
		if isTest(pass, f) {
			continue
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || fd.Body == nil {
				continue
			}
			if fn, _ := pass.TypesInfo.Defs[fd.Name].(*types.Func); fn == nil || recvNamed(fn) != "Model" {
				continue
			}
			// A func literal runs where it's called: now only if it's called
			// at once (or deferred). One returned or handed on, a tea.Cmd or
			// cmdErr's work, runs off the UI goroutine.
			now := map[*ast.FuncLit]bool{}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.GoStmt:
					return false // runs off the UI goroutine
				case *ast.FuncLit:
					return now[n]
				case *ast.CallExpr:
					if fl, ok := n.Fun.(*ast.FuncLit); ok {
						now[fl] = true
					}
					if pkg, name, _ := callee(pass.TypesInfo, n); blocking[pkg][name] {
						pass.Reportf(n.Pos(), "%s.%s blocks agtop's UI in a Model method: do it in a tea.Cmd", pkg[strings.LastIndex(pkg, "/")+1:], name)
					}
				}
				return true
			})
		}
	}
	return nil, nil
}
