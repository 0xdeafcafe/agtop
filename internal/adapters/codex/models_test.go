package codex

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/0xdeafcafe/rush/internal/agent"
)

// The models Codex lists are its picker's, hidden ones left out.
func TestListModels(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "models_cache.json"), []byte(`{"models":[
		{"slug":"gpt-6-astra","visibility":"list","description":"Frontier intelligence."},
		{"slug":"gpt-reserve","visibility":"hide","description":"Hidden."},
		{"slug":"gpt-6-luna","visibility":"list","description":"Fast and cheap."}]}`), 0o600)
	got := Adapter{}.ListModels(agent.Profile{Dir: dir})
	if len(got) != 2 || got[0].ID != "gpt-6-astra" || got[0].Note != "Frontier intelligence" || got[1].ID != "gpt-6-luna" {
		t.Errorf("listed %+v", got)
	}
}
