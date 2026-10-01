package vibe

import (
	"github.com/0xdeafcafe/rush/internal/agent"
	"os"
	"path/filepath"
	"testing"
)

func TestConfiguredModelsAndSessionDiscovery(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VIBE_CONFIG_PATH", "")
	cfg := `active_model = "local"
[[models]]
name = "devstral-custom"
alias = "local"
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}
	a := Adapter{}
	p := agent.Profile{Dir: dir}
	models := a.ListModels(p)
	if len(models) != 2 || models[0].ID != "local" {
		t.Fatalf("models: %+v", models)
	}
	root := filepath.Join(dir, "logs", "session", "saved")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "meta.json"), []byte(`{"session_id":"s","title":"Review","start_time":"2026-01-01T00:00:00Z","environment":{"working_directory":"/work"},"config":{"active_model":"local"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "messages.jsonl"), []byte(`{"role":"user","content":"hi"}`), 0600); err != nil {
		t.Fatal(err)
	}
	past := a.Past(p)
	if len(past) != 1 || past[0].Model != "local" || past[0].Cwd != "/work" || past[0].Name != "Review" {
		t.Fatalf("past: %+v", past)
	}
}

func TestDefaultAndAllowedModels(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VIBE_CONFIG_PATH", "")
	a := Adapter{}
	p := agent.Profile{Dir: dir}
	models := a.ListModels(p)
	if len(models) < 2 {
		t.Fatalf("fresh onboarding has no built-ins: %+v", models)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("allowed_models = [\"mistral-*\"]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	models = a.ListModels(p)
	if len(models) != 1 || models[0].ID != "mistral-medium-3.5" {
		t.Fatalf("allowed catalog: %+v", models)
	}
	if len(a.Choices().Models) != 0 {
		t.Fatal("Choices must not read the model catalog on the UI goroutine")
	}
}
