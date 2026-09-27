package usage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestRefreshReadsOnceInAWhile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quotas.json")
	reads := 0
	fetch := func(context.Context) (Quota, error) {
		reads++
		return Quota{Windows: []Window{{ID: "primary", Percent: 30}}}, nil
	}
	q := Refresh(path, "codex:a", false, fetch)
	if reads != 1 || q.Used("") != 30 || q.Account != "codex:a" || q.FetchedAt.IsZero() {
		t.Fatalf("first refresh: %d reads, %+v", reads, q)
	}
	if q = Refresh(path, "codex:a", false, fetch); reads != 1 || q.Used("") != 30 {
		t.Errorf("a fresh reading was read again: %d reads", reads)
	}
	if q = Refresh(path, "codex:b", true, fetch); reads != 1 || len(q.Windows) != 0 {
		t.Errorf("offline read: %d reads, %+v", reads, q)
	}

	// An older reading doesn't replace a newer one.
	old := Load(path)["codex:a"]
	old.FetchedAt = time.Now().Add(-time.Hour)
	if err := Record(path, "codex:a", old); err != nil || Load(path)["codex:a"].FetchedAt.Equal(old.FetchedAt) {
		t.Errorf("an older reading replaced a newer one")
	}
	failed := Refresh(filepath.Join(t.TempDir(), "q.json"), "x", false, func(context.Context) (Quota, error) { return Quota{}, errors.New("signed out") })
	if failed.Problem != "signed out" {
		t.Errorf("failed read = %+v", failed)
	}
}
