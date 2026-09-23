package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/actions"
	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/state"
)

func (m *Model) key(k tea.KeyPressMsg) tea.Cmd {
	s := k.String()
	if m.confirm != nil {
		return m.confirmKey(s)
	}
	if s == "ctrl+c" && len(m.input) == 0 && m.mode == modeList {
		if time.Since(m.quitArmed) < 2*time.Second {
			m.scanner.Flush()
			return tea.Quit
		}
		m.quitArmed = time.Now()
		m.flash("ctrl+c again to exit", false)
		return nil
	}
	switch m.mode {
	case modeHelp:
		m.mode = modeList
		return nil
	case modeProcs:
		return m.procKey(s)
	case modeAccounts:
		return m.acctKey(k, s)
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
	switch s {
	case "up", "ctrl+k":
		m.move(-1)
		return m.loadPreview()
	case "down", "ctrl+j":
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
	case "tab":
		m.preview = !m.preview
		return m.loadPreview()
	case "right":
		if empty {
			m.preview = true
			return m.loadPreview()
		}
	case "left":
		if empty && m.preview {
			m.preview = false
			return nil
		}
	case "esc":
		switch {
		case !empty:
			m.input, m.inKind = m.input[:0], inPrompt
		case m.preview:
			m.preview = false
		default:
			m.armed = ""
		}
		return nil
	case "enter":
		return m.submit()
	case "ctrl+r":
		if a != nil {
			m.inKind, m.input = inRename, []rune(a.DisplayName)
		}
		return nil
	case "ctrl+e":
		if a != nil {
			m.inKind, m.input = inGroup, []rune(a.Group)
		}
		return nil
	case "ctrl+t":
		if a != nil {
			return m.togglePin(a)
		}
	case "ctrl+f":
		if a != nil {
			m.toggleDone(a)
		}
		return nil
	case "ctrl+s":
		m.cycleGroupBy()
		return nil
	case "ctrl+o":
		if a != nil {
			m.expanded[a.Key] = !m.expanded[a.Key]
			m.rebuild()
		}
		return nil
	case "ctrl+x":
		return m.stopOrRemove(a)
	case "ctrl+p":
		m.mode, m.procCursor, m.procMachine = modeProcs, 0, a == nil || a.Worker == nil
		return nil
	case "ctrl+a":
		m.mode, m.acctCursor = modeAccounts, 0
		return nil
	case "ctrl+l":
		if a != nil {
			m.openCwd(a)
		}
		return nil
	case "?":
		if empty {
			m.mode = modeHelp
			return nil
		}
	}
	m.editKey(k, s)
	return nil
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
	if a.Live() && a.Worker != nil {
		m.flash("stopping "+a.DisplayName+"…", false)
		return cmdErr("stopped "+a.DisplayName, func() error { return actions.Stop(a.Acct, a.ID) })
	}
	if m.armed == a.Key {
		m.armed = ""
		m.flash("deleting "+a.DisplayName+"…", false)
		return cmdErr("deleted "+a.DisplayName, func() error { return actions.Remove(a.Acct, a.ID) })
	}
	m.armed = a.Key
	m.flash("ctrl+x again to delete", false)
	return nil
}

func (m *Model) submit() tea.Cmd {
	text := strings.TrimSpace(string(m.input))
	kind := m.inKind
	m.input, m.inKind = m.input[:0], inPrompt
	a := m.selected()
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
	if text == "" {
		return m.attach(a)
	}
	if strings.HasPrefix(text, "/") {
		return m.command(text)
	}
	if m.preview && a != nil {
		m.flash("sending to "+a.DisplayName+"…", false)
		return cmdErr("sent to "+a.DisplayName, func() error { return actions.Reply(a.Acct, a.ID, text) })
	}
	acct := m.store.Config.ActiveAccount()
	dir := m.launchDir
	m.flash("starting a new session…", false)
	return func() tea.Msg {
		id, err := actions.Dispatch(acct, dir, text)
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
			return cmdErr("stopped "+a.DisplayName, func() error { return actions.Stop(a.Acct, a.ID) })
		}
	case "/rm", "/delete":
		if need() {
			return cmdErr("deleted "+a.DisplayName, func() error { return actions.Remove(a.Acct, a.ID) })
		}
	case "/kill":
		if need() {
			m.askKillTree(a)
		}
	case "/cd", "/move":
		if need() {
			return m.relaunch(a, expand(arg), nil, a.Acct)
		}
	case "/add-dir":
		if need() {
			return m.relaunch(a, "", []string{expand(arg)}, a.Acct)
		}
	case "/account":
		if arg == "" {
			m.mode = modeAccounts
			return nil
		}
		return m.useAccount(arg)
	case "/group":
		if need() {
			m.inKind, m.input = inGroup, []rune(arg)
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
			m.inKind, m.input = inRename, []rune(arg)
			return m.submit()
		}
	case "/native":
		return m.nativeView()
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
	mem, _, n := m.snap.Table.Sum(a.Worker.PID, nil)
	tab := m.snap.Table
	root := a.Worker.PID
	m.confirm = &confirmation{
		question: "Stop " + a.DisplayName + "?",
		detail:   fmt.Sprintf("%d processes · %s · the conversation is kept", n, memStr(mem)),
		onYes: func() tea.Cmd {
			return cmdErr("stopped "+a.DisplayName, func() error { return actions.Stop(a.Acct, a.ID) })
		},
		bangText: "SIGKILL the whole tree",
		onBang: func() tea.Cmd {
			return func() tea.Msg {
				return doneMsg{text: fmt.Sprintf("killed %d processes", actions.KillTree(tab, root))}
			}
		},
	}
}

func memStr(b uint64) string { return mem(b) }

// procRows are the rows the process screen shows, in order.
func (m *Model) procRows() []procRow {
	tab := m.snap.Table
	if tab == nil {
		return nil
	}
	var out []procRow
	if m.procMachine {
		for _, r := range m.snap.Machine.Rows {
			out = append(out, procRow{pid: r.PID, label: r.Label, cmd: r.Cmd, mem: r.Mem, cpu: r.CPU, n: r.Procs, start: r.Start, role: r.Role, key: r.Key})
		}
		return out
	}
	a := m.selected()
	if a == nil || a.Worker == nil {
		return nil
	}
	for _, n := range tab.Tree(a.Worker.PID) {
		cmd := m.shortCmd(n.PID, n.Comm)
		label := cmd
		if i := strings.IndexByte(cmd, ' '); i > 0 && len(cmd) > 40 {
			label = cmd[:i]
		}
		out = append(out, procRow{pid: n.PID, depth: n.Depth, label: label, cmd: cmd, mem: n.Footprint, cpu: n.CPU, n: 1, start: n.Start})
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
}

func (m *Model) procKey(s string) tea.Cmd {
	rows := m.procRows()
	switch s {
	case "esc", "q", "ctrl+p", "left":
		m.mode = modeList
	case "tab":
		m.procMachine, m.procCursor = !m.procMachine, 0
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
			m.sel, m.procMachine, m.procCursor = rows[m.procCursor].key, false, 0
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
				onBang:   m.killTreeCmd(r.pid),
			}
		}
	case "!":
		if m.procCursor < len(rows) {
			r := rows[m.procCursor]
			m.confirm = &confirmation{
				question: fmt.Sprintf("SIGKILL %d and everything under it?", r.pid),
				detail:   trimCmd(r.cmd, 80),
				onYes:    m.killTreeCmd(r.pid),
			}
		}
	}
	return nil
}

func (m *Model) killTreeCmd(pid int) func() tea.Cmd {
	tab := m.snap.Table
	return func() tea.Cmd {
		return func() tea.Msg {
			return doneMsg{text: fmt.Sprintf("killed %d processes", actions.KillTree(tab, pid))}
		}
	}
}

func trimCmd(s string, n int) string {
	home, _ := os.UserHomeDir()
	s = strings.ReplaceAll(s, home, "~")
	return fit(s, n)
}

func (m *Model) acctKey(k tea.KeyPressMsg, s string) tea.Cmd {
	accts := m.snap.Accounts
	if m.inKind == inNewAccount {
		switch s {
		case "esc":
			m.inKind, m.input = inPrompt, m.input[:0]
		case "enter":
			name := strings.TrimSpace(string(m.input))
			m.inKind, m.input = inPrompt, m.input[:0]
			return m.addAccount(name)
		default:
			m.editKey(k, s)
		}
		return nil
	}
	switch s {
	case "esc", "q", "ctrl+a":
		m.mode = modeList
	case "up", "k":
		if m.acctCursor > 0 {
			m.acctCursor--
		}
	case "down", "j":
		if m.acctCursor < len(accts)-1 {
			m.acctCursor++
		}
	case "enter":
		if m.acctCursor < len(accts) {
			return m.useAccount(accts[m.acctCursor].Name)
		}
	case "n", "ctrl+n":
		m.inKind, m.input = inNewAccount, m.input[:0]
	case "l":
		if m.acctCursor < len(accts) {
			acct := accts[m.acctCursor].Account
			return tea.ExecProcess(actions.Login(acct), func(err error) tea.Msg { return doneMsg{err: err, text: "signed in to " + acct.Name} })
		}
	case "m":
		a := m.selected()
		if a != nil && m.acctCursor < len(accts) && accts[m.acctCursor].Name != a.Acct.Name {
			to := accts[m.acctCursor].Account
			m.mode = modeList
			return m.relaunch(a, "", nil, to)
		}
	}
	return nil
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

func (m *Model) addAccount(name string) tea.Cmd {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	fields := strings.Fields(name)
	name = fields[0]
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".claude-"+name)
	if len(fields) > 1 {
		dir = expand(fields[1])
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		m.flash(err.Error(), true)
		return nil
	}
	m.store.Config.Accounts = append(m.store.Config.Accounts, claude.Account{Name: name, ConfigDir: dir})
	_ = m.store.SaveConfig()
	m.refresh()
	acct := claude.Account{Name: name, ConfigDir: dir}
	return tea.ExecProcess(actions.Login(acct), func(err error) tea.Msg {
		return doneMsg{err: err, text: "added " + name}
	})
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

var _ = syscall.SIGTERM
