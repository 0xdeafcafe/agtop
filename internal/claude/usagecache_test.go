package claude

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFetchedUsageKeepsOtherAccounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	at := time.Now().Truncate(time.Second)
	if err := SaveFetchedUsage(path, "/a", FetchedUsage{Usage: Usage{FetchedAt: at, SevenDay: Window{Present: true, Percent: 50}}}); err != nil {
		t.Fatal(err)
	}
	wait := at.Add(time.Hour)
	if err := SaveFetchedUsage(path, "/b", FetchedUsage{Wait: wait}); err != nil {
		t.Fatal(err)
	}
	all := LoadFetchedUsage(path)
	if a := all["/a"]; a.Usage.SevenDay.Percent != 50 || !a.Usage.FetchedAt.Equal(at) {
		t.Fatalf("/a = %+v", a)
	}
	if b := all["/b"]; !b.Wait.Equal(wait) {
		t.Fatalf("/b = %+v", b)
	}
}

func TestUsageSinceEmptiesResetWindows(t *testing.T) {
	now := time.Now()
	u := Usage{
		FiveHour: Window{Present: true, Percent: 3, ResetsAt: now.Add(-time.Hour)},
		SevenDay: Window{Present: true, Percent: 1, ResetsAt: now.Add(48 * time.Hour)},
	}.Since(now)
	if !u.FiveHour.Present || u.FiveHour.Percent != 0 || !u.FiveHour.ResetsAt.IsZero() {
		t.Fatalf("a window that has reset should read empty, got %+v", u.FiveHour)
	}
	if !u.SevenDay.Present || u.SevenDay.Percent != 1 {
		t.Fatalf("seven-day = %+v", u.SevenDay)
	}
}

func TestLiveUsage(t *testing.T) {
	info := []byte(`{"status":"allowed","resetsAt":1790368200,"rateLimitType":"five_hour","unifiedWindows":{"five_hour":{"utilization":0.96,"resetsAt":1790368200},"seven_day":{"utilization":0.26,"resetsAt":1790521200}}}`)
	at := time.Now()
	u, ok := LiveUsage(info, at)
	if !ok || !u.FiveHour.Present || int(u.FiveHour.Percent+0.5) != 96 || int(u.SevenDay.Percent+0.5) != 26 {
		t.Fatalf("got %+v (%v)", u, ok)
	}
	if !u.FiveHour.ResetsAt.Equal(time.Unix(1790368200, 0)) || !u.FetchedAt.Equal(at) {
		t.Fatalf("times: %+v", u)
	}
	if _, ok := LiveUsage([]byte(`{"status":"rejected","resetsAt":1790368200}`), at); ok {
		t.Fatal("info without windows isn't a reading")
	}
}

func TestRecordUsageKeepsNewer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	now := time.Now().Truncate(time.Second)
	wait := now.Add(time.Hour)
	_ = SaveFetchedUsage(path, "k", FetchedUsage{Usage: Usage{FetchedAt: now, FiveHour: Window{Present: true, Percent: 50}}, Wait: wait})
	_ = RecordUsage(path, "k", Usage{FetchedAt: now.Add(-time.Minute), FiveHour: Window{Present: true, Percent: 10}})
	if f := LoadFetchedUsage(path)["k"]; f.Usage.FiveHour.Percent != 50 {
		t.Fatalf("an older reading replaced a newer: %+v", f.Usage)
	}
	_ = RecordUsage(path, "k", Usage{FetchedAt: now.Add(time.Minute), FiveHour: Window{Present: true, Percent: 60}})
	if f := LoadFetchedUsage(path)["k"]; f.Usage.FiveHour.Percent != 60 || !f.Wait.Equal(wait) {
		t.Fatalf("got %+v, want 60%% with the wait kept", f)
	}
}

// A session that started signed in as one login keeps sending readings
// after the folder is switched to another, and Claude Code picks up the new
// sign-in as it goes: those readings must not land on the login it started
// as, or two logins show the same usage.
func TestRecordLiveUsageOnlyWhileStillSignedIn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "usage.json")
	a := Account{ConfigDir: filepath.Join(dir, "cfg")}
	signIn := func(id string) {
		t.Helper()
		if err := os.MkdirAll(a.ConfigDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(a.StatePath(), []byte(`{"oauthAccount":{"accountUuid":"`+id+`"}}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	owner := "alex"
	was := signInOwner
	signInOwner = func(Account) (string, error) { return owner, nil }
	t.Cleanup(func() { signInOwner = was })
	now := time.Now()
	signIn("alex")
	if err := RecordLiveUsage(path, a, "alex", Usage{FetchedAt: now, FiveHour: Window{Present: true, Percent: 10}}); err != nil {
		t.Fatal(err)
	}
	signIn("borrowed")
	if err := RecordLiveUsage(path, a, "alex", Usage{FetchedAt: now.Add(time.Minute), FiveHour: Window{Present: true, Percent: 84}}); err != nil {
		t.Fatal(err)
	}
	all := LoadFetchedUsage(path)
	if got := all[Login{ID: "alex"}.UsageKey()].Usage; got.FiveHour.Percent != 10 || got.AccountID != "alex" {
		t.Fatalf("alex's reading = %+v, want its own 10%%", got)
	}
	if _, ok := all[Login{ID: "borrowed"}.UsageKey()]; ok {
		t.Fatal("a reading whose account can't be told was kept as borrowed's")
	}
}

// A Claude Code started as one login, running on another's sign-in since a
// switch, writes its own name back into the folder: the folder names the
// login it started as, but the readings are the other's.
func TestRecordLiveUsageNotOnAnotherLoginsSignIn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "usage.json")
	a := Account{ConfigDir: filepath.Join(dir, "cfg")}
	if err := os.MkdirAll(a.ConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.StatePath(), []byte(`{"oauthAccount":{"accountUuid":"personal"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	was := signInOwner
	signInOwner = func(Account) (string, error) { return "alex", nil }
	t.Cleanup(func() { signInOwner = was })
	if err := RecordLiveUsage(path, a, "personal", Usage{FetchedAt: time.Now(), FiveHour: Window{Present: true, Percent: 53}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := LoadFetchedUsage(path)[Login{ID: "personal"}.UsageKey()]; ok {
		t.Fatal("alex's reading was kept as personal's")
	}
}
