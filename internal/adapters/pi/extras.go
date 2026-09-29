package pi

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/0xdeafcafe/rush/internal/actions"
	"github.com/0xdeafcafe/rush/internal/adapters/claude/claude"
	"github.com/0xdeafcafe/rush/internal/agent"
)

// Commands are the prompt templates (/name) and skills (/skill:name) a
// session in cwd has: the project's .pi first, then p's own, then the
// .agents skills Pi shares with other agents. Packages' and settings'
// extra folders aren't read.
func (Adapter) Commands(p agent.Profile, cwd string) []agent.Command {
	home, _ := os.UserHomeDir()
	seen := map[string]bool{}
	var out []agent.Command
	add := func(cs []agent.Command, source string) {
		for _, c := range cs {
			if c.Name != "" && !seen[c.Name] {
				seen[c.Name], c.Source = true, source
				out = append(out, c)
			}
		}
	}
	for _, d := range []struct{ dir, source string }{
		{filepath.Join(cwd, ".pi"), "project"}, {p.Dir, "yours"},
	} {
		if cwd == "" && d.source == "project" {
			continue
		}
		add(claude.ReadCommands(filepath.Join(d.dir, "prompts"), ""), d.source)
		add(claude.ReadSkills(filepath.Join(d.dir, "skills"), "skill:"), d.source)
	}
	if cwd != "" {
		add(claude.ReadSkills(filepath.Join(cwd, ".agents", "skills"), "skill:"), "project")
	}
	if home != "" {
		add(claude.ReadSkills(filepath.Join(home, ".agents", "skills"), "skill:"), "yours")
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// SettingsFiles are p's settings.json, then the project's .pi one, which
// Pi lays over it.
func (Adapter) SettingsFiles(p agent.Profile, cwd string) []agent.SettingsFile {
	files := []agent.SettingsFile{{Label: "yours", Path: filepath.Join(p.Dir, "settings.json")}}
	if cwd != "" {
		files = append(files, agent.SettingsFile{Label: "project", Path: filepath.Join(cwd, ".pi", "settings.json")})
	}
	return files
}

// Memory is the AGENTS.md files a session in cwd reads: p's own, then, from
// the root down to cwd, each folder's AGENTS.md, or its CLAUDE.md when it
// has none. p's, and the project's AGENTS.md when it has neither, are
// listed even before they exist, so they can be written.
func (Adapter) Memory(p agent.Profile, cwd, _ string) agent.Memory {
	var files []agent.MemoryFile
	add := func(path, name string, always bool) bool {
		st, err := os.Stat(path)
		if (err != nil || st.IsDir()) && !always {
			return false
		}
		f := agent.MemoryFile{Group: "Instructions", Path: path, Name: name, Missing: err != nil}
		if err == nil {
			f.Size, f.Mod, f.Up = st.Size(), st.ModTime(), claude.EstTokens(st.Size())
		}
		files = append(files, f)
		return true
	}
	add(filepath.Join(p.Dir, "AGENTS.md"), "yours · AGENTS.md", true)
	root := actions.RepoRoot(cwd)
	var dirs []string
	for d := cwd; d != "" && d != "/" && d != "."; d = filepath.Dir(d) {
		dirs = append([]string{d}, dirs...)
	}
	for _, d := range dirs {
		rel, _ := filepath.Rel(cwd, d)
		if !add(filepath.Join(d, "AGENTS.md"), filepath.Join(rel, "AGENTS.md"), false) &&
			!add(filepath.Join(d, "CLAUDE.md"), filepath.Join(rel, "CLAUDE.md"), false) && d == firstNonEmpty(root, cwd) {
			add(filepath.Join(d, "AGENTS.md"), filepath.Join(rel, "AGENTS.md"), true)
		}
	}
	return agent.Memory{Files: files, Groups: []string{"Instructions"}, When: map[string]string{"Instructions": "every session"}}
}

// ForgetFile deletes one of its files.
func (Adapter) ForgetFile(path string) error { return os.Remove(path) }

var (
	_ agent.Commander     = Adapter{}
	_ agent.SettingsFiler = Adapter{}
	_ agent.MemoryReader  = Adapter{}
)
