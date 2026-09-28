package lint

import (
	"go/ast"
	"go/types"
	"regexp"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Read finds reads with no bound: io.ReadAll of a reader nothing limits,
// and os.ReadFile of a transcript, which grows for as long as a session
// runs (hundreds of megabytes): read those a line at a time.
var Read = &analysis.Analyzer{
	Name: "agtopread",
	Doc:  "reports io.ReadAll without a limit, and whole transcripts read with os.ReadFile",
	Run:  runRead,
}

var transcriptish = regexp.MustCompile(`(?i)transcript|jsonl|rollout`)

func runRead(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		if isTest(pass, f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			switch pkg, name, _ := callee(pass.TypesInfo, call); {
			case pkg == "io" && name == "ReadAll" && !limited(pass.TypesInfo, call.Args[0]):
				pass.Reportf(call.Pos(), "io.ReadAll reads however much there is: wrap the reader in io.LimitReader, or stream it")
			case pkg == "os" && name == "ReadFile" && transcriptish.MatchString(types.ExprString(call.Args[0])):
				pass.Reportf(call.Pos(), "os.ReadFile of a transcript holds all of it at once: read it a line at a time (bufio.Reader)")
			}
			return true
		})
	}
	return nil, nil
}

// limited is whether r is io.LimitReader(…), or a *io.LimitedReader.
func limited(info *types.Info, r ast.Expr) bool {
	if c, ok := ast.Unparen(r).(*ast.CallExpr); ok {
		if pkg, name, _ := callee(info, c); pkg == "io" && name == "LimitReader" {
			return true
		}
	}
	return strings.HasSuffix(types.TypeString(info.TypeOf(r), nil), "io.LimitedReader")
}
