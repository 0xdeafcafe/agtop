package vibe

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent"
)

func TestAccountSummaryMatchesCurrentCredential(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(keyEnv, "test-key-do-not-display")
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte("test-key-do-not-display")))[:32]
	cache := fmt.Sprintf(`{"%s":{"stored_at_timestamp":%d,"payload":{"plan_type":"pro","plan_name":"Pro","organization_kind":"personal","customer_id":"customer-1","api_base":"https://api.mistral.ai/path?secret=hidden"}},"another-key":{"stored_at_timestamp":%d,"payload":{"plan_name":"Wrong account"}}}`, hash, time.Now().Unix(), time.Now().Unix())
	if err := os.WriteFile(filepath.Join(dir, "whoami_cache.json"), []byte(cache), 0600); err != nil {
		t.Fatal(err)
	}
	summary := (Adapter{}).AccountSummary(agent.Profile{Dir: dir})
	if !summary.SignedIn || summary.Plan != "Pro" || summary.Name != "Customer customer-1" || summary.Endpoint != "https://api.mistral.ai" {
		t.Fatalf("summary: %+v", summary)
	}
	if text := fmt.Sprint(summary); strings.Contains(text, "test-key") || strings.Contains(text, "secret=") {
		t.Fatal("account summary exposed credential data")
	}
}

func TestAccountSummaryUnreadableCredentialsAreUnknown(t *testing.T) {
	old := readStoredKey
	readStoredKey = func() []byte { return nil }
	defer func() { readStoredKey = old }()
	t.Setenv(keyEnv, "")
	s := (Adapter{}).AccountSummary(agent.Profile{Dir: t.TempDir()})
	if s.SignedIn || s.Status != "unknown" || strings.Contains(s.Note, "No Mistral credentials found") {
		t.Fatalf("unavailable treated as signed out: %+v", s)
	}
}
