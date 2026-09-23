package claude

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Command is a slash command or skill a session can run.
type Command struct {
	Name         string
	Description  string
	ArgumentHint string
	Skill        bool
}

var (
	cmdMu    sync.Mutex
	cmdCache = map[string]cmdEntry{}
)

type cmdEntry struct {
	at   time.Time
	cmds []Command
}

// Commands lists the custom commands and skills a session in cwd can use:
// the account's own, the project's (cwd up to the filesystem root), and
// those of installed and synced plugins, named plugin:name. It is read at
// most every 30 seconds.
func Commands(configDir, cwd string) []Command {
	key := configDir + "\x00" + cwd
	cmdMu.Lock()
	defer cmdMu.Unlock()
	if e, ok := cmdCache[key]; ok && time.Since(e.at) < 30*time.Second {
		return e.cmds
	}
	seen := map[string]bool{}
	var out []Command
	add := func(c Command) {
		if c.Name != "" && !seen[c.Name] {
			seen[c.Name] = true
			out = append(out, c)
		}
	}
	scan := func(root, prefix string) {
		for _, c := range readSkills(filepath.Join(root, "skills"), prefix) {
			add(c)
		}
		for _, c := range readCommands(filepath.Join(root, "commands"), prefix) {
			add(c)
		}
	}
	for d := cwd; d != "" && d != "/" && d != "."; d = filepath.Dir(d) {
		scan(filepath.Join(d, ".claude"), "")
	}
	scan(configDir, "")
	synced, _ := filepath.Glob(filepath.Join(configDir, "skills", "synced", "*"))
	for _, d := range synced {
		for _, c := range readSkills(d, "") {
			add(c)
		}
	}
	for _, root := range pluginRoots(configDir, cwd) {
		scan(root, pluginName(root)+":")
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	cmdCache[key] = cmdEntry{at: time.Now(), cmds: out}
	return out
}

// pluginRoots are the folders of plugins installed for this account (user
// scope, or project scope for a project containing cwd) and synced ones.
func pluginRoots(configDir, cwd string) []string {
	var roots []string
	var inst struct {
		Plugins map[string][]struct {
			Scope       string `json:"scope"`
			ProjectPath string `json:"projectPath"`
			InstallPath string `json:"installPath"`
		} `json:"plugins"`
	}
	if b, err := os.ReadFile(filepath.Join(configDir, "plugins", "installed_plugins.json")); err == nil {
		_ = json.Unmarshal(b, &inst)
	}
	for _, list := range inst.Plugins {
		for _, p := range list {
			if p.Scope == "project" && p.ProjectPath != "" && !strings.HasPrefix(cwd+"/", p.ProjectPath+"/") {
				continue
			}
			roots = append(roots, p.InstallPath)
		}
	}
	synced, _ := filepath.Glob(filepath.Join(configDir, "plugins", "synced", "*", "*"))
	for _, d := range synced {
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			roots = append(roots, d)
		}
	}
	return roots
}

func pluginName(root string) string {
	var m struct {
		Name string `json:"name"`
	}
	if b, err := os.ReadFile(filepath.Join(root, ".claude-plugin", "plugin.json")); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	if m.Name != "" {
		return m.Name
	}
	return filepath.Base(root)
}

func readSkills(dir, prefix string) []Command {
	files, _ := filepath.Glob(filepath.Join(dir, "*", "SKILL.md"))
	var out []Command
	for _, f := range files {
		fm := frontmatter(f)
		name := firstOf(fm["name"], filepath.Base(filepath.Dir(f)))
		out = append(out, Command{Name: prefix + name, Description: fm["description"], ArgumentHint: fm["argument-hint"], Skill: true})
	}
	return out
}

// readCommands reads commands/*.md, where a subfolder becomes part of the
// name (commands/git/push.md is git:push).
func readCommands(dir, prefix string) []Command {
	var out []Command
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
			return nil
		}
		rel, _ := filepath.Rel(dir, strings.TrimSuffix(p, ".md"))
		fm := frontmatter(p)
		out = append(out, Command{Name: prefix + strings.ReplaceAll(rel, string(filepath.Separator), ":"),
			Description: firstOf(fm["description"], fm[""]), ArgumentHint: fm["argument-hint"]})
		return nil
	})
	return out
}

// frontmatter reads the simple key: value lines between --- fences at the
// top of a markdown file. Without one, "" holds the first line of text.
func frontmatter(path string) map[string]string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	in := false
	for n := 0; sc.Scan() && n < 60; n++ {
		line := sc.Text()
		if n == 0 && strings.TrimSpace(line) == "---" {
			in = true
			continue
		}
		if !in {
			if t := strings.TrimSpace(strings.TrimLeft(line, "# ")); t != "" {
				out[""] = t
				return out
			}
			continue
		}
		if strings.TrimSpace(line) == "---" {
			return out
		}
		if k, v, ok := strings.Cut(line, ":"); ok && !strings.HasPrefix(k, " ") {
			out[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return out
}

func firstOf(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}
