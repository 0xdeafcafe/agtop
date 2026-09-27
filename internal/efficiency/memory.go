package efficiency

import (
	"path/filepath"

	"github.com/0xdeafcafe/agtop/internal/claude"
)

// MemoryFindings are what's untidy in the memory and instructions a
// session in cwd loads, for the account in cfg: each says what to do, and
// enter opens the file to do it in.
func MemoryFindings(cfg, cwd string) []Finding {
	if cfg == "" || cwd == "" {
		return nil
	}
	r := claude.CheckMemory(cfg, cwd, filepath.Join(cfg, "projects", claude.ProjectSlug(cwd)))
	var out []Finding
	for i, p := range r.Problems {
		out = append(out, Finding{
			Title:  p.Title,
			Detail: "In " + filepath.Base(cwd) + ": " + p.Fix,
			Open:   p.Path,
			rank:   2 + i,
		})
	}
	return out
}
