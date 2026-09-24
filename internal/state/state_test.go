package state

import (
	"os"
	"path/filepath"
	"testing"
)

// A config.json that can't be read comes back as it was last read whole,
// rather than empty, so the next save doesn't wipe your settings.
func TestLoadFallsBackToLastGood(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGTOP_HOME", dir)
	s := Load()
	s.Config.SortBy = "cost"
	s.Config.Accounts = append(s.Config.Accounts, s.Config.ActiveAccount())
	if err := s.SaveConfig(); err != nil {
		t.Fatal(err)
	}
	Load() // read whole: kept as the last good copy
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"sortBy":"cost"}  "groupBy":`), 0o600); err != nil {
		t.Fatal(err)
	}
	s = Load()
	if s.Config.SortBy != "cost" || len(s.Config.Accounts) != 1 {
		t.Fatalf("got %+v, want the last good config", s.Config)
	}
	if _, err := os.Stat(path + ".broken"); err != nil {
		t.Fatalf("the unreadable config wasn't kept aside: %v", err)
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "*.tmp")); len(m) > 0 {
		t.Fatalf("temp files left behind: %v", m)
	}
}
