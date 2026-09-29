package agent

// Plugin is an installed plugin, or one a marketplace offers. Its JSON is
// Claude Code's plugin list's, the first agent that had them.
type Plugin struct {
	ID          string `json:"id"` // name@marketplace
	Name        string `json:"name"`
	Marketplace string `json:"marketplaceName"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Installs    int    `json:"installCount"`

	// Installed ones only.
	Installed   bool   `json:"-"`
	Enabled     bool   `json:"enabled"`
	Scope       string `json:"scope"` // user, project, local
	ProjectPath string `json:"projectPath"`
	InstallPath string `json:"installPath"`
	LastUpdated string `json:"lastUpdated"`

	Parts PluginParts `json:"-"`
}

// PluginParts is what a plugin brings, read from its folder.
type PluginParts struct {
	Skills, Agents, Commands, Hooks, MCP []string
}

// Marketplace is a catalogue plugins are installed from.
type Marketplace struct {
	Name     string `json:"name"`
	Source   string `json:"source"`
	Repo     string `json:"repo"`
	URL      string `json:"url"`
	Location string `json:"installLocation"`
}

// Where is how a marketplace is found: its GitHub repo, URL or folder.
func (m Marketplace) Where() string {
	switch {
	case m.Repo != "":
		return m.Repo
	case m.URL != "":
		return m.URL
	}
	return m.Location
}

// PluginOp is a change to an agent's plugins or marketplaces.
type PluginOp int

const (
	PluginEnable PluginOp = iota
	PluginDisable
	PluginUpdate
	PluginRemove
	PluginInstall
	MarketAdd    // ID is where it is: a repo, URL or folder
	MarketUpdate // an empty ID updates every one
	MarketRemove
)

// PluginChange is one change: to the plugin or marketplace ID, in scope
// (user, project, local) where that matters.
type PluginChange struct {
	Op        PluginOp
	ID, Scope string
}

// Plugger is an agent with plugins, from marketplaces: FeaturePlugins.
// dir is the folder whose project plugins count.
type Plugger interface {
	Plugins(p Profile, dir string) (installed, available []Plugin, err error)
	Marketplaces(p Profile, dir string) ([]Marketplace, error)
	// PluginCost is roughly what a plugin adds to every session: "~1.2k tok".
	PluginCost(p Profile, dir, id string) (string, error)
	ChangePlugins(p Profile, dir string, c PluginChange) error
}
