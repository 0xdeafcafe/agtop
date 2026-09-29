package agent

// SettingsFile is one of the settings files an agent's session reads, and
// what it is to you: "yours", "project".
type SettingsFile struct {
	Label, Path string
}

// SettingsFiler is an agent whose settings are JSON files rush edits in
// place (settingsfile.File), and they're read in order: a profile's own,
// then a project's.
type SettingsFiler interface {
	SettingsFiles(p Profile, cwd string) []SettingsFile
}

// StatusLiner is an agent that runs a command of yours to draw a line
// under its prompt.
type StatusLiner interface {
	// StatusLine is p's command now; "" is none.
	StatusLine(p Profile) (string, error)
	// SetStatusLine makes command p's; "" takes it away.
	SetStatusLine(p Profile, command string) error
}
