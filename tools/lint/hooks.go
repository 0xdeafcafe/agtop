package lint

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Hooks keeps rush's UI from talking to plugins other than through
// internal/hooks, whose every method returns at once. A plugin must never
// be able to hold up a frame: a call on the broker's connection, from the
// UI's goroutine, waits on whatever the plugin is doing.
var Hooks = &analysis.Analyzer{
	Name: "rushhooks",
	Doc:  "reports rush's UI reaching the plugin broker other than through internal/hooks",
	Run:  runHooks,
}

// brokerOnly are internal/plugin's ways to the broker and to plugins.
var brokerOnly = map[string]bool{
	"DialBroker": true, "DialBrokerWith": true, "NewConn": true, "NewLineConn": true, "EnsureBroker": true,
}

// brokerTypes are its types whose methods talk to the broker or a plugin.
var brokerTypes = map[string]bool{"Conn": true, "Broker": true}

func runHooks(pass *analysis.Pass) (any, error) {
	path := pass.Pkg.Path()
	if !strings.HasSuffix(path, "/internal/ui") && !strings.Contains(path, "/internal/ui/") {
		return nil, nil
	}
	for _, f := range pass.Files {
		if isTest(pass, f) {
			continue
		}
		for _, imp := range f.Imports {
			if p := strings.Trim(imp.Path.Value, `"`); strings.HasSuffix(p, "/internal/plugind") {
				pass.Reportf(imp.Pos(), "the UI reaches plugins only through internal/hooks, never the broker's package")
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			pkg, name, fn := callee(pass.TypesInfo, call)
			if !strings.HasSuffix(pkg, "/internal/plugin") {
				return true
			}
			if brokerOnly[name] && recvNamed(fn) == "" || brokerTypes[recvNamed(fn)] {
				pass.Reportf(call.Pos(), "plugin.%s talks to the broker and can wait on a plugin: go through internal/hooks, whose methods never block the UI", qualified(fn))
			}
			return true
		})
	}
	return nil, nil
}

func qualified(fn *types.Func) string {
	if r := recvNamed(fn); r != "" {
		return r + "." + fn.Name()
	}
	return fn.Name()
}
