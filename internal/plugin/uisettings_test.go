package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestSettings(t *testing.T) {
	home, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("AGTOP_HOME", home)
	m := Manifest{Name: "drafts", Command: []string{"bin"}, Settings: []SettingSpec{
		{Key: "on", Title: "On", Type: "bool"},
		{Key: "mode", Title: "Mode", Type: "choice", Choices: []string{"a", "b"}, Default: "b"},
		{Key: "greeting", Title: "Greeting", Type: "text", Default: "hi"},
	}}
	p, err := Load(install(t, m, nil))
	if err != nil {
		t.Fatal(err)
	}

	if err := SetSetting("drafts", "on", "true"); err == nil {
		t.Fatal("set a setting for a plugin that isn't approved")
	}
	if err := Approve(p); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"on": "false", "mode": "b", "greeting": "hi"}
	if got := SettingValues("drafts"); !sameValues(got, want) {
		t.Fatalf("defaults = %v, want %v", got, want)
	}

	for _, c := range []struct{ key, value string }{
		{"on", "yes"}, {"mode", "c"}, {"greeting", strings.Repeat("x", MaxSettingText+1)},
		{"greeting", "a\x1b[31mred"}, {"greeting", "\xff"}, {"missing", "x"},
	} {
		if err := SetSetting("drafts", c.key, c.value); err == nil {
			t.Errorf("%s = %q was taken", c.key, c.value)
		}
	}
	if err := SetSetting("drafts", "on", "true"); err != nil {
		t.Fatal(err)
	}
	if err := SetSetting("drafts", "greeting", "héllo there"); err != nil {
		t.Fatal(err)
	}
	want = map[string]string{"on": "true", "mode": "b", "greeting": "héllo there"}
	if got := SettingValues("drafts"); !sameValues(got, want) {
		t.Fatalf("values = %v, want %v", got, want)
	}
	fi, err := os.Stat(filepath.Join(Root(), "settings.json"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("settings.json: %v %v", fi, err)
	}

	// Writers taking turns lose nothing.
	m2 := Manifest{Name: "other", Command: []string{"bin"}, Settings: []SettingSpec{{Key: "n", Title: "N", Type: "text"}}}
	p2, _ := Load(install(t, m2, nil))
	if err := Approve(p2); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			if i%2 == 0 {
				_ = SetSetting("other", "n", "v")
			} else {
				_ = SetSetting("drafts", "mode", "a")
			}
		})
	}
	wg.Wait()
	if SettingValues("other")["n"] != "v" || SettingValues("drafts")["mode"] != "a" || SettingValues("drafts")["on"] != "true" {
		t.Fatalf("lost a write: %v %v", SettingValues("other"), SettingValues("drafts"))
	}
	// A stored value the manifest no longer allows reads as the default.
	_ = os.WriteFile(filepath.Join(Root(), "settings.json"), []byte(`{"drafts":{"on":"maybe","mode":"z"}}`), 0o600)
	if got := SettingValues("drafts"); got["on"] != "false" || got["mode"] != "b" {
		t.Fatalf("bad stored values = %v", got)
	}
	if len(SettingValues("nobody")) != 0 {
		t.Fatal("an unapproved plugin has values")
	}
}

func sameValues(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
