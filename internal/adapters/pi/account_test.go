package pi

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/0xdeafcafe/rush/internal/agent"
)

func TestAccountSummary(t *testing.T) {
	dir := t.TempDir()
	p := agent.Profile{Kind: Kind, Dir: dir}
	if s := (Adapter{}).AccountSummary(p); s.SignedIn {
		t.Fatalf("empty dir signed in: %+v", s)
	}
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"defaultProvider":"anthropic"}`), 0o600)
	os.WriteFile(filepath.Join(dir, "auth.json"), []byte(`{"anthropic":{"type":"oauth","access":"x","refresh":"y","expires":1},"openai":{"type":"api_key","key":"k"}}`), 0o600)
	s := (Adapter{}).AccountSummary(p)
	if !s.SignedIn || s.Method != "OAuth · auth.json" || s.Name != "anthropic" || s.Note != "Also signed in to anthropic, openai" {
		t.Fatalf("got %+v", s)
	}
}
