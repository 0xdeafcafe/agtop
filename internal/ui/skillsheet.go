package ui

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/fleet"
)

// --- /skills ---

// skillSheet lists the skills and custom commands a session can use,
// grouped by where they come from: search, see one whole, use it (it goes
// into the message box) or edit your own.
type skillSheet struct {
	conn     string
	all      []claude.Command
	tab      int // 0 skills, 1 commands
	cur      [2]int
	query    []rune
	queryPos int
}

func (m *Model) openSkills(c *hostConn, a *fleet.Agent) {
	cwd := firstNonEmpty(c.sess.Info.Cwd, a.Cwd)
	all := claude.Commands(firstNonEmpty(a.Acct.ConfigDir, claude.DefaultAccount().ConfigDir), cwd)
	m.sheet = &skillSheet{conn: c.key, all: all}
}

// sourceRank puts your own first, then the project's, claude.ai's, and
// the plugins'.
func sourceRank(s string) int {
	switch s {
	case "yours":
		return 0
	case "project":
		return 1
	case "claude.ai":
		return 2
	}
	return 3
}

// shown is the tab's list for the search, grouped by source.
func (k *skillSheet) shown() []claude.Command {
	q := strings.ToLower(strings.TrimSpace(string(k.query)))
	var out []claude.Command
	for _, c := range k.all {
		if c.Skill != (k.tab == 0) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(c.Name+" "+c.Description+" "+c.Source), q) {
			continue
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		ra, rb := sourceRank(a.Source), sourceRank(b.Source)
		if ra != rb {
			return ra < rb
		}
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		return a.Name < b.Name
	})
	return out
}

func (k *skillSheet) key(m *Model, kp tea.KeyPressMsg, s string) tea.Cmd {
	list := k.shown()
	cur := &k.cur[k.tab]
	*cur = max(0, min(*cur, len(list)-1))
	switch s {
	case "esc", "ctrl+c":
		m.sheet = nil
	case "tab", "shift+tab":
		k.tab = 1 - k.tab
	case "up":
		*cur = roundMove(*cur, -1, len(list))
	case "down":
		*cur = roundMove(*cur, 1, len(list))
	case "pgup":
		*cur = max(0, *cur-10)
	case "pgdown":
		*cur = max(0, min(len(list)-1, *cur+10))
	case "enter":
		if len(list) == 0 {
			return nil
		}
		c := list[*cur]
		m.sheet = nil
		if h := m.host; h != nil && h.key == k.conn {
			h.input, h.back = []rune("/"+c.Name+" "), 0
			m.paneFocus = true
		}
	case "ctrl+e", "ctrl+o":
		if len(list) == 0 || list[*cur].Path == "" {
			return nil
		}
		c := list[*cur]
		if !editable(c) {
			m.flash(c.Name+" comes from "+c.Source+": an update would overwrite changes to it", true)
			return nil
		}
		return editFile(c.Path)
	default:
		if q, pos, ok := edit(k.query, k.queryPos, kp, s); ok {
			k.query, k.queryPos, *cur = q, pos, 0
		}
	}
	return nil
}

func editable(c claude.Command) bool { return c.Source == "yours" || c.Source == "project" }

func (k *skillSheet) body(m *Model, w, h int) []string {
	skills, cmds := 0, 0
	for _, c := range k.all {
		if c.Skill {
			skills++
		} else {
			cmds++
		}
	}
	out := []string{
		sheetTitle("Skills", "what Claude picks up when a task calls for it, and your own / commands", w),
		"",
		sheetTabs([]string{fmt.Sprintf("Skills %d", skills), fmt.Sprintf("Commands %d", cmds)}, k.tab),
		"",
		"  " + paint(cOrange, "⌕ ") + textField(k.query, k.queryPos, true, "search", w-6),
		"",
	}
	list := k.shown()
	cur := max(0, min(k.cur[k.tab], len(list)-1))
	detail := k.detail(list, cur, w)
	listH := max(3, h-len(out)-len(detail)-4)
	var rows []string
	selAt, last := 0, ""
	for i, c := range list {
		if c.Source != last {
			rows = append(rows, paint(cSub+bold, "  "+sourceTitle(c.Source)))
			last = c.Source
		}
		if i == cur {
			selAt = len(rows)
		}
		name := c.Name
		if _, n, ok := strings.Cut(name, ":"); ok && c.Source != "yours" && c.Source != "project" && c.Source != "claude.ai" {
			name = n // the plugin's heading says whose it is
		}
		line := paint(cText+bold, fit(name, 26)) + " " + dim(fit(approxTokens(c.Size), 10)) + " " + faint(ansi.Truncate(oneLine(c.Description), max(0, w-44), "…"))
		rows = append(rows, sheetRow(line, i == cur, w))
	}
	if len(list) == 0 {
		rows = append(rows, dim("  nothing matches"))
	}
	from, to := window(len(rows), selAt, listH)
	out = append(out, rows[from:to]...)
	out = append(out, "", faint(strings.Repeat("─", w)))
	out = append(out, detail...)
	return append(out, "", keysFit(w, "type", "search", "enter", "use it", "ctrl+e", "edit", "tab", "skills/commands", "esc", "close"))
}

func (k *skillSheet) detail(list []claude.Command, cur int, w int) []string {
	if len(list) == 0 {
		return nil
	}
	c := list[cur]
	head := "  " + paint(cText+bold, "/"+c.Name)
	if c.ArgumentHint != "" {
		head += " " + dim(c.ArgumentHint)
	}
	out := []string{head}
	for i, l := range wrap(oneLine(firstNonEmpty(c.Description, "no description")), w-4) {
		if i == 3 {
			break
		}
		out = append(out, "  "+paint(cSub, l))
	}
	where := tildify(c.Path)
	if !editable(c) {
		where += " · " + c.Source + "'s, read-only"
	}
	return append(out, "  "+dim(ansi.Truncate(where+" · "+approxTokens(c.Size)+" when used", w-4, "…")))
}

func sourceTitle(s string) string {
	switch s {
	case "yours":
		return "Yours"
	case "project":
		return "This project's"
	case "claude.ai":
		return "From claude.ai"
	case "":
		return "Other"
	}
	return "Plugin · " + s
}

// approxTokens is a file's size as tokens, roughly (four bytes each).
func approxTokens(size int64) string {
	if size <= 0 {
		return ""
	}
	t := size / 4
	if t >= 1000 {
		return fmt.Sprintf("~%.1fk tok", float64(t)/1000)
	}
	return fmt.Sprintf("~%d tok", t)
}
