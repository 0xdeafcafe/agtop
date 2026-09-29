package efficiency

import (
	"path/filepath"

	"github.com/0xdeafcafe/agtop/internal/agent"
)

// MemoryFindings are what's untidy in the memory and instructions a
// session of p's in cwd loads, as its agent's MemoryReader says: each says
// what to do, and enter opens the file to do it in.
func MemoryFindings(p agent.Profile, cwd string) []Finding {
	mr, ok := agent.As[agent.MemoryReader](p.Kind)
	if p.Dir == "" || cwd == "" || !ok {
		return nil
	}
	var out []Finding
	for i, pr := range mr.Memory(p, cwd, "").Problems {
		out = append(out, Finding{
			Title:  pr.Title,
			Detail: "In " + filepath.Base(cwd) + ": " + pr.Fix,
			Open:   pr.Path,
			rank:   2 + i,
		})
	}
	return out
}
