package cross

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Claude Code on another provider goes to its URL with its key, never
// your own Anthropic key; a model picked stands in for Claude's names.
func TestClaudeEnv(t *testing.T) {
	env := claudeEnv("https://api.deepseek.com/anthropic", "sk-1", "")
	for _, want := range []string{"ANTHROPIC_BASE_URL=https://api.deepseek.com/anthropic", "ANTHROPIC_AUTH_TOKEN=sk-1", "ANTHROPIC_API_KEY="} {
		if !slices.Contains(env, want) {
			t.Errorf("missing %s in %v", want, env)
		}
	}
	if slices.ContainsFunc(env, func(e string) bool { return strings.HasPrefix(e, "ANTHROPIC_MODEL=") }) {
		t.Error("with no model picked, the provider maps Claude's names itself")
	}
	if !slices.Contains(claudeEnv("u", "k", "glm-4.6"), "ANTHROPIC_DEFAULT_OPUS_MODEL=glm-4.6") {
		t.Error("a model picked stands in for opus")
	}
}

// Pi reads a provider it knows from the key's variable, and one it
// doesn't from a models.json only you can read.
func TestPiSetup(t *testing.T) {
	dir := t.TempDir()
	var anthropic, deepseek Pair
	for _, p := range pairs {
		switch p.kind {
		case "anthropic-pi":
			anthropic = p
		case "deepseek-pi":
			deepseek = p
		}
	}
	if env, err := anthropic.piSetup(dir, "sk-a"); err != nil || len(env) != 1 || env[0] != "ANTHROPIC_API_KEY=sk-a" {
		t.Errorf("anthropic: %v %v", env, err)
	}
	if _, err := deepseek.piSetup(dir, "sk-d"); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(dir, "models.json")
	b, _ := os.ReadFile(f)
	if !strings.Contains(string(b), `"rush-deepseek"`) || !strings.Contains(string(b), "sk-d") || !strings.Contains(string(b), "deepseek-chat") {
		t.Errorf("models.json: %s", b)
	}
	if st, _ := os.Stat(f); st.Mode().Perm() != 0o600 {
		t.Errorf("the key's file is %v", st.Mode().Perm())
	}
}
