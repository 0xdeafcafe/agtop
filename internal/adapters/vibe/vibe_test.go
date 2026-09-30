package vibe

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/0xdeafcafe/rush/internal/agent"
)

// Vibe runs once its key is where Vibe looks: the environment, the
// keychain or its home's .env.
func TestCheckKey(t *testing.T) {
	keychained = func() bool { return false }
	t.Setenv(keyEnv, "")
	dir := t.TempDir()
	p := agent.Profile{Kind: Kind, Dir: dir}
	if err := (Adapter{}).CheckKey(p); err != errNoKey {
		t.Errorf("no key: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("OTHER=1\nexport MISTRAL_API_KEY='k'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (Adapter{}).CheckKey(p); err != nil {
		t.Errorf("key in .env: %v", err)
	}
	t.Setenv(keyEnv, "k")
	if err := (Adapter{}).CheckKey(agent.Profile{Dir: t.TempDir()}); err != nil {
		t.Errorf("key in the environment: %v", err)
	}
}
