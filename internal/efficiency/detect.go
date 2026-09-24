package efficiency

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/0xdeafcafe/agtop/internal/claude"
)

// Status is how far a saver is set up.
type Status int

const (
	Off     Status = iota
	Partial        // found, but not doing anything yet: rtk's binary without its hook
	On
)

// Found is what detection found of a saver.
type Found struct {
	Status Status
	Parts  []string  // what was found, in words: "hook in settings.json"
	Wants  string    // what's missing when Partial
	Since  time.Time // when it was installed, when that's known
	Value  string    // a setting's value
}

// Env is an account's setup as detection needs it, read once per look.
type Env struct {
	Acct     claude.Account
	Settings *claude.Settings
	hooks    []string // every hook command in settings.json
	enabled  map[string]bool
	plugins  map[string]time.Time // installed plugin → installed at
	mcp      map[string]bool
}

// LoadEnv reads an account's settings, plugins and MCP servers.
func LoadEnv(a claude.Account) *Env {
	e := &Env{Acct: a, enabled: map[string]bool{}, plugins: map[string]time.Time{}, mcp: map[string]bool{}}
	e.Settings, _ = claude.LoadSettings(a)
	if e.Settings == nil {
		e.Settings, _ = claude.LoadSettingsFile(os.DevNull)
	}
	var hooks map[string][]struct {
		Hooks []struct {
			Command string `json:"command"`
		} `json:"hooks"`
	}
	e.Settings.Get("hooks", &hooks)
	for _, groups := range hooks {
		for _, g := range groups {
			for _, h := range g.Hooks {
				e.hooks = append(e.hooks, h.Command)
			}
		}
	}
	e.Settings.Get("enabledPlugins", &e.enabled)
	var inst struct {
		Plugins map[string][]struct {
			Scope       string    `json:"scope"`
			InstalledAt time.Time `json:"installedAt"`
		} `json:"plugins"`
	}
	if b, err := os.ReadFile(filepath.Join(a.ConfigDir, "plugins", "installed_plugins.json")); err == nil {
		_ = json.Unmarshal(b, &inst)
	}
	for id, list := range inst.Plugins {
		for _, p := range list {
			if p.Scope == "user" || e.plugins[id].IsZero() {
				e.plugins[id] = p.InstalledAt
			}
		}
	}
	var st struct {
		MCP map[string]json.RawMessage `json:"mcpServers"`
	}
	if b, err := os.ReadFile(a.StatePath()); err == nil {
		_ = json.Unmarshal(b, &st)
	}
	for name := range st.MCP {
		e.mcp[name] = true
	}
	return e
}

// searchPath is where programs are looked for: the PATH, and where
// package managers put them, which a GUI-started agtop's PATH may miss.
var searchPath = func() []string {
	home, _ := os.UserHomeDir()
	dirs := filepath.SplitList(os.Getenv("PATH"))
	return append(dirs, "/opt/homebrew/bin", "/usr/local/bin", filepath.Join(home, ".local", "bin"),
		filepath.Join(home, ".cargo", "bin"), filepath.Join(home, "go", "bin"))
}

// LookPath finds a program the way detection does.
func LookPath(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	for _, d := range searchPath() {
		p := filepath.Join(d, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

// Detect looks for a saver in this account.
func (e *Env) Detect(s *Saver) Found {
	if st := s.Setting; st != nil {
		return e.detectSetting(st)
	}
	var f Found
	d := s.Detect
	bin, hook, plugin, mcp := false, false, false, false
	for _, b := range d.Bins {
		if p := LookPath(b); p != "" {
			bin = true
			f.Parts = append(f.Parts, b+" installed")
			if t := brewTime(p); !t.IsZero() {
				f.Since = t
			}
		}
	}
	for _, h := range d.Hooks {
		for _, c := range e.hooks {
			if strings.Contains(c, h) && !hook {
				hook = true
				f.Parts = append(f.Parts, "hook in settings.json")
			}
		}
	}
	for _, id := range d.Plugins {
		if t, ok := e.plugins[id]; ok {
			if e.enabled[id] {
				plugin = true
				f.Parts = append(f.Parts, "plugin "+id+" on")
			} else {
				f.Parts = append(f.Parts, "plugin "+id+" off")
			}
			if !t.IsZero() {
				f.Since = t
			}
		}
	}
	for _, m := range d.MCP {
		for name := range e.mcp {
			if name == m || strings.Contains(name, m) {
				mcp = true
				f.Parts = append(f.Parts, "MCP server "+name)
			}
		}
		for id := range e.enabled {
			if strings.Contains(id, m) && e.enabled[id] && !plugin {
				mcp = true
				f.Parts = append(f.Parts, "plugin "+id)
			}
		}
	}
	file := false
	for _, p := range d.Files {
		if _, err := os.Stat(filepath.Join(e.Acct.ConfigDir, p)); err == nil {
			file = true
			f.Parts = append(f.Parts, p)
		}
	}
	any := bin || hook || plugin || mcp || file
	switch {
	case !any:
		f.Status = Off
	case d.Need == "hook" && !hook:
		f.Status, f.Wants = Partial, "its hook isn't in settings.json"
	case d.Need == "mcp" && !mcp:
		f.Status, f.Wants = Partial, "it isn't added as an MCP server"
	case len(d.Plugins) > 0 && !plugin && !hook:
		f.Status, f.Wants = Partial, "the plugin is off"
	default:
		f.Status = On
	}
	return f
}

func (e *Env) detectSetting(st *Setting) Found {
	var f Found
	var raw json.RawMessage
	ok := false
	if st.Env {
		v, set := e.Settings.Env()[st.Key]
		ok, f.Value = set, v
	} else if e.Settings.Get(st.Key, &raw) {
		ok, f.Value = true, strings.Trim(string(raw), `"`)
	}
	if !ok {
		return f
	}
	f.Status = On
	f.Parts = []string{fmt.Sprintf("%s = %s", st.Key, f.Value)}
	return f
}

// brewTime is when Homebrew installed the program at p, from its receipt.
func brewTime(p string) time.Time {
	real, err := filepath.EvalSymlinks(p)
	if err != nil || !strings.Contains(real, "/Cellar/") {
		return time.Time{}
	}
	// …/Cellar/<name>/<version>/bin/<name>
	dir := filepath.Dir(filepath.Dir(real))
	b, err := os.ReadFile(filepath.Join(dir, "INSTALL_RECEIPT.json"))
	if err != nil {
		return time.Time{}
	}
	var r struct {
		Time int64 `json:"time"`
	}
	if json.Unmarshal(b, &r) != nil || r.Time == 0 {
		return time.Time{}
	}
	return time.Unix(r.Time, 0)
}

// Pick is the recipe to use: the first whose program is found.
func Pick(rs []Recipe) (*Recipe, string) {
	var missing []string
	for i := range rs {
		if rs[i].Needs == "" || LookPath(rs[i].Needs) != "" {
			return &rs[i], ""
		}
		missing = append(missing, rs[i].Needs)
	}
	if len(missing) == 0 {
		return nil, "nothing to run"
	}
	return nil, "needs " + strings.Join(missing, " or ")
}

// Plan is the commands a recipe runs here, steps already done left out.
func (r *Recipe) Plan() [][]string {
	var out [][]string
	for _, s := range r.Steps {
		if s.Skip != "" && LookPath(s.Skip) != "" {
			continue
		}
		out = append(out, s.Argv)
	}
	return out
}
