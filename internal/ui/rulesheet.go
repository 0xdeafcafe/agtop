package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/actions"
	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/fleet"
)

// --- /permissions and /hooks ---

// settingsFile is one of the settings files a session reads.
type settingsFile struct {
	label, path string
}

// settingsFiles are yours, then the project's (shared through git) and
// the project's just-for-you one, where Claude Code saves "don't ask again".
func settingsFiles(acct claude.Account, cwd string) []settingsFile {
	files := []settingsFile{{"yours", filepath.Join(acct.ConfigDir, "settings.json")}}
	root := firstNonEmpty(actions.RepoRoot(cwd), cwd)
	if root != "" {
		files = append(files,
			settingsFile{"project", filepath.Join(root, ".claude", "settings.json")},
			settingsFile{"project · just you", filepath.Join(root, ".claude", "settings.local.json")})
	}
	return files
}

var ruleKinds = []string{"allow", "ask", "deny"}

// permRule is one permission rule and the file it's in.
type permRule struct {
	text string
	file settingsFile
}

// permSheet is /permissions: the allow, ask and deny rules from every
// settings file, each marked with its file; add one to any file, or take
// one out.
type permSheet struct {
	files  []settingsFile
	rules  [3][]permRule
	mode   string
	tab    int
	cur    [3]int
	adding bool
	input  []rune
	pos    int
	target int // which file a new rule goes to
	armed  string
	err    string
}

func (m *Model) openPermissions(c *hostConn, a *fleet.Agent) {
	p := &permSheet{files: settingsFiles(a.Acct, firstNonEmpty(c.sess.Info.Cwd, a.Cwd))}
	p.target = len(p.files) - 1
	p.load()
	m.sheet = p
}

func (p *permSheet) load() {
	p.rules = [3][]permRule{}
	p.mode = ""
	for _, f := range p.files {
		s, err := claude.LoadSettingsFile(f.path)
		if err != nil {
			p.err = err.Error()
			continue
		}
		for i, kind := range ruleKinds {
			var list []string
			s.Get("permissions."+kind, &list)
			for _, r := range list {
				p.rules[i] = append(p.rules[i], permRule{r, f})
			}
		}
		if m := s.String("permissions.defaultMode"); m != "" {
			p.mode = m + " (" + f.label + ")"
		}
	}
}

// change edits one file's list for the tab's kind and reads everything
// again.
func (p *permSheet) change(f settingsFile, edit func([]string) []string) {
	s, err := claude.LoadSettingsFile(f.path)
	if err == nil {
		key := "permissions." + ruleKinds[p.tab]
		var list []string
		s.Get(key, &list)
		list = edit(list)
		var v any = list
		if len(list) == 0 {
			v = nil
		}
		if err = s.Set(key, v); err == nil {
			err = s.Save()
		}
	}
	if err != nil {
		p.err = "couldn't save " + tildify(f.path) + ": " + err.Error()
	}
	p.load()
}

func (p *permSheet) key(m *Model, k tea.KeyPressMsg, s string) tea.Cmd {
	if p.adding {
		switch s {
		case "esc":
			p.adding = false
		case "tab", "shift+tab":
			d := 1
			if s == "shift+tab" {
				d = len(p.files) - 1
			}
			p.target = (p.target + d) % len(p.files)
		case "enter":
			text := strings.TrimSpace(string(p.input))
			p.adding, p.input, p.pos = false, nil, 0
			if text != "" {
				p.change(p.files[p.target], func(l []string) []string {
					if slices.Contains(l, text) {
						return l
					}
					return append(l, text)
				})
			}
		default:
			p.input, p.pos, _ = edit(p.input, p.pos, k, s)
		}
		return nil
	}
	list := p.rules[p.tab]
	cur := &p.cur[p.tab]
	*cur = max(0, min(*cur, len(list)-1))
	switch s {
	case "esc", "ctrl+c", "q":
		m.sheet = nil
	case "tab", "right":
		p.tab, p.armed = (p.tab+1)%3, ""
	case "shift+tab", "left":
		p.tab, p.armed = (p.tab+2)%3, ""
	case "up", "k":
		*cur = roundMove(*cur, -1, len(list))
		p.armed = ""
	case "down", "j":
		*cur = roundMove(*cur, 1, len(list))
		p.armed = ""
	case "a", "+", "enter":
		p.adding, p.err = true, ""
	case "x", "delete", "backspace":
		if len(list) == 0 {
			return nil
		}
		r := list[*cur]
		id := r.file.path + "\x00" + r.text
		if p.armed != id {
			p.armed = id
			return nil
		}
		p.armed = ""
		p.change(r.file, func(l []string) []string { return slices.DeleteFunc(l, func(x string) bool { return x == r.text }) })
	case "ctrl+e", "e":
		if len(list) > 0 {
			return editFile(list[*cur].file.path)
		}
		return editFile(p.files[0].path)
	}
	return nil
}

func (p *permSheet) body(m *Model, w, h int) []string {
	about := "what Claude may do without asking, must ask about, and may never do"
	out := []string{sheetTitle("Permissions", about, w), ""}
	out = append(out, sheetTabs([]string{
		fmt.Sprintf("Allow %d", len(p.rules[0])), fmt.Sprintf("Ask %d", len(p.rules[1])), fmt.Sprintf("Deny %d", len(p.rules[2])),
	}, p.tab))
	if p.mode != "" {
		out = append(out, dim("  default mode: "+p.mode))
	}
	out = append(out, "")
	list := p.rules[p.tab]
	cur := max(0, min(p.cur[p.tab], len(list)-1))
	listH := max(3, h-len(out)-6)
	var rows []string
	for i, r := range list {
		line := paint(cText, fit(r.text, max(20, w-28))) + "  " + dim(r.file.label)
		if p.armed == r.file.path+"\x00"+r.text {
			line = paint(cRed, "x again takes out "+ansi.Truncate(r.text, w-30, "…"))
		}
		rows = append(rows, sheetRow(line, i == cur, w))
	}
	if len(rows) == 0 {
		rows = append(rows, dim("  none · a adds one, like Bash(npm test:*), Read(./secrets/**) or WebFetch(domain:example.com)"))
	}
	from, to := window(len(rows), cur, listH)
	out = append(out, rows[from:to]...)
	out = append(out, "")
	switch {
	case p.adding:
		out = append(out, "  "+paint(cOrange, ruleKinds[p.tab]+" ❯ ")+textField(p.input, p.pos, true, "Bash(git status:*)", w-40)+"  "+dim("into ")+paint(cText, p.files[p.target].label)+dim(" · tab"))
	case p.err != "":
		out = append(out, "  "+paint(cRed, ansi.Truncate(p.err, w-4, "…")))
	case len(list) > 0:
		out = append(out, "  "+dim(tildify(list[cur].file.path)))
	}
	return append(out, "", keysFit(w, "a", "add", "x", "take out", "←→", "allow/ask/deny", "e", "edit the file", "esc", "close"))
}

// hook is one command a hook event runs.
type hook struct {
	event, matcher, command string
	file                    settingsFile
}

// hookSheet is /hooks: every hook by event, from each settings file and
// plugin, with the file it's in; enter opens that file to change it.
type hookSheet struct {
	hooks []hook
	off   bool // disableAllHooks
	user  string
	cur   int
}

func (m *Model) openHooks(c *hostConn, a *fleet.Agent) tea.Cmd {
	cwd := firstNonEmpty(c.sess.Info.Cwd, a.Cwd)
	hs := &hookSheet{user: filepath.Join(a.Acct.ConfigDir, "settings.json")}
	for _, f := range settingsFiles(a.Acct, cwd) {
		s, err := claude.LoadSettingsFile(f.path)
		if err != nil {
			continue
		}
		var off bool
		if s.Get("disableAllHooks", &off) && off {
			hs.off = true
		}
		var raw json.RawMessage
		if s.Get("hooks", &raw) {
			hs.hooks = append(hs.hooks, parseHooks(raw, f)...)
		}
	}
	hs.sort()
	m.sheet = hs
	// Enabled plugins' hooks come after, read-only.
	acct := a.Acct
	return sheetDo(func() ([]hook, error) {
		inst, _, err := claude.Plugins(acct, cwd)
		var out []hook
		for _, pl := range inst {
			if !pl.Enabled {
				continue
			}
			path := filepath.Join(pl.InstallPath, "hooks", "hooks.json")
			if b, err := os.ReadFile(path); err == nil {
				var w struct {
					Hooks json.RawMessage `json:"hooks"`
				}
				if json.Unmarshal(b, &w) == nil {
					out = append(out, parseHooks(w.Hooks, settingsFile{"plugin " + pl.Name, path})...)
				}
			}
		}
		return out, err
	}, func(m *Model, more []hook, err error) tea.Cmd {
		hs.hooks = append(hs.hooks, more...)
		hs.sort()
		return nil
	})
}

func (hs *hookSheet) sort() {
	sort.SliceStable(hs.hooks, func(i, j int) bool { return hs.hooks[i].event < hs.hooks[j].event })
}

func parseHooks(raw json.RawMessage, f settingsFile) []hook {
	var byEvent map[string][]struct {
		Matcher string `json:"matcher"`
		Hooks   []struct {
			Type    string `json:"type"`
			Command string `json:"command"`
			Prompt  string `json:"prompt"`
		} `json:"hooks"`
	}
	if json.Unmarshal(raw, &byEvent) != nil {
		return nil
	}
	var out []hook
	for ev, groups := range byEvent {
		for _, g := range groups {
			for _, h := range g.Hooks {
				out = append(out, hook{ev, g.Matcher, firstNonEmpty(h.Command, h.Prompt, h.Type), f})
			}
		}
	}
	return out
}

func (hs *hookSheet) key(m *Model, k tea.KeyPressMsg, s string) tea.Cmd {
	switch s {
	case "esc", "ctrl+c", "q":
		m.sheet = nil
	case "up", "k":
		hs.cur = roundMove(hs.cur, -1, len(hs.hooks))
	case "down", "j":
		hs.cur = roundMove(hs.cur, 1, len(hs.hooks))
	case "enter", "e", "ctrl+e":
		path := hs.user
		if len(hs.hooks) > 0 {
			h := hs.hooks[hs.cur]
			if strings.HasPrefix(h.file.label, "plugin ") {
				m.flash("that hook is "+h.file.label+"'s: /plugins turns the plugin off", true)
				return nil
			}
			path = h.file.path
		}
		m.sheet = nil
		return editFile(path)
	}
	return nil
}

func (hs *hookSheet) body(m *Model, w, h int) []string {
	out := []string{sheetTitle("Hooks", "commands Claude Code runs around tools, prompts and sessions", w), ""}
	if hs.off {
		out = append(out, paint(cYellow, "  All hooks are off (disableAllHooks): Settings › Claude turns them back on."), "")
	}
	listH := max(3, h-len(out)-6)
	var rows []string
	selAt, last := 0, ""
	for i, hk := range hs.hooks {
		if hk.event != last {
			rows = append(rows, paint(cSub+bold, "  "+hk.event))
			last = hk.event
		}
		if i == hs.cur {
			selAt = len(rows)
		}
		match := hk.matcher
		if match == "" {
			match = "*"
		}
		line := paint(cBlue, fit(match, 14)) + " " + paint(cText, fit(oneLine(hk.command), max(20, w-44))) + "  " + dim(hk.file.label)
		rows = append(rows, sheetRow(line, i == hs.cur, w))
	}
	if len(rows) == 0 {
		rows = append(rows, dim("  no hooks · enter opens your settings.json to add one"))
	}
	from, to := window(len(rows), selAt, listH)
	out = append(out, rows[from:to]...)
	out = append(out, "")
	if len(hs.hooks) > 0 {
		out = append(out, "  "+dim(ansi.Truncate(tildify(hs.hooks[hs.cur].file.path), w-4, "…")))
	}
	return append(out, "", keysFit(w, "↑↓", "choose", "enter", "edit its file", "esc", "close"))
}
