package acp

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent"
)

func TestKimiQuotaOf(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	body := []byte(`{"user":{"membership":{"level":"LEVEL_INTERMEDIATE"}},
		"usage":{"limit":"100","remaining":"74","resetTime":"2026-10-05T00:00:00.123456789Z"},
		"limits":[{"window":{"duration":300,"timeUnit":"TIME_UNIT_MINUTE"},"detail":{"limit":"100","used":"40","reset_in":3600}}]}`)
	q, err := kimiQuotaOf(body, now)
	if err != nil {
		t.Fatal(err)
	}
	if q.Plan != "Kimi Code · Intermediate" || len(q.Windows) != 2 {
		t.Fatalf("got %+v", q)
	}
	w, f := q.Windows[0], q.Windows[1]
	if w.Label != "7d" || w.Used != 26 || w.Percent != 26 || w.ResetsAt.Day() != 5 {
		t.Errorf("weekly: %+v", w)
	}
	if f.Label != "5h" || f.Span != 5*time.Hour || f.Percent != 40 || !f.ResetsAt.Equal(now.Add(time.Hour)) {
		t.Errorf("5h: %+v", f)
	}
}

func TestKimiCredential(t *testing.T) {
	dir := t.TempDir()
	cfg := `default_model = "kimi-code/kimi-for-coding"
[models."kimi-code/kimi-for-coding"]
provider = "managed:kimi-code"
model = "kimi-for-coding"
max_context_size = 262144
[providers."managed:kimi-code"]
type = "kimi"
base_url = "https://api.kimi.com/coding/v1"
api_key = ""
oauth = { storage = "file", key = "oauth/kimi-code" }
`
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte(cfg), 0o600)
	if s := (Kimi{}).AccountSummary(kimiProfile(dir)); s.SignedIn {
		t.Fatalf("signed in with no token: %+v", s)
	}
	os.MkdirAll(filepath.Join(dir, "credentials"), 0o700)
	os.WriteFile(filepath.Join(dir, "credentials", "kimi-code.json"), []byte(`{"access_token":"a","refresh_token":"r","expires_at":1}`), 0o600)
	s := (Kimi{}).AccountSummary(kimiProfile(dir))
	if !s.SignedIn || s.Plan != "Kimi Code" || s.Endpoint != "https://api.kimi.com" || s.Method != "OAuth · Kimi credentials file" {
		t.Fatalf("got %+v", s)
	}
	if c := kimiCredential(dir); !c.expired {
		t.Error("token from 1970 should read as expired")
	}
}

func kimiProfile(dir string) agent.Profile { return agent.Profile{Kind: "kimi", Dir: dir} }

// Kimi Code keeps ~/.kimi-code; rush reads it there before the old ~/.kimi.
func TestKimiHome(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("KIMI_CODE_HOME", "")
	t.Setenv("KIMI_SHARE_DIR", "")
	if got := kimiHome(); got != filepath.Join(h, ".kimi") {
		t.Fatalf("without Kimi Code: %s", got)
	}
	os.MkdirAll(filepath.Join(h, ".kimi-code"), 0o700)
	os.WriteFile(filepath.Join(h, ".kimi-code", "config.toml"), nil, 0o600)
	if got := kimiHome(); got != filepath.Join(h, ".kimi-code") {
		t.Fatalf("with Kimi Code: %s", got)
	}
	t.Setenv("KIMI_CODE_HOME", "/x")
	if got := kimiHome(); got != "/x" {
		t.Fatalf("KIMI_CODE_HOME: %s", got)
	}
}

func TestKimiUnknownAndExpiredCredentialEvidence(t *testing.T) {
	dir := t.TempDir()
	if s := (Kimi{}).AccountSummary(kimiProfile(dir)); s.Status != "unknown" || s.SignedIn {
		t.Fatalf("missing config should be unknown: %+v", s)
	}
	cfg := `default_model="active"
[models.active]
provider="managed:kimi-code"
[providers."managed:kimi-code"]
oauth={storage="file",key="oauth/kimi-code"}
`
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte(cfg), 0600)
	os.MkdirAll(filepath.Join(dir, "credentials"), 0700)
	file := filepath.Join(dir, "credentials", "kimi-code.json")
	os.WriteFile(file, []byte(`{"access_token":"expired","expires_at":1}`), 0600)
	if s := (Kimi{}).AccountSummary(kimiProfile(dir)); s.Status != "access token expired" {
		t.Fatalf("expired nonrefreshable credential: %+v", s)
	}
	os.WriteFile(file, []byte(`{"access_token":"expired","refresh_token":"refresh","expires_at":1}`), 0600)
	if s := (Kimi{}).AccountSummary(kimiProfile(dir)); !s.SignedIn || s.Status != "credentials available" {
		t.Fatalf("refreshable credential missing: %+v", s)
	}
	os.WriteFile(file, []byte(`{"access_token":`), 0600)
	if s := (Kimi{}).AccountSummary(kimiProfile(dir)); s.Status != "unknown" || s.SignedIn {
		t.Fatalf("unreadable token treated as signed out: %+v", s)
	}
}

func TestKimiAccountDoesNotBorrowDifferentModelsCredential(t *testing.T) {
	dir := t.TempDir()
	cfg := `default_model="active"
[models.active]
provider="active-provider"
[models.other]
provider="other-provider"
[providers.active-provider]
type="kimi"
[providers.other-provider]
api_key="other-account-key"
`
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte(cfg), 0600)
	s := (Kimi{}).AccountSummary(kimiProfile(dir))
	if s.SignedIn || s.Status != "unknown" {
		t.Fatalf("borrowed another model's account: %+v", s)
	}
}
