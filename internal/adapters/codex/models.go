package codex

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/jsonx"
)

// ListModels are the models Codex offers the account signed in at p, as
// its models cache last had them: the ones its picker lists, with what
// each is for.
func (Adapter) ListModels(p agent.Profile) []agent.Choice {
	b, err := os.ReadFile(filepath.Join(p.Dir, "models_cache.json"))
	if err != nil {
		return nil
	}
	var cache struct {
		Models []struct {
			Slug        string `json:"slug"`
			Visibility  string `json:"visibility"`
			Description string `json:"description"`
		} `json:"models"`
	}
	if jsonx.Unmarshal(b, &cache) != nil {
		return nil
	}
	var out []agent.Choice
	for _, m := range cache.Models {
		if m.Slug != "" && m.Visibility == "list" {
			out = append(out, agent.Choice{ID: m.Slug, Note: strings.TrimSuffix(strings.TrimSpace(m.Description), ".")})
		}
	}
	return out
}
