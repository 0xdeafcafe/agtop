package claude

import (
	"os"
	"path/filepath"

	"github.com/0xdeafcafe/rush/internal/actions"
	"github.com/0xdeafcafe/rush/internal/adapters/claude/claude"
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

// StatusLineProfile is the folder Claude Code ran the status line for:
// CLAUDE_CONFIG_DIR's, else ~/.claude.
func (Adapter) StatusLineProfile() agent.Profile {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return agent.Profile{Kind: Kind, Dir: dir}
	}
	return claude.DefaultAccount().Profile()
}

var _ agent.StatusLineProfiler = Adapter{}

// Compaction is where a session on p in cwd compacts, from its process's
// env and the settings files it reads: yours, then the project's and its
// just-for-you one.
func (Adapter) Compaction(p agent.Profile, cwd string, env []string) agent.Compaction {
	src := claude.CompactSource{Env: env, State: claude.ReadSettingsCached(claudeJSON(p.Dir))}
	paths := []string{filepath.Join(p.Dir, "settings.json")}
	if root := projectRoot(cwd); root != "" {
		paths = append(paths, filepath.Join(root, ".claude", "settings.json"), filepath.Join(root, ".claude", "settings.local.json"))
	}
	for _, path := range paths {
		src.Settings = append(src.Settings, claude.ReadSettingsCached(path))
	}
	return claude.Compaction(src)
}

// projectRoot is the checkout cwd is in, found by its .git without
// running git (the list asks for every row); else cwd itself.
func projectRoot(cwd string) string {
	if cwd == "" {
		return ""
	}
	for dir := cwd; ; {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		up := filepath.Dir(dir)
		if up == dir {
			return cwd
		}
		dir = up
	}
}

var _ agent.Compacter = Adapter{}
