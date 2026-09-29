package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvName(t *testing.T) {
	for key, want := range map[string]string{
		"copyOnSelect":           "RUSH_COPY_ON_SELECT",
		"theme":                  "RUSH_THEME",
		"hibernate.afterMinutes": "RUSH_HIBERNATE_AFTER_MINUTES",
		"searchTranscriptsOnKey": "RUSH_SEARCH_TRANSCRIPTS_ON_KEY",
	} {
		if got := EnvName(key); got != want {
			t.Errorf("%s: %s, want %s", key, got, want)
		}
	}
}

// A setting from the environment wins for the run, and isn't saved; one
// you change meanwhile is.
func TestEnvSetsAnySetting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("RUSH_HOME", home)
	_ = os.WriteFile(filepath.Join(home, "config.json"), []byte(`{"theme": "dark", "sideWidth": 0.3, "groupBy": "status"}`), 0o600)
	t.Setenv("RUSH_COPY_ON_SELECT", "0")
	t.Setenv("RUSH_THEME", "light")
	t.Setenv("RUSH_HIBERNATE_AFTER_MINUTES", "7")
	t.Setenv("RUSH_SIDE_WIDTH", "0.4")
	t.Setenv("RUSH_GROUP_BY", "agent")
	t.Setenv("RUSH_COLOR_BLIND", "maybe") // not a bool: left alone

	s := Load()
	c := s.Config
	if c.CopiesOnSelect() || c.Theme != "light" || c.Hibernate.AfterMinutes != 7 || c.SideWidth != 0.4 || c.GroupBy != "agent" || c.ColorBlind {
		t.Fatalf("from the environment: %+v", c)
	}
	if name, ok := s.FromEnv("theme"); !ok || name != "RUSH_THEME" {
		t.Fatalf("FromEnv: %q %v", name, ok)
	}
	s.Config.GroupBy = "group" // changed in rush: that's saved
	if err := s.SaveConfig(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(home, "config.json"))
	saved := string(b)
	for _, want := range []string{`"theme": "dark"`, `"sideWidth": 0.3`, `"groupBy": "group"`} {
		if !strings.Contains(saved, want) {
			t.Errorf("saved lacks %s:\n%s", want, saved)
		}
	}
	for _, not := range []string{"copyOnSelect", `"afterMinutes": 7`, "light"} {
		if strings.Contains(saved, not) {
			t.Errorf("saved has %s from the environment:\n%s", not, saved)
		}
	}
}
