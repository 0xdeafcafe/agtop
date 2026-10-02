package claude

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/settingsfile"
)

func settingsOf(t *testing.T, body string) *settingsfile.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := settingsfile.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCompaction(t *testing.T) {
	for _, tc := range []struct {
		name     string
		env      []string
		settings []string
		state    string
		want     agent.Compaction
	}{
		{name: "nothing set is the model's window"},
		{name: "the environment", env: []string{"CLAUDE_CODE_AUTO_COMPACT_WINDOW=400000"}, want: agent.Compaction{Window: 400_000, Headroom: 33_000}},
		{name: "a settings env block", settings: []string{`{"env":{"CLAUDE_CODE_AUTO_COMPACT_WINDOW":"400000"}}`}, want: agent.Compaction{Window: 400_000, Headroom: 33_000}},
		{name: "a settings env block over the process's", env: []string{"CLAUDE_CODE_AUTO_COMPACT_WINDOW=200000"},
			settings: []string{`{"env":{"CLAUDE_CODE_AUTO_COMPACT_WINDOW":"300000"}}`}, want: agent.Compaction{Window: 300_000, Headroom: 33_000}},
		{name: "the project's just-yours file last", settings: []string{`{"autoCompactWindow":500000}`, `{"autoCompactWindow":300000}`, `{"autoCompactWindow":250000}`},
			want: agent.Compaction{Window: 250_000, Headroom: 33_000}},
		{name: "the environment over autoCompactWindow", env: []string{"CLAUDE_CODE_AUTO_COMPACT_WINDOW=600000"}, settings: []string{`{"autoCompactWindow":300000}`},
			want: agent.Compaction{Window: 600_000, Headroom: 33_000}},
		{name: "an invalid environment leaves autoCompactWindow", env: []string{"CLAUDE_CODE_AUTO_COMPACT_WINDOW=big"}, settings: []string{`{"autoCompactWindow":300000}`},
			want: agent.Compaction{Window: 300_000, Headroom: 33_000}},
		{name: "a setting out of range is dropped", settings: []string{`{"autoCompactWindow":50000}`}},
		{name: "too small is raised to the least", env: []string{"CLAUDE_CODE_AUTO_COMPACT_WINDOW=50000"}, want: agent.Compaction{Window: 100_000, Headroom: 33_000}},
		{name: "too big is capped", env: []string{"CLAUDE_CODE_AUTO_COMPACT_WINDOW=5000000"}, want: agent.Compaction{Window: 1_000_000, Headroom: 33_000}},
		{name: "parsed as parseInt does", env: []string{"CLAUDE_CODE_AUTO_COMPACT_WINDOW=400k"}, want: agent.Compaction{Window: 100_000, Headroom: 33_000}},
		{name: "a smaller output limit leaves less room", env: []string{"CLAUDE_CODE_AUTO_COMPACT_WINDOW=400000", "CLAUDE_CODE_MAX_OUTPUT_TOKENS=8000"},
			want: agent.Compaction{Window: 400_000, Headroom: 21_000}},
		{name: "auto-compact off", env: []string{"CLAUDE_CODE_AUTO_COMPACT_WINDOW=400000", "DISABLE_AUTO_COMPACT=1"}},
		{name: "compaction off", settings: []string{`{"env":{"DISABLE_COMPACT":"true"},"autoCompactWindow":300000}`}},
		{name: "auto-compact off in /config", env: []string{"CLAUDE_CODE_AUTO_COMPACT_WINDOW=400000"}, state: `{"autoCompactEnabled":false}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := CompactSource{Env: tc.env}
			for _, s := range tc.settings {
				src.Settings = append(src.Settings, settingsOf(t, s))
			}
			if tc.state != "" {
				src.State = settingsOf(t, tc.state)
			}
			if got := Compaction(src); got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestReadSettingsCached(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if ReadSettingsCached(path) != nil {
		t.Fatal("a missing file")
	}
	os.WriteFile(path, []byte(`{"autoCompactWindow":300000}`), 0o600)
	first := ReadSettingsCached(path)
	if first == nil || ReadSettingsCached(path) != first {
		t.Fatal("an unchanged file is read once")
	}
	os.WriteFile(path, []byte(`{"autoCompactWindow":400000,"model":"opus"}`), 0o600)
	var n int
	if s := ReadSettingsCached(path); s == nil || !s.Get("autoCompactWindow", &n) || n != 400_000 {
		t.Fatal("a changed file is read again")
	}
}
