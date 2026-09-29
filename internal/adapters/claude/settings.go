package claude

import (
	"path/filepath"

	"github.com/0xdeafcafe/rush/internal/actions"
	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/settingsfile"
)

// SettingsFiles are yours, then the project's (shared through git) and the
// project's just-for-you one, where Claude Code saves "don't ask again".
func (Adapter) SettingsFiles(p agent.Profile, cwd string) []agent.SettingsFile {
	files := []agent.SettingsFile{{Label: "yours", Path: filepath.Join(p.Dir, "settings.json")}}
	root := actions.RepoRoot(cwd)
	if root == "" {
		root = cwd
	}
	if root != "" {
		files = append(files,
			agent.SettingsFile{Label: "project", Path: filepath.Join(root, ".claude", "settings.json")},
			agent.SettingsFile{Label: "project · just you", Path: filepath.Join(root, ".claude", "settings.local.json")})
	}
	return files
}

// StatusLine is the command in p's settings.json.
func (Adapter) StatusLine(p agent.Profile) (string, error) {
	s, err := settingsfile.Load(filepath.Join(p.Dir, "settings.json"))
	if err != nil {
		return "", err
	}
	return s.String("statusLine.command"), nil
}

// SetStatusLine writes command into p's settings.json, flush to the left.
func (Adapter) SetStatusLine(p agent.Profile, command string) error {
	s, err := settingsfile.Load(filepath.Join(p.Dir, "settings.json"))
	if err != nil {
		return err
	}
	var v any
	if command != "" {
		v = map[string]any{"type": "command", "command": command, "padding": 0}
	}
	if err := s.Set("statusLine", v); err != nil {
		return err
	}
	return s.Save()
}
