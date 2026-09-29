// Package lint is rush's own checks, as a golangci-lint module plugin (see
// .custom-gcl.yml): what the stock linters don't know about rush.
package lint

import (
	"go/ast"
	"go/types"
	"strings"

	"github.com/golangci/plugin-module-register/register"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"
)

func init() { register.Plugin("rush", New) }

type plugin struct{}

// New is the plugin; it takes no settings.
func New(any) (register.LinterPlugin, error) { return plugin{}, nil }

func (plugin) BuildAnalyzers() ([]*analysis.Analyzer, error) {
	return Analyzers, nil
}

func (plugin) GetLoadMode() string { return register.LoadModeTypesInfo }

// Analyzers are every check.
var Analyzers = []*analysis.Analyzer{JSON, UIBlock, Hot, Read, Hooks}

// callee is the package path and name of the function or method call
// calls, when it's a static one.
func callee(info *types.Info, call *ast.CallExpr) (pkg, name string, fn *types.Func) {
	fn, _ = typeutil.Callee(info, call).(*types.Func)
	if fn == nil || fn.Pkg() == nil {
		return "", "", nil
	}
	return fn.Pkg().Path(), fn.Name(), fn
}

// recvNamed is the name of a method's receiver type, pointer or not.
func recvNamed(fn *types.Func) string {
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return ""
	}
	t := sig.Recv().Type()
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if n, ok := t.(*types.Named); ok {
		return n.Obj().Name()
	}
	return ""
}

func isTest(pass *analysis.Pass, n ast.Node) bool {
	return strings.HasSuffix(pass.Fset.File(n.Pos()).Name(), "_test.go")
}
