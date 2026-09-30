package codex

import (
	"os"
	"path/filepath"
	"strings"
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

// A model goes to Codex by its id: named by its last word (astra) it's
// found, and one Codex hasn't (a Claude model) is refused, saying which
// it has, rather than sent to a server that refuses the turn.
func TestModelID(t *testing.T) {
	models := []cachedModel{{Slug: "gpt-6-astra", Visibility: "list"}, {Slug: "gpt-reserve", Visibility: "hide"}, {Slug: "gpt-6-luna", Visibility: "list"}}
	for in, want := range map[string]string{"astra": "gpt-6-astra", "GPT-6-Luna": "gpt-6-luna", "gpt-reserve": "gpt-reserve", "": ""} {
		if got, err := modelID(in, models); err != nil || got != want {
			t.Errorf("%q: %q, %v", in, got, err)
		}
	}
	if _, err := modelID("opus[1m]", models); err == nil || !strings.Contains(err.Error(), "gpt-6-astra, gpt-6-luna") {
		t.Errorf("a Claude model: %v", err)
	}
	if got, err := modelID("anything", nil); err != nil || got != "anything" {
		t.Errorf("with no cache it should stand: %q %v", got, err)
	}
}
