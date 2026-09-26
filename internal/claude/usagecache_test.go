package claude

import (
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
