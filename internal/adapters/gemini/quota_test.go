package gemini

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent"
)

// A free Google account's allowance reads as one window a model, the plan
// as free, and an API key or a lapsed token as why there's no reading.
func TestQuota(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, ":loadCodeAssist"):
			w.Write([]byte(`{"currentTier":{"id":"free-tier","name":"Gemini Code Assist for individuals"},"cloudaicompanionProject":"proj-1"}`))
		case strings.HasSuffix(r.URL.Path, ":retrieveUserQuota"):
			w.Write([]byte(`{"buckets":[
				{"modelId":"gemini-2.5-pro","remainingFraction":0.25,"remainingAmount":"25","resetTime":"2026-10-01T00:00:00Z","tokenType":"REQUESTS"},
				{"modelId":"gemini-2.5-flash","remainingFraction":1},
				{"tokenType":"REQUESTS"}]}`))
		}
	}))
	defer srv.Close()
	endpoint = srv.URL + "/v1internal"
	dir := t.TempDir()
	p := agent.Profile{Kind: Kind, Dir: dir}

	if _, err := (Adapter{}).Quota(context.Background(), p, agent.Account{}); err != errNoGoogle {
		t.Fatalf("no sign-in: %v", err)
	}
	creds := func(expiry time.Time) {
		b := `{"access_token":"tok","expiry_date":` + strconv.FormatInt(expiry.UnixMilli(), 10) + `}`
		os.WriteFile(filepath.Join(dir, credsFile), []byte(b), 0o600)
	}
	creds(time.Now().Add(-time.Minute))
	if _, err := (Adapter{}).Quota(context.Background(), p, agent.Account{}); err != errLapsed {
		t.Fatalf("lapsed: %v", err)
	}
	creds(time.Now().Add(time.Hour))
	os.WriteFile(filepath.Join(dir, "google_accounts.json"), []byte(`{"active":"a@example.com","old":[]}`), 0o600)
	q, err := (Adapter{}).Quota(context.Background(), p, agent.Account{})
	if err != nil {
		t.Fatal(err)
	}
	if q.Plan != "free" || q.Email != "a@example.com" || len(q.Windows) != 2 {
		t.Fatalf("got %+v", q)
	}
	pro := q.Windows[0]
	if pro.Label != "2.5-pro" || pro.Percent != 75 || pro.Used != 75 || pro.Limit != 100 || pro.ResetsAt.IsZero() {
		t.Errorf("pro: %+v", pro)
	}
	if q.Used("gemini-2.5-flash") != 0 || q.Used("gemini-2.5-pro") != 75 {
		t.Errorf("each window limits its own model: %+v", q.Windows)
	}
}
