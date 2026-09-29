package lint

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// Hot finds allocation and cleanup that costs more each time round: a
// regexp compiled on every call, a defer that waits for the whole function
// inside a loop, and a string grown with += in a loop.
var Hot = &analysis.Analyzer{
	Name: "rushhot",
	Doc:  "reports regexps compiled per call, defer in loops, and strings built with += in loops",
	Run:  runHot,
}

var compiles = map[string]bool{"MustCompile": true, "Compile": true, "MustCompilePOSIX": true, "CompilePOSIX": true}

func runHot(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		if isTest(pass, f) {
			continue
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			walkHot(pass, fd.Body, 0)
		}
	}
	return nil, nil
}

// walkHot looks through n; loops is how many loops it's inside, in this
// function (a func literal starts again at none).
func walkHot(pass *analysis.Pass, n ast.Node, loops int) {
	ast.Inspect(n, func(c ast.Node) bool {
		if c == n {
			return true
		}
		switch c := c.(type) {
		case *ast.FuncLit:
			walkHot(pass, c.Body, 0)
			return false
		case *ast.ForStmt:
			walkHot(pass, c.Body, loops+1)
			return false
		case *ast.RangeStmt:
			walkHot(pass, c.Body, loops+1)
			return false
		case *ast.DeferStmt:
			if loops > 0 {
				pass.Reportf(c.Pos(), "defer in a loop waits for the function to return: what it frees piles up, one per turn")
			}
		case *ast.AssignStmt:
			if loops > 0 && c.Tok == token.ADD_ASSIGN && len(c.Lhs) == 1 {
				if b, ok := pass.TypesInfo.TypeOf(c.Lhs[0]).Underlying().(*types.Basic); ok && b.Info()&types.IsString != 0 {
					pass.Reportf(c.Pos(), "string += in a loop copies the whole string each turn: use a strings.Builder")
				}
			}
		case *ast.CallExpr:
			if pkg, name, _ := callee(pass.TypesInfo, c); pkg == "regexp" && compiles[name] && len(c.Args) == 1 {
				if tv, ok := pass.TypesInfo.Types[c.Args[0]]; ok && tv.Value != nil {
					pass.Reportf(c.Pos(), "regexp.%s of a constant on every call: compile it once, in a package var", name)
				}
			}
		}
		return true
	})
}
