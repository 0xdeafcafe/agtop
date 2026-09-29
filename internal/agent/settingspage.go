package agent

// SettingsPage is what an agent offers on its page in agtop's Settings:
// keys of the first of its SettingsFiles, each a row agtop edits in place,
// and the environment its sessions start with.
type SettingsPage struct {
	// Note is under the settings file's section, after its path.
	Note string
	// Unset is what a key not set means: "Claude Code's own default".
	Unset string
	Rows  []SettingRow
	// Env are the variables offered as choices; anything else in EnvKey
	// shows as a row of its own. Empty EnvKey: the agent has no env block.
	EnvKey string
	Env    []EnvSetting
}

// SettingRow is one key of a settings file.
type SettingRow struct {
	Label, Key, What string
	Type             SettingType
	Choices          []string // besides unset
}

// SettingType is how a row's value is kept.
type SettingType int

const (
	SettingString SettingType = iota
	SettingFlag               // true is "on"
	SettingOff                // true is "off": a key that turns a thing off
	SettingInt
)

// EnvSetting is a variable offered with choices, and what they mean.
type EnvSetting struct {
	Label, Name, What string
	Choices           []string // besides unset
	Means             map[string]string
}

// SettingsPager is an agent with a SettingsPage.
type SettingsPager interface {
	SettingsPage() SettingsPage
}

// AgentDef is a definition an agent's sessions can start as.
type AgentDef struct {
	Name, Scope, Model, Desc string
	// Path is its file; "" is the agent's built-in one.
	Path string
}

// Definer is an agent whose sessions can start as a definition of yours.
type Definer interface {
	// AgentDefs are p's definitions, the built-in one first, then those of
	// every project under roots.
	AgentDefs(p Profile, roots []string) []AgentDef
	// NewAgentDef makes a definition of p's called name, from a template
	// if there isn't one, and says where it is.
	NewAgentDef(p Profile, name string) (string, error)
}
