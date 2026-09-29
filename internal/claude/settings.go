package claude

import (
	"path/filepath"

	"github.com/0xdeafcafe/rush/internal/settingsfile"
)

// Settings is a Claude Code settings file: an account's settings.json, or
// a project's .claude/settings.json or settings.local.json.
type Settings = settingsfile.File

// LoadSettings reads acct's settings.json; a missing file is an empty one.
func LoadSettings(acct Account) (*Settings, error) {
	return settingsfile.Load(filepath.Join(acct.ConfigDir, "settings.json"))
}

// LoadSettingsFile reads any Claude Code settings file. Missing is empty.
func LoadSettingsFile(path string) (*Settings, error) { return settingsfile.Load(path) }
