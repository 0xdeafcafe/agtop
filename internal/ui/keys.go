package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/actions"
	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/state"
)

func (m *Model) key(k tea.KeyPressMsg) tea.Cmd {
	s := k.String()
	m.hover = "" // the keyboard takes over from the mouse
	m.lastKeyAt = time.Now()
	if m.embedded {
		return m.embedKey(k)
	}
	if s == "ctrl+q" {
		m.scanner.Flush()
		return tea.Quit
	}
	if m.confirm != nil {
		return m.confirmKey(s)
	}
	if m.picker != nil {
		return m.pickerKey(s)
	}
	// In zen, while the next agent connects, keys wait rather than land in
	// some other box.
	if m.zen && m.host == nil && s != "tab" && s != "shift+tab" && s != "ctrl+q" && s != "ctrl+c" {
		return nil
	}
	if m.paneFocus && m.host != nil && m.mode == modeList && m.dialog == nil && s == "ctrl+c" {
		return m.paneKey(k, s)
	}
	if s == "ctrl+c" {
		if m.anchor > 0 && m.anchor-1 != m.cursorPos() {
			m.editInput(k, s) // copies the selection
			return nil
		}
		if len(m.input) > 0 {
			m.input, m.back, m.anchor = m.input[:0], 0, 0
			return nil
		}
		return m.quitKey()
	}
	if m.paneFocus && m.host != nil && m.mode == modeList && m.dialog == nil && s != "tab" {
		return m.paneKey(k, s)
	}
	if (s == "tab" || s == "shift+tab") && m.mode != modeCwd && (m.dialog == nil || m.dialog.asking == "") {
		if s == "tab" {
			m.setView(m.view + 1)
		} else {
			m.setView(m.view - 1)
		}
		return m.loadPreview()
	}
	if m.dialog != nil {
		return m.dialogKey(k, s)
	}
	switch m.mode {
	case modeHelp:
		m.mode = modeList
		return nil
	case modeProcs:
		return m.procKey(s)
	case modeCwd:
		return m.cwdKey(k, s)
	}
	return m.listKey(k, s)
}

func (m *Model) editKey(k tea.KeyPressMsg, s string) bool {
	switch s {
	case "backspace", "ctrl+h":
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
	case "ctrl+u":
		m.input = m.input[:0]
	case "ctrl+w", "alt+backspace":
		t := strings.TrimRight(string(m.input), " ")
		if i := strings.LastIndexByte(t, ' '); i >= 0 {
			m.input = []rune(t[:i+1])
		} else {
			m.input = m.input[:0]
		}
	default:
		if k.Text != "" && k.Mod&^tea.ModShift == 0 {
			m.input = append(m.input, []rune(k.Text)...)
			return true
		}
		return false
	}
	return true
}

func (m *Model) listKey(k tea.KeyPressMsg, s string) tea.Cmd {
	a := m.selected()
	empty := len(m.input) == 0
	if (s == "backspace" || s == "ctrl+h") && empty && len(m.images) > 0 {
		m.images = m.images[:len(m.images)-1]
		return nil
	}
	if s == "enter" && empty && len(m.images) > 0 && m.inKind == inPrompt {
		return m.startHosted("", m.startDir())
	}
	switch s {
	case "up":
		m.move(-1)
		return m.loadPreview()
	case "down":
		m.move(1)
		return m.loadPreview()
	case "pgup":
		m.move(-10)
		return m.loadPreview()
	case "pgdown", "ctrl+d":
		m.move(10)
		return m.loadPreview()
	case "home":
		if empty {
			m.move(-len(m.order))
			return m.loadPreview()
		}
	case "end":
		if empty {
			m.move(len(m.order))
			return m.loadPreview()
		}
	case "right":
		if empty && a != nil && !strings.HasPrefix(m.sel, "§") {
			return m.focusPane(a)
		}
		if empty {
			if t, ok := strings.CutPrefix(m.sel, "§"); ok {
				if m.folded(t) {
					m.toggleFold(t)
				}
				return nil
			}
			if m.canEmbed() {
				m.embedded = true
				return nil
			}
			// On a narrow screen the preview is already full width.
			if (m.preview || m.wide()) && m.w >= 120 {
				m.preview, m.full = true, true
			} else {
				m.preview = true
			}
			return m.loadPreview()
		}
	case "left":
		if empty {
			switch {
			case m.full:
				m.full = false
				if m.wide() {
					m.preview = false
				}
			case m.preview:
				m.preview = false
			case strings.HasPrefix(m.sel, "§"):
				if t := strings.TrimPrefix(m.sel, "§"); !m.folded(t) {
					m.toggleFold(t)
				}
			}
			// Otherwise ← has nothing to close; it never walks the
			// selection up onto a group title.
			return nil
		}
	case "esc":
		switch {
		case !empty:
			m.input = m.input[:0]
			if m.inKind != inReply {
				m.inKind = inPrompt
			}
		case m.inKind != inPrompt:
			m.inKind = inPrompt
		case m.preview:
			m.preview, m.full = false, false
		case m.armed != "":
			m.armed = ""
		case time.Since(m.quitArmed) < 2*time.Second:
			m.scanner.Flush()
			return tea.Quit
		default:
			m.quitArmed = time.Now()
			m.flash("esc again to quit", false)
		}
		return nil
	case "enter":
		if t, ok := strings.CutPrefix(m.sel, "§"); ok && empty {
			m.toggleFold(t)
			return nil
		}
		// Enter on an agent opens its Session with the keys in its message
		// box, whatever kind of session it is.
		if empty && a != nil && m.inKind == inPrompt {
			return m.focusPane(a)
		}
		return m.submit()
	case "f2":
		switch {
		case a == nil:
			m.flash("select an agent first", true)
		case !empty && m.inKind == inPrompt:
			m.flash("finish or clear the draft first (esc)", true)
		default:
			m.inKind, m.input, m.back, m.promptFor = inRename, []rune(a.DisplayName), 0, a.Key
		}
		return nil
	case "ctrl+l":
		// Drafting a new session, or nothing selected: choose where it
		// starts. Otherwise: move the selected agent.
		drafting := !empty && m.inKind == inPrompt && !strings.HasPrefix(string(m.input), "/")
		if drafting || a == nil {
			m.openDirPicker()
			return nil
		}
		m.openCwd(a)
		return nil
	case "ctrl+y":
		return m.openPR(a)
	case "ctrl+t":
		if a != nil {
			return m.togglePin(a)
		}
	case "ctrl+s":
		m.cycleGroupBy()
		return nil
	case "ctrl+o":
		// Reply: straight into the agent's Session message box.
		if a != nil {
			return m.focusPane(a)
		}
		return nil
	case "ctrl+x":
		return m.stopOrRemove(a)
	case "shift+up", "shift+down":
		n := m.dockLines()
		if s == "shift+up" {
			n++
		} else {
			n--
		}
		m.store.Config.DockLines = min(max(n, 1), 15)
		_ = m.store.SaveConfig()
		return nil
	case "[", "]":
		if empty && a != nil {
			if c := m.host; c != nil && c.key == a.Key {
				n, d := len(m.views(c)), 1
				if s == "[" {
					d = -1
				}
				c.view, c.scroll = (c.view%n+d+n)%n, 0
			} else {
				m.claudeView = 1 - m.claudeView
			}
			m.preview = true
			return nil
		}
	case "ctrl+f":
		// Open a Claude Code agent full screen (Claude Code's own view).
		if a != nil && !a.Agtop && !a.Interactive {
			return m.attach(a)
		}
		return nil
	case "ctrl+n":
		return m.nextNeedingYou()
	case "alt+left", "alt+right":
		if empty && m.listW > 0 {
			d := 2
			if s == "alt+left" {
				d = -2
			}
			m.setSideWidth(m.sideWidth() + d)
			return nil
		}
	case "?":
		if empty {
			m.mode = modeHelp
			return nil
		}
	}
	m.editInput(k, s)
	return nil
}

// nextNeedingYou selects the agent that has waited longest for you, so a
// stack of questions can be cleared with ctrl+n and an answer each.
func (m *Model) nextNeedingYou() tea.Cmd {
	var best *fleet.Agent
	for _, a := range m.order {
		if a.NeedsYou() && (best == nil || a.UpdatedAt.Before(best.UpdatedAt)) {
			best = a
		}
	}
	if best == nil {
		m.flash("nothing needs you", false)
		return nil
	}
	m.sel = best.Key
	m.rebuild()
	return m.loadPreview()
}

// quitKey arms quitting on the first ctrl+c and quits on a second one.
func (m *Model) quitKey() tea.Cmd {
	if time.Since(m.quitArmed) < 2*time.Second {
		m.scanner.Flush()
		return tea.Quit
	}
	m.quitArmed = time.Now()
	m.flash("ctrl+c again to quit", false)
	return nil
}

func (m *Model) sectionOf(key string) string {
	cur := ""
	for _, l := range m.lines {
		if l.kind == lineSection {
			cur = sectionKey(l.title)
		}
		if l.kind == lineAgent && l.agent.Key == key {
			return cur
		}
	}
	return key
}

func (m *Model) cycleGroupBy() {
	cur := m.store.Config.GroupBy
	next := groupModes[0]
	for i, g := range groupModes {
		if g == cur {
			next = groupModes[(i+1)%len(groupModes)]
		}
	}
	m.store.Config.GroupBy = next
	_ = m.store.SaveConfig()
	m.flash("grouped by "+next, false)
	m.rebuild()
}

func (m *Model) stopOrRemove(a *fleet.Agent) tea.Cmd {
	if a == nil {
		return nil
	}
	if a.Interactive {
		pid := a.PID
		m.confirm = &confirmation{
			question: "Close " + a.DisplayName + "?",
			detail:   fmt.Sprintf("sends SIGTERM to the terminal session (pid %d)", pid),
			onYes: func() tea.Cmd {
				return cmdErr("closed "+a.DisplayName, func() error { return actions.Terminate(pid) })
			},
		}
		return nil
	}
	if a.PID != 0 || (a.Live() && a.Worker != nil) {
		m.flash("stopping "+a.DisplayName+"…", false)
		return cmdErr("stopped "+a.DisplayName, func() error { return actions.Stop(a.Acct, a.ID, a.PID) })
	}
	if m.armed == a.Key && time.Since(m.armedAt) < 5*time.Second {
		m.armed = ""
		m.flash("deleting "+a.DisplayName+"…", false)
		return cmdErr("deleted "+a.DisplayName, func() error { return actions.Remove(a.Acct, a.ID) })
	}
	m.armed, m.armedAt = a.Key, time.Now()
	m.flash("ctrl+x again within 5s to delete "+a.DisplayName+" (and its worktree, when that's safe)", false)
	return nil
}

func (m *Model) submit() tea.Cmd {
	text := strings.TrimSpace(string(m.input))
	kind := m.inKind
	a := m.selected()
	if kind == inRename || kind == inGroup {
		a = m.agentByKey(m.promptFor) // the agent the prompt was opened for
	}
	if kind == inReply && (a == nil || a.Interactive) {
		m.flash("pick the agent to reply to first (↑↓)", true)
		return nil
	}
	m.input, m.inKind = m.input[:0], inPrompt
	switch kind {
	case inRename:
		if a == nil {
			return nil
		}
		if text == "" || text == a.Name {
			delete(m.store.Overlay.Names, a.Key)
		} else {
			m.store.Overlay.Names[a.Key] = text
		}
		_ = m.store.SaveOverlay()
		m.refresh()
		return nil
	case inGroup:
		if a == nil {
			return nil
		}
		if text == "" {
			delete(m.store.Overlay.Groups, a.Key)
		} else {
			m.store.Overlay.Groups[a.Key] = text
			if m.store.Config.GroupBy != "group" {
				m.store.Config.GroupBy = "group"
				_ = m.store.SaveConfig()
			}
		}
		_ = m.store.SaveOverlay()
		m.refresh()
		return nil
	}
	if kind == inReply {
		if a == nil || text == "" {
			return nil
		}
		m.inKind = inReply
		m.markSeen(a)
		m.flash("sending to "+a.DisplayName+"…", false)
		m.loader.Nudge(a.Key)
		m.refresh()
		if a.Agtop {
			return sendHosted(a, text)
		}
		return cmdErr("sent to "+a.DisplayName, func() error { return actions.Reply(a.Acct, a.ID, text) })
	}
	if text == "" {
		return m.attach(a)
	}
	if strings.HasPrefix(text, "/") {
		return m.command(text)
	}
	// The Prompt only starts new sessions; replies go through a Session's
	// own message box.
	if d := m.store.Config.Dispatch; d.RunIn != "daemon" && (d.Agent == "" || d.Agent == "claude") {
		return m.startHosted(text, m.startDir())
	}
	acct := m.store.Config.ActiveAccount()
	dir := m.startDir()
	flags := m.store.Config.Dispatch.Flags()
	m.flash("starting a new session…", false)
	return func() tea.Msg {
		id, err := actions.Dispatch(acct, dir, text, flags...)
		if err != nil {
			return doneMsg{err: err}
		}
		return doneMsg{text: "started " + id}
	}
}

func (m *Model) command(text string) tea.Cmd {
	f := strings.Fields(text)
	name, arg := f[0], strings.TrimSpace(strings.TrimPrefix(text, f[0]))
	a := m.selected()
	need := func() bool {
		if a == nil {
			m.flash("select an agent first", true)
			return false
		}
		return true
	}
	switch name {
	case "/done", "/undone":
		if need() {
			m.toggleDone(a)
		}
	case "/stop":
		if need() {
			return cmdErr("stopped "+a.DisplayName, func() error { return actions.Stop(a.Acct, a.ID, a.PID) })
		}
	case "/rm", "/delete":
		if need() {
			m.confirm = &confirmation{
				question: "Delete " + a.DisplayName + "?",
				detail:   "removes the session, and its worktree when that's safe",
				onYes: func() tea.Cmd {
					return cmdErr("deleted "+a.DisplayName, func() error { return actions.Remove(a.Acct, a.ID) })
				},
			}
		}
	case "/kill":
		if need() {
			m.askKillTree(a)
		}
	case "/cd", "/move":
		if need() {
			if expand(arg) == "" {
				m.flash("which folder? /cd <path>", true)
				return nil
			}
			return m.relaunch(a, expand(arg), nil, a.Acct)
		}
	case "/add-dir":
		if need() {
			if expand(arg) == "" {
				m.flash("which folder? /add-dir <path>", true)
				return nil
			}
			return m.relaunch(a, "", []string{expand(arg)}, a.Acct)
		}
	case "/account":
		if arg == "" {
			m.setView(3) // Accounts
			return nil
		}
		return m.useAccount(arg)
	case "/group":
		if need() {
			m.inKind, m.input, m.promptFor = inGroup, []rune(arg), a.Key
			return m.submit()
		}
	case "/by":
		for _, g := range groupModes {
			if g == arg {
				m.store.Config.GroupBy = g
				_ = m.store.SaveConfig()
				m.rebuild()
				return nil
			}
		}
		m.flash("group by one of: "+strings.Join(groupModes, ", "), true)
	case "/rename":
		if need() {
			m.inKind, m.input, m.promptFor = inRename, []rune(arg), a.Key
			return m.submit()
		}
	case "/sort":
		for _, mode := range sortModes {
			if mode == arg {
				m.setSort(mode)
				return nil
			}
		}
		m.flash("sort by one of: "+strings.Join(sortModes, ", "), true)
	case "/native":
		return m.nativeView()
	case "/width":
		var pct float64
		if _, err := fmt.Sscanf(strings.TrimSuffix(arg, "%"), "%g", &pct); err != nil || pct <= 0 {
			m.store.Config.SideWidth = 0
			_ = m.store.SaveConfig()
			m.flash("list width back to agtop's choice · /width 30% sets your own", false)
			return nil
		}
		m.setSideWidth(int(pct / 100 * float64(m.w)))
	case "/agtop":
		if need() {
			return m.moveToAgtop(a)
		}
	case "/hibernate":
		var n int
		fmt.Sscanf(arg, "%d", &n)
		m.store.Config.Hibernate.AfterMinutes = n
		_ = m.store.SaveConfig()
		if n > 0 {
			m.flash(fmt.Sprintf("finished agents stop after %dm idle", n), false)
		} else {
			m.flash("hibernation off", false)
		}
	case "/help":
		m.mode = modeHelp
	case "/quit", "/exit":
		m.scanner.Flush()
		return tea.Quit
	default:
		m.flash("unknown command "+name+" — ? lists them", true)
	}
	return nil
}

func expand(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "~") {
		home, _ := os.UserHomeDir()
		p = home + p[1:]
	}
	if p != "" && !filepath.IsAbs(p) {
		p, _ = filepath.Abs(p)
	}
	return filepath.Clean(p)
}

func (m *Model) relaunch(a *fleet.Agent, dir string, addDirs []string, to claude.Account) tea.Cmd {
	note := ""
	if dir != "" && dir != a.Cwd {
		note = fmt.Sprintf("Your working directory is now %s (it was %s). Paths from earlier in this conversation point at the old folder.", dir, a.Cwd)
	}
	if len(addDirs) > 0 {
		note = fmt.Sprintf("You now also have access to %s.", strings.Join(addDirs, ", "))
	}
	if to.Name != a.Acct.Name && note == "" {
		note = "This conversation moved to another account; carry on where you left off."
	}
	r := actions.Relaunch{From: a.Acct, To: to, Job: a.Job, Dir: dir, AddDirs: addDirs, Note: note}
	m.flash("relaunching "+a.DisplayName+"…", false)
	return func() tea.Msg {
		id, err := r.Run()
		if err != nil {
			return doneMsg{err: err}
		}
		return movedMsg{from: a, to: state.Key(to.Name, id)}
	}
}

func (m *Model) confirmKey(s string) tea.Cmd {
	c := m.confirm
	switch s {
	case "y", "enter":
		m.confirm = nil
		if c.onYes != nil {
			return c.onYes()
		}
	case "!":
		m.confirm = nil
		if c.onBang != nil {
			return c.onBang()
		}
	case "n", "esc", "ctrl+c":
		m.confirm = nil
	}
	return nil
}

func (m *Model) askKillTree(a *fleet.Agent) {
	if a.Worker == nil || m.snap.Table == nil {
		m.flash(a.DisplayName+" has no running process", true)
		return
	}
	memBytes, _, n := m.snap.Table.Sum(a.Worker.PID, nil)
	root := a.Worker.PID
	var start time.Time
	if p := m.snap.Table.Procs[root]; p != nil {
		start = p.Start
	}
	m.confirm = &confirmation{
		question: "Stop " + a.DisplayName + "?",
		detail:   fmt.Sprintf("%d processes · %s · the conversation is kept", n, mem(memBytes)),
		onYes: func() tea.Cmd {
			return cmdErr("stopped "+a.DisplayName, func() error { return actions.Stop(a.Acct, a.ID, a.PID) })
		},
		bangText: "SIGKILL the whole tree",
		onBang:   killTree(root, start),
	}
}

// procRows are the rows the process screen shows, in order.
// procRows lists every agent's process tree, alphabetically, each under a
// heading row, then the Claude processes that belong to no agent.
func (m *Model) procRows() []procRow {
	tab := m.snap.Table
	if tab == nil {
		return nil
	}
	var out []procRow
	agents := append([]*fleet.Agent(nil), m.snap.Agents...)
	sort.SliceStable(agents, func(i, j int) bool {
		return strings.ToLower(agents[i].DisplayName) < strings.ToLower(agents[j].DisplayName)
	})
	for _, a := range agents {
		if a.PID == 0 || tab.Procs[a.PID] == nil {
			continue
		}
		out = append(out, procRow{pid: a.PID, label: oneLine(a.DisplayName), cmd: m.context(a), mem: a.Mem, cpu: a.CPU,
			n: a.Procs, start: tab.Procs[a.PID].Start, key: a.Key, heading: true})
		for _, n := range tab.Tree(a.PID) {
			out = append(out, procRow{pid: n.PID, depth: n.Depth + 1, cmd: m.shortCmd(n.PID, n.Comm), mem: n.Footprint, cpu: n.CPU, n: 1, start: n.Start, key: a.Key})
		}
	}
	for _, r := range m.snap.Machine.Rows {
		if r.Role == fleet.RoleWorker {
			continue
		}
		out = append(out, procRow{pid: r.PID, label: r.Label, cmd: r.Cmd, mem: r.Mem, cpu: r.CPU, n: r.Procs, start: r.Start, role: r.Role, other: true})
	}
	return out
}

type procRow struct {
	pid, depth, n int
	label, cmd    string
	mem           uint64
	cpu           float64
	start         time.Time
	role          fleet.Role
	key           string
	heading       bool // an agent's own row above its tree
	other         bool // a Claude process that belongs to no agent
}

func (m *Model) procKey(s string) tea.Cmd {
	rows := m.procRows()
	switch s {
	case "esc", "q", "ctrl+p", "left":
		m.setView(0)
	case "up", "k":
		if m.procCursor > 0 {
			m.procCursor--
		}
	case "down", "j":
		if m.procCursor < len(rows)-1 {
			m.procCursor++
		}
	case "enter":
		if m.procCursor < len(rows) && rows[m.procCursor].key != "" {
			m.sel = rows[m.procCursor].key
			m.setView(0)
		}
	case "ctrl+x", "x":
		if m.procCursor < len(rows) {
			r := rows[m.procCursor]
			m.confirm = &confirmation{
				question: fmt.Sprintf("Send SIGTERM to %d?", r.pid),
				detail:   trimCmd(r.cmd, 80),
				onYes: func() tea.Cmd {
					return cmdErr(fmt.Sprintf("sent SIGTERM to %d", r.pid), func() error { return actions.Terminate(r.pid) })
				},
				bangText: "SIGKILL it and everything under it",
				onBang:   killTree(r.pid, r.start),
			}
		}
	case "!":
		if m.procCursor < len(rows) {
			r := rows[m.procCursor]
			m.confirm = &confirmation{
				question: fmt.Sprintf("SIGKILL %d and everything under it?", r.pid),
				detail:   trimCmd(r.cmd, 80),
				onYes:    killTree(r.pid, r.start),
			}
		}
	}
	return nil
}

func killTree(pid int, start time.Time) func() tea.Cmd {
	return func() tea.Cmd {
		return func() tea.Msg {
			n, err := actions.KillTree(pid, start)
			if err != nil {
				return doneMsg{err: err}
			}
			return doneMsg{text: fmt.Sprintf("killed %d processes", n)}
		}
	}
}

func trimCmd(s string, n int) string {
	home, _ := os.UserHomeDir()
	s = strings.ReplaceAll(s, home, "~")
	return ansi.Truncate(s, n, "…")
}

func (m *Model) useAccount(name string) tea.Cmd {
	for _, a := range m.store.Config.AllAccounts() {
		if a.Name == name {
			m.store.Config.Active = name
			_ = m.store.SaveConfig()
			m.flash("new sessions start on "+name, false)
			m.refresh()
			return nil
		}
	}
	m.flash("no account named "+name, true)
	return nil
}

func (m *Model) openCwd(a *fleet.Agent) {
	m.mode, m.cwdFor, m.cwdCursor, m.cwdMove = modeCwd, a.Key, -1, true
	m.input = []rune(tildify(a.Cwd))
}

// cwdChoices are the folders worth offering: every folder an agent ran in,
// their repositories, and those repositories' Claude worktrees.
func (m *Model) cwdChoices() []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, a := range m.snap.Agents {
		add(a.Repo)
		if a.Repo != "" {
			wt := filepath.Join(a.Repo, ".claude", "worktrees")
			if ents, err := os.ReadDir(wt); err == nil {
				for _, e := range ents {
					if e.IsDir() {
						add(filepath.Join(wt, e.Name()))
					}
				}
			}
		}
		add(a.Cwd)
	}
	q := strings.ToLower(strings.TrimSpace(string(m.input)))
	q = strings.TrimPrefix(q, "~")
	var hits []string
	for _, p := range out {
		if q == "" || strings.Contains(strings.ToLower(p), q) {
			hits = append(hits, p)
		}
	}
	sort.Strings(hits)
	return hits
}

func (m *Model) cwdKey(k tea.KeyPressMsg, s string) tea.Cmd {
	choices := m.cwdChoices()
	switch s {
	case "esc":
		m.mode, m.input = modeList, m.input[:0]
	case "tab":
		m.cwdMove = !m.cwdMove
	case "up":
		if m.cwdCursor > 0 {
			m.cwdCursor--
		}
	case "down":
		if m.cwdCursor < len(choices)-1 {
			m.cwdCursor++
		}
	case "enter":
		target := expand(string(m.input))
		if m.cwdCursor >= 0 && m.cwdCursor < len(choices) {
			target = choices[m.cwdCursor]
		}
		var a *fleet.Agent
		for _, x := range m.snap.Agents {
			if x.Key == m.cwdFor {
				a = x
			}
		}
		m.mode, m.input = modeList, m.input[:0]
		if a == nil {
			return nil
		}
		if m.cwdMove {
			return m.relaunch(a, target, nil, a.Acct)
		}
		return m.relaunch(a, "", []string{target}, a.Acct)
	default:
		if m.editKey(k, s) {
			m.cwdCursor = -1
		}
	}
	return nil
}
