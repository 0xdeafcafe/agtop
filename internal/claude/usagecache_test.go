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

func TestUsageSinceDropsResetWindows(t *testing.T) {
	now := time.Now()
	u := Usage{
		FiveHour: Window{Present: true, Percent: 3, ResetsAt: now.Add(-time.Hour)},
		SevenDay: Window{Present: true, Percent: 1, ResetsAt: now.Add(48 * time.Hour)},
	}.Since(now)
	if u.FiveHour.Present {
		t.Fatal("a window that has reset should be dropped")
	}
	if !u.SevenDay.Present || u.SevenDay.Percent != 1 {
		t.Fatalf("seven-day = %+v", u.SevenDay)
	}
}
