package efficiency

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xdeafcafe/agtop/internal/claude"
)

// testAccount is an account in a folder of its own, on a machine with no
// programs: what's installed here mustn't change what tests see.
func testAccount(t *testing.T, settings string) claude.Account {
	t.Helper()
	t.Setenv("PATH", "")
	was := searchPath
	searchPath = func() []string { return nil }
	t.Cleanup(func() { searchPath = was })
	dir := t.TempDir()
	if settings != "" {
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(settings), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return claude.Account{Name: "test", ConfigDir: dir}
}

func TestDetect(t *testing.T) {
	a := testAccount(t, `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"rtk hook claude"}]}]},
		"enabledPlugins":{"caveman@caveman":true},
		"bashOutputMaxChars":15000,
		"env":{"CLAUDE_CODE_SUBAGENT_MODEL":"sonnet"}}`)
	_ = os.MkdirAll(filepath.Join(a.ConfigDir, "plugins"), 0o700)
	_ = os.WriteFile(filepath.Join(a.ConfigDir, "plugins", "installed_plugins.json"),
		[]byte(`{"version":2,"plugins":{"caveman@caveman":[{"scope":"user","installedAt":"2026-09-01T10:00:00Z"}]}}`), 0o600)
	_ = os.WriteFile(filepath.Join(a.ConfigDir, ".claude.json"), []byte(`{"mcpServers":{"serena":{"command":"serena"}}}`), 0o600)
	e := LoadEnv(a)

	if f := e.Detect(Find("rtk")); f.Status == Off {
		t.Fatalf("rtk's hook is in settings.json: %+v", f)
	}
	cave := e.Detect(Find("caveman"))
	if cave.Status != On || cave.Since.IsZero() {
		t.Fatalf("caveman's plugin is on, installed 1 Sep: %+v", cave)
	}
	if f := e.Detect(Find("serena")); f.Status != On {
		t.Fatalf("serena is an MCP server: %+v", f)
	}
	if f := e.Detect(Find("bash-output")); f.Status != On || f.Value != "15000" {
		t.Fatalf("bashOutputMaxChars is set: %+v", f)
	}
	if f := e.Detect(Find("subagent-model")); f.Status != On || f.Value != "sonnet" {
		t.Fatalf("the subagent model is set in env: %+v", f)
	}
	if f := e.Detect(Find("autocompact")); f.Status != Off {
		t.Fatalf("autoCompactWindow isn't set: %+v", f)
	}
}

// Settings savers are changed in place, keeping the rest of the file, and
// logged as agtop's doing.
func TestSettingPlan(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	a := testAccount(t, `{"model":"opus","env":{"KEEP":"1"}}`)
	sv := Find("autocompact")
	p := NewPlan(LoadEnv(a), sv, false)
	if p.Blocked != "" || len(p.Changes) != 1 || !strings.Contains(p.Changes[0], "autoCompactWindow: 400000") {
		t.Fatalf("plan: %+v", p)
	}
	if _, err := p.Run(LoadEnv(a)); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(a.ConfigDir, "settings.json"))
	if !strings.Contains(string(b), `"autoCompactWindow": 400000`) || !strings.Contains(string(b), `"KEEP": "1"`) || !strings.Contains(string(b), `"model": "opus"`) {
		t.Fatalf("settings after: %s", b)
	}
	evs := LoadEvents()
	if len(evs) != 1 || evs[0].Kind != "setting" || evs[0].Source != "agtop" {
		t.Fatalf("events: %+v", evs)
	}
	backups, _ := filepath.Glob(filepath.Join(Dir(), "backups", "*", "settings.json"))
	if len(backups) != 1 {
		t.Fatalf("settings.json wasn't backed up first: %v", backups)
	}
	// Observing afterwards doesn't log it again.
	Observe(LoadEnv(a))
	if n := len(LoadEvents()); n != 1 {
		t.Fatalf("observing logged agtop's own change again: %d events", n)
	}

	rm := NewPlan(LoadEnv(a), sv, true)
	if _, err := rm.Run(LoadEnv(a)); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(filepath.Join(a.ConfigDir, "settings.json"))
	if strings.Contains(string(b), "autoCompactWindow") {
		t.Fatalf("removing left it: %s", b)
	}
}

// A change made outside agtop is noticed and logged; the first look at an
// account logs nothing without an install date.
func TestObserve(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	a := testAccount(t, `{}`)
	Observe(LoadEnv(a))
	if n := len(LoadEvents()); n != 0 {
		t.Fatalf("first look logged %d events", n)
	}
	_ = os.WriteFile(filepath.Join(a.ConfigDir, "settings.json"), []byte(`{"cleanupPeriodDays":90}`), 0o600)
	Observe(LoadEnv(a))
	evs := LoadEvents()
	if len(evs) != 1 || evs[0].Saver != "history" || evs[0].Source != "noticed" {
		t.Fatalf("events: %+v", evs)
	}
}

func TestManualIsBlocked(t *testing.T) {
	a := testAccount(t, "")
	if p := NewPlan(LoadEnv(a), Find("headroom"), false); p.Blocked == "" {
		t.Fatal("headroom is set up by hand; agtop shouldn't run anything")
	}
}
