package settingsfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Detach hands the changes so far to a File saved elsewhere: the one
// still being edited keeps showing them, and doesn't write them twice.
func TestDetach(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte(`{"model":"opus","env":{"A":"1"}}`), 0o600)
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Set("env.B", "2")
	d := s.Detach()
	if s.String("env.B") != "2" {
		t.Fatal("the edited one should still show the change")
	}
	// The file changes on disk before the save: that's kept.
	os.WriteFile(path, []byte(`{"model":"sonnet","env":{"A":"1"}}`), 0o600)
	if err := d.Save(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), `"B": "2"`) || !strings.Contains(string(b), "sonnet") {
		t.Fatalf("saved:\n%s", b)
	}
	if len(s.changes) != 0 {
		t.Fatal("the changes should have gone with the detached one")
	}
}
