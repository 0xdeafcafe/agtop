package lint

import (
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Adapters keeps the core agent-neutral: nothing under internal/ but the
// adapters themselves imports one. The core reaches them through the
// registry (internal/agent); the programs (cmd/, tools/) register them,
// and tests may import them to.
var Adapters = &analysis.Analyzer{
	Name: "rushadapters",
	Doc:  "bans importing internal/adapters/... from the core: go through internal/agent",
	Run:  runAdapters,
}

func runAdapters(pass *analysis.Pass) (any, error) {
	path := pass.Pkg.Path()
	if !strings.Contains(path, "/internal/") || strings.Contains(path+"/", "/internal/adapters/") {
		return nil, nil
	}
	for _, f := range pass.Files {
		if isTest(pass, f) {
			continue
		}
		for _, im := range f.Imports {
			if p, _ := strconv.Unquote(im.Path.Value); strings.Contains(p+"/", "/internal/adapters/") {
				pass.Reportf(im.Pos(), "the core imports no adapter: reach %s through internal/agent", p[strings.Index(p, "/internal/")+1:])
			}
		}
	}
	return nil, nil
}
