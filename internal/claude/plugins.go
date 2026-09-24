package claude

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Plugin is an installed plugin, or one a marketplace offers.
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

// PluginCLI runs `claude plugin …` for the account in dir.
func PluginCLI(a Account, dir string, args ...string) ([]byte, error) {
	c := exec.Command("claude", append([]string{"plugin"}, args...)...)
	c.Env, c.Dir = a.Env(), dir
	var out, errb bytes.Buffer
	c.Stdout, c.Stderr = &out, &errb
	if err := c.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = strings.TrimSpace(out.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return out.Bytes(), fmt.Errorf("claude plugin %s: %s", args[0], lastLine(msg))
	}
	return out.Bytes(), nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// Plugins lists the installed plugins (with what each brings) and those the
// account's marketplaces offer, most installed first.
func Plugins(a Account, dir string) (installed, available []Plugin, err error) {
	out, err := PluginCLI(a, dir, "list", "--available", "--json")
	if err != nil {
		return nil, nil, err
	}
	var raw struct {
		Installed []Plugin `json:"installed"`
		Available []struct {
			Plugin
			PluginID string `json:"pluginId"`
		} `json:"available"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, nil, fmt.Errorf("claude plugin list: %w", err)
	}
	for _, p := range raw.Installed {
		p.Installed = true
		p.Name, p.Marketplace, _ = strings.Cut(p.ID, "@")
		if p.Name == "" {
			p.Name = p.ID
		}
		readManifest(&p)
		p.Parts = pluginParts(p.InstallPath)
		installed = append(installed, p)
	}
	sort.SliceStable(installed, func(i, j int) bool { return installed[i].ID < installed[j].ID })
	for _, p := range raw.Available {
		p.Plugin.ID = p.PluginID
		available = append(available, p.Plugin)
	}
	sort.SliceStable(available, func(i, j int) bool { return available[i].Installs > available[j].Installs })
	return installed, available, nil
}

// Marketplaces lists the account's plugin marketplaces.
func Marketplaces(a Account, dir string) ([]Marketplace, error) {
	out, err := PluginCLI(a, dir, "marketplace", "list", "--json")
	if err != nil {
		return nil, err
	}
	var ms []Marketplace
	if err := json.Unmarshal(out, &ms); err != nil {
		return nil, fmt.Errorf("claude plugin marketplace list: %w", err)
	}
	return ms, nil
}

var alwaysOn = regexp.MustCompile(`Always-on:\s*(~?[\d.,]+k?\s*tok)`)

// PluginCost is roughly how many tokens a plugin adds to every session,
// from `claude plugin details`.
func PluginCost(a Account, dir, id string) (string, error) {
	out, err := PluginCLI(a, dir, "details", id)
	if err != nil {
		return "", err
	}
	if m := alwaysOn.FindSubmatch(out); m != nil {
		return string(m[1]), nil
	}
	return "", nil
}

func readManifest(p *Plugin) {
	b, err := os.ReadFile(filepath.Join(p.InstallPath, ".claude-plugin", "plugin.json"))
	if err != nil {
		return
	}
	var m struct {
		Description string `json:"description"`
		Version     string `json:"version"`
	}
	if json.Unmarshal(b, &m) == nil {
		p.Description = firstOf(p.Description, m.Description)
		p.Version = firstOf(p.Version, m.Version)
	}
}

// pluginParts reads what a plugin folder holds: skills/<name>/SKILL.md,
// agents/*.md, commands/*.md, hooks/hooks.json's events and .mcp.json's
// servers.
func pluginParts(dir string) PluginParts {
	var p PluginParts
	if dir == "" {
		return p
	}
	skills, _ := filepath.Glob(filepath.Join(dir, "skills", "*", "SKILL.md"))
	for _, s := range skills {
		p.Skills = append(p.Skills, filepath.Base(filepath.Dir(s)))
	}
	names := func(glob string) (out []string) {
		fs, _ := filepath.Glob(filepath.Join(dir, glob))
		for _, f := range fs {
			out = append(out, strings.TrimSuffix(filepath.Base(f), ".md"))
		}
		return out
	}
	p.Agents, p.Commands = names("agents/*.md"), names("commands/*.md")
	if b, err := os.ReadFile(filepath.Join(dir, "hooks", "hooks.json")); err == nil {
		var h struct {
			Hooks map[string]json.RawMessage `json:"hooks"`
		}
		if json.Unmarshal(b, &h) == nil {
			for ev := range h.Hooks {
				p.Hooks = append(p.Hooks, ev)
			}
			sort.Strings(p.Hooks)
		}
	}
	if b, err := os.ReadFile(filepath.Join(dir, ".mcp.json")); err == nil {
		var m struct {
			Servers map[string]json.RawMessage `json:"mcpServers"`
		}
		if json.Unmarshal(b, &m) == nil {
			for s := range m.Servers {
				p.MCP = append(p.MCP, s)
			}
			sort.Strings(p.MCP)
		}
	}
	return p
}
