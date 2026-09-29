package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// With WriteBehind on, a save returns at once and the writer puts the
// newest of each file on disk; a copy kept before is made before the save
// that follows it.
func TestWriteBehind(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	WriteBehind()
	defer func() { behind.Lock(); behind.on = false; behind.Unlock() }()

	s := &Store{}
	s.Config.GroupBy = "old"
	if err := s.SaveConfig(); err != nil {
		t.Fatal(err)
	}
	if err := Flush(); err != nil {
		t.Fatal(err)
	}
	KeepBefore("test")
	for _, g := range []string{"a", "b", "new"} {
		s.Config.GroupBy = g
		_ = s.SaveConfig()
	}
	if err := Flush(); err != nil {
		t.Fatal(err)
	}
	read := func(name string) string {
		b, _ := os.ReadFile(filepath.Join(Dir(), name))
		return string(b)
	}
	if got := read("config.json"); !strings.Contains(got, `"new"`) {
		t.Errorf("config.json is %s, want the newest save", got)
	}
	if got := read("config.json.test"); !strings.Contains(got, `"old"`) {
		t.Errorf("the copy kept before is %s, want the config as it was", got)
	}
}

// The writer encodes state.json from the overlay as it was saved: the view
// changing its maps meanwhile neither shows in that save nor races it.
func TestWriteBehindOverlay(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	WriteBehind()
	defer func() { behind.Lock(); behind.on = false; behind.Unlock() }()

	s := &Store{Overlay: Overlay{Names: map[string]string{"a": "saved"}}}
	if err := s.SaveOverlay(); err != nil {
		t.Fatal(err)
	}
	s.Overlay.Names["a"] = "after"
	if err := Flush(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(Dir(), "state.json"))
	if !strings.Contains(string(b), `"saved"`) {
		t.Errorf("state.json is %s, want the overlay as saved", b)
	}
}
