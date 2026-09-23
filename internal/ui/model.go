// Package ui is the agtop view: the native layout plus cost, time, CPU/RAM,
// preview, processes, accounts, groups and folder moves.
package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/actions"
	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/daemon"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/state"
)

type mode int

const (
	modeList mode = iota
	modeProcs
	modeAccounts
	modeCwd
	modeHelp
)

type inputKind int

const (
	inPrompt inputKind = iota
	inRename
	inGroup
	inReply
	inNewAccount
)

var groupModes = []string{"status", "repo", "account", "group"}

type confirmation struct {
	question string
	detail   string
	onYes    func() tea.Cmd
	onBang   func() tea.Cmd
	bangText string
}

type Model struct {
	store     *state.Store
	loader    *fleet.Loader
	scanner   *fleet.Scanner
	snap      *fleet.Snapshot
	launchDir string
	version   string

	w, h     int
	mode     mode
	tick     int
	scanning bool
	loaded   bool

	sel          string
	order        []*fleet.Agent
	lines        []listLine
	scroll       int
	preview      bool
	full         bool
	previews     map[string]previewEntry
	live         *live
	liveOpening  string
	liveFailed   string
	liveFailedAt time.Time
	expanded     map[string]bool
	hover        string
	hoverAt      time.Time
	rowKeys      []string
	listTop      int
	lastClick    time.Time

	input  []rune
	inKind inputKind
	dirIdx int

	status     string
	statusErr  bool
	statusAt   time.Time
	armed      string
	quitArmed  time.Time
	confirm    *confirmation
	dialog     *dialog
	hibernated map[string]bool
	armedAt    time.Time
	attached   string
	view       int

	procCursor  int
	procMachine bool
	acctCursor  int
	cwdMove     bool
	cwdCursor   int
	cwdFor      string

	lastState map[string]string
}

type previewEntry struct {
	p    claude.Preview
	size int64
}

type lineKind int

const (
	lineBlank lineKind = iota
	lineSection
	lineAgent
	lineSub
	lineCard
	lineTask
)

type listLine struct {
	kind   lineKind
	title  string
	meta   string
	folded bool
	peek   string
	agent  *fleet.Agent
	task   claude.Task
	last   bool
	more   int
}

func sectionKey(title string) string { return "§" + title }

func New(store *state.Store, version string) *Model {
	dir, _ := os.Getwd()
	m := &Model{
		store: store, loader: fleet.NewLoader(store), scanner: fleet.NewScanner(),
		launchDir: dir, version: version, previews: map[string]previewEntry{},
		expanded: map[string]bool{}, lastState: map[string]string{}, cwdMove: true,
		hibernated: map[string]bool{},
	}
	if store.Config.GroupBy == "" {
		store.Config.GroupBy = "status"
	}
	m.snap = m.loader.Load(true)
	m.rebuild()
	return m
}

type tickMsg time.Time
type hoverMsg struct{}
type scanMsg map[string]fleet.Spend
type previewMsg struct {
	key string
	e   previewEntry
}
type doneMsg struct {
	text string
	err  error
}
type movedMsg struct {
	from *fleet.Agent
	to   string
}
type attachDoneMsg struct {
	agent *fleet.Agent
	err   error
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *Model) Init() tea.Cmd { return tea.Batch(tick(), m.scan()) }

func (m *Model) scan() tea.Cmd {
	if m.scanning {
		return nil
	}
	m.scanning = true
	targets := m.targets()
	sc := m.scanner
	return func() tea.Msg { return scanMsg(sc.Run(targets)) }
}

// startDirs are the folders a new session can start in: where agtop was
// opened, then folders with agents running, then recent ones.
func (m *Model) startDirs() []string {
	seen := map[string]bool{}
	var out []string
	add := func(d string) {
		if d != "" && !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	add(m.launchDir)
	agents := append([]*fleet.Agent(nil), m.snap.Agents...)
	sort.SliceStable(agents, func(i, j int) bool {
		if agents[i].Open() != agents[j].Open() {
			return agents[i].Open()
		}
		return agents[i].UpdatedAt.After(agents[j].UpdatedAt)
	})
	for _, a := range agents {
		if len(out) >= 12 {
			break
		}
		if a.Interactive && strings.Contains(a.Cwd, "/var/folders/") {
			continue
		}
		add(a.Cwd)
	}
	return out
}

// dockLines is how many lines of the latest message the dock shows.
func (m *Model) dockLines() int {
	n := m.store.Config.DockLines
	if n == 0 {
		n = 3
	}
	return min(max(n, 1), max(1, m.h/3))
}

func (m *Model) startDir() string {
	dirs := m.startDirs()
	return dirs[((m.dirIdx%len(dirs))+len(dirs))%len(dirs)]
}

func (m *Model) targets() []fleet.Target {
	var targets []fleet.Target
	for _, a := range m.snap.Agents {
		if a.TranscriptPath != "" {
			targets = append(targets, fleet.Target{Key: a.Key, Path: a.TranscriptPath})
		}
	}
	return targets
}

func (m *Model) rowAt(y int) string {
	i := y - m.listTop
	if m.mode != modeList || m.dialog != nil || i < 0 || i >= len(m.rowKeys) {
		return ""
	}
	return m.rowKeys[i]
}

func (m *Model) mouseMove(y int) tea.Cmd {
	k := m.rowAt(y)
	if k != m.hover {
		m.hover, m.hoverAt = k, time.Now()
		if k != "" && !strings.HasPrefix(k, "§") {
			return tea.Tick(400*time.Millisecond, func(time.Time) tea.Msg { return hoverMsg{} })
		}
	}
	return nil
}

func (m *Model) mouseClick(y int) tea.Cmd {
	k := m.rowAt(y)
	if k == "" {
		return nil
	}
	double := k == m.sel && time.Since(m.lastClick) < 400*time.Millisecond
	m.sel, m.lastClick, m.armed = k, time.Now(), ""
	if strings.HasPrefix(k, "§") {
		m.toggleFold(strings.TrimPrefix(k, "§"))
		return nil
	}
	if double {
		return m.attach(m.selected())
	}
	return m.loadPreview()
}

func (m *Model) loadPreview() tea.Cmd {
	a := m.focused()
	if a == nil || a.TranscriptPath == "" {
		return nil
	}
	st, err := os.Stat(a.TranscriptPath)
	if err != nil {
		return nil
	}
	if e, ok := m.previews[a.Key]; ok && e.size == st.Size() {
		return nil
	}
	key, path, size := a.Key, a.TranscriptPath, st.Size()
	return func() tea.Msg {
		return previewMsg{key: key, e: previewEntry{p: claude.ReadPreview(path, 384<<10), size: size}}
	}
}

// loadLivePreviews keeps the transcript tail of every working agent fresh, so
// rows can say what each one is doing right now.
func (m *Model) loadLivePreviews() tea.Cmd {
	var cmds []tea.Cmd
	for _, a := range m.snap.Agents {
		if !a.Live() || a.TranscriptPath == "" || len(cmds) >= 8 {
			continue
		}
		st, err := os.Stat(a.TranscriptPath)
		if err != nil {
			continue
		}
		if e, ok := m.previews[a.Key]; ok && e.size == st.Size() {
			continue
		}
		key, path, size := a.Key, a.TranscriptPath, st.Size()
		cmds = append(cmds, func() tea.Msg {
			return previewMsg{key: key, e: previewEntry{p: claude.ReadPreview(path, 128<<10), size: size}}
		})
	}
	return tea.Batch(cmds...)
}

// markSeen acknowledges an agent's question until it asks something new.
func (m *Model) markSeen(a *fleet.Agent) {
	if a == nil || a.State != "blocked" {
		return
	}
	m.store.Overlay.Seen[a.Key] = time.Now()
	_ = m.store.SaveOverlay()
}

func (m *Model) flash(s string, err bool) {
	m.status, m.statusErr, m.statusAt = s, err, time.Now()
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := m.update(msg)
	return m, tea.Batch(cmd, m.syncLive())
}

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case liveOpenMsg:
		return m, m.onLiveOpen(msg)
	case liveMsg:
		return m, m.onLive(msg)
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return m, nil
	case tickMsg:
		m.tick++
		m.refresh()
		cmds := []tea.Cmd{tick()}
		if m.tick%3 == 0 {
			cmds = append(cmds, m.scan())
		}
		cmds = append(cmds, m.loadPreview())
		if m.tick%2 == 0 {
			cmds = append(cmds, m.loadLivePreviews())
		}
		return m, tea.Batch(cmds...)
	case scanMsg:
		m.scanning = false
		m.loader.SetSpend(msg)
		m.loaded = true
		m.refresh()
		return m, nil
	case dialogReload:
		if m.dialog != nil {
			m.loadDialog()
		}
		if msg.err != nil {
			m.flash(msg.err.Error(), true)
		}
		return m, nil
	case hoverMsg:
		return m, m.loadPreview()
	case previewMsg:
		m.previews[msg.key] = msg.e
		return m, nil
	case doneMsg:
		if msg.err != nil {
			m.flash(msg.err.Error(), true)
		} else if msg.text != "" {
			m.flash(msg.text, false)
		}
		m.refresh()
		return m, nil
	case movedMsg:
		// The old row keeps its history; it moves to Done and points at the new one.
		m.store.Overlay.Moved[msg.from.Key] = msg.to
		m.store.Overlay.Done[msg.from.Key] = time.Now()
		if n := m.store.Overlay.Names[msg.from.Key]; n != "" {
			m.store.Overlay.Names[msg.to] = n
		}
		_ = m.store.SaveOverlay()
		m.sel = msg.to
		m.flash("moved — now "+strings.TrimPrefix(msg.to, msg.from.Acct.Name+"/"), false)
		m.refresh()
		return m, nil
	case attachDoneMsg:
		m.attached = ""
		m.refresh()
		if msg.err != nil && msg.agent != nil {
			if daemon.IsRefusal(msg.err, "EKICKED") {
				m.flash("opened in another window", false)
				return m, nil
			}
			a := msg.agent
			return m, tea.ExecProcess(actions.AttachFallback(a.Acct, a.ID), func(err error) tea.Msg {
				return doneMsg{err: err}
			})
		}
		return m, nil
	case tea.PasteMsg:
		if m.acceptsText() {
			m.input = append(m.input, []rune(oneLine(msg.Content))...)
		}
		return m, nil
	case tea.KeyPressMsg:
		return m, m.key(msg)
	case tea.MouseMotionMsg:
		return m, m.mouseMove(msg.Y)
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			return m, m.mouseClick(msg.Y)
		}
	case tea.MouseWheelMsg:
		if m.mode == modeList && m.dialog == nil {
			switch msg.Button {
			case tea.MouseWheelUp:
				m.move(-3)
			case tea.MouseWheelDown:
				m.move(3)
			}
			return m, m.loadPreview()
		}
	}
	return m, nil
}

var viewNames = []string{"Agents", "Processes", "Accounts", "Coding agents", "Settings"}

// setView switches the whole screen; tab and shift+tab cycle through them.
func (m *Model) setView(v int) {
	m.view = (v + len(viewNames)) % len(viewNames)
	m.dialog, m.mode = nil, modeList
	switch m.view {
	case 1:
		a := m.selected()
		m.mode, m.procCursor, m.procMachine = modeProcs, 0, a == nil || a.PID == 0
	case 2, 3, 4:
		m.openDialog(m.view - 2)
	}
}

// wide is when the preview gets its own half of the screen.
func (m *Model) wide() bool { return m.w >= 170 }

func (m *Model) acceptsText() bool {
	return m.confirm == nil && m.dialog == nil && (m.mode == modeList || m.mode == modeCwd || m.inKind == inNewAccount)
}

func (m *Model) refresh() {
	m.snap = m.loader.Load(true)
	m.notify()
	m.hibernate()
	m.rebuild()
}

// notify posts a notification when an agent starts waiting on the user.
func (m *Model) notify() {
	first := len(m.lastState) == 0
	for _, a := range m.snap.Agents {
		if a.Checking {
			continue
		}
		prev := m.lastState[a.Key]
		m.lastState[a.Key] = a.State
		if !first && !m.store.Config.Quiet && prev != "" && prev != "blocked" && a.NeedsYou() {
			body := a.Needs
			if body == "" {
				body = oneLine(a.Detail)
			}
			actions.Notify(a.DisplayName+" needs you", body)
		}
	}
}

// hibernate stops finished agents whose process is still resident.
func (m *Model) hibernate() {
	after := m.store.Config.Hibernate.AfterMinutes
	if after <= 0 {
		return
	}
	for _, a := range m.snap.Agents {
		if a.Worker != nil && a.State == "done" && a.Age(m.snap.At) > time.Duration(after)*time.Minute && !m.hibernated[a.Key] {
			m.hibernated[a.Key] = true // one try each; a failed stop is not retried every second
			go actions.Stop(a.Acct, a.ID)
		}
	}
}

func (m *Model) selected() *fleet.Agent {
	for _, a := range m.order {
		if a.Key == m.sel {
			return a
		}
	}
	return nil
}

func (m *Model) selIndex() int {
	for i, a := range m.order {
		if a.Key == m.sel {
			return i
		}
	}
	return -1
}

func (m *Model) move(d int) {
	items := m.items()
	if len(items) == 0 {
		return
	}
	i := 0
	for j, k := range items {
		if k == m.sel {
			i = j
		}
	}
	i = min(max(i+d, 0), len(items)-1)
	m.sel = items[i]
	m.armed, m.hover = "", ""
}

// items are the selectable rows in display order: sections and agents.
func (m *Model) items() []string {
	var out []string
	for _, l := range m.lines {
		switch l.kind {
		case lineSection:
			out = append(out, sectionKey(l.title))
		case lineAgent:
			out = append(out, l.agent.Key)
		}
	}
	return out
}

func (m *Model) folded(title string) bool {
	if v, ok := m.store.Config.Folds[title]; ok {
		return v
	}
	return title == "Earlier"
}

func (m *Model) toggleFold(title string) {
	if m.store.Config.Folds == nil {
		m.store.Config.Folds = map[string]bool{}
	}
	m.store.Config.Folds[title] = !m.folded(title)
	_ = m.store.SaveConfig()
	m.rebuild()
}

// focused is the agent whose card is open: the selection, or a row the
// mouse has rested on.
func (m *Model) focused() *fleet.Agent {
	key := m.sel
	if m.hover != "" && time.Since(m.hoverAt) > 350*time.Millisecond {
		key = m.hover
	}
	for _, a := range m.order {
		if a.Key == key {
			return a
		}
	}
	return nil
}

// rebuild groups the agents for the current group-by mode. What needs the
// user comes first; anything finished more than a day ago goes to Earlier.
func (m *Model) rebuild() {
	by := m.store.Config.GroupBy
	now := m.snap.At
	type group struct {
		name   string
		agents []*fleet.Agent
		rank   int
		recent time.Time
	}
	groups := map[string]*group{}
	add := func(name string, rank int, a *fleet.Agent) {
		g := groups[name]
		if g == nil {
			g = &group{name: name, rank: rank}
			groups[name] = g
		}
		g.agents = append(g.agents, a)
		if a.UpdatedAt.After(g.recent) {
			g.recent = a.UpdatedAt
		}
	}
	for _, a := range m.snap.Agents {
		fresh := a.Open() || a.Busy() || a.Pinned || a.Age(now) < 24*time.Hour
		switch {
		case a.NeedsYou():
			add("Needs you", 0, a)
		case a.Waiting():
			add("Waiting on you", 3, a)
		case a.Checking || a.JustFinished(now):
			add("Working", 2, a)
		case a.Pinned:
			add("Pinned", 1, a)
		case a.Live() || a.Busy():
			add("Working", 2, a)
		case a.PID != 0:
			add("Idle", 3, a)
		case !fresh:
			add("Earlier", 9, a)
		case a.Done:
			add("Done", 8, a)
		case by == "repo":
			name := "No repository"
			if a.Repo != "" {
				name = filepath.Base(a.Repo)
				if a.Branch != "" {
					name += " · " + a.Branch
				}
			}
			add(name, 4, a)
		case by == "account":
			add(a.Acct.Name, 4, a)
		case by == "group" && a.Group != "":
			add(a.Group, 4, a)
		case a.Live():
			add("Working", 2, a)
		default:
			add("Today", 7, a)
		}
	}
	list := make([]*group, 0, len(groups))
	for _, g := range groups {
		list = append(list, g)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].rank != list[j].rank {
			return list[i].rank < list[j].rank
		}
		return list[i].recent.After(list[j].recent)
	})
	for _, g := range list {
		rank := func(a *fleet.Agent) int {
			switch {
			case a.Live() && !a.Checking:
				return 0
			case a.Checking || a.JustFinished(now):
				return 1
			default:
				return 2
			}
		}
		sort.SliceStable(g.agents, func(i, j int) bool { return rank(g.agents[i]) < rank(g.agents[j]) })
	}
	m.order = m.order[:0]
	m.lines = m.lines[:0]
	for _, g := range list {
		var cost float64
		var names []string
		for _, a := range g.agents {
			cost += a.Spend.Cost
			if len(names) < 4 {
				names = append(names, oneLine(a.DisplayName))
			}
		}
		fold := m.folded(g.name)
		meta := sectionMeta(len(g.agents), cost)
		if g.name == "Idle" {
			var held uint64
			for _, a := range g.agents {
				held += a.Mem
			}
			meta = fmt.Sprintf("%d · %s held in memory", len(g.agents), mem(held))
		}
		m.lines = append(m.lines, listLine{kind: lineSection, title: g.name, meta: meta,
			folded: fold, peek: strings.Join(names, ", ")})
		for _, a := range g.agents {
			m.order = append(m.order, a)
			if fold {
				continue
			}
			m.lines = append(m.lines, listLine{kind: lineAgent, agent: a})

		}
		m.lines = append(m.lines, listLine{kind: lineBlank})
	}
	valid := false
	for _, k := range m.items() {
		valid = valid || k == m.sel
	}
	if !valid {
		if items := m.items(); len(items) > 0 {
			m.sel = items[min(1, len(items)-1)]
		}
	}
}

func sectionMeta(n int, cost float64) string {
	s := fmt.Sprintf("%d", n)
	if cost > 0 {
		s += " · " + money(cost)
	}
	return s
}

func tildify(p string) string {
	home, _ := os.UserHomeDir()
	if home != "" && strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}

func (m *Model) account(name string) claude.Account {
	for _, a := range m.store.Config.AllAccounts() {
		if a.Name == name {
			return a
		}
	}
	return m.store.Config.ActiveAccount()
}

func cmdErr(text string, f func() error) tea.Cmd {
	return func() tea.Msg {
		if err := f(); err != nil {
			return doneMsg{err: err}
		}
		return doneMsg{text: text}
	}
}

func (m *Model) attach(a *fleet.Agent) tea.Cmd {
	if a == nil {
		return nil
	}
	m.attached = a.Key
	m.markSeen(a)
	if a.Interactive {
		m.flash(fmt.Sprintf("%s is open in another terminal (pid %d)", a.DisplayName, a.PID), false)
		return nil
	}
	// One attach at a time from here: the preview's would fight the full
	// screen over the session's size.
	m.closeLive()
	s := &daemon.Session{Client: daemon.Client{Account: a.Acct}, Short: a.ID}
	return tea.Exec(s, func(err error) tea.Msg { return attachDoneMsg{agent: a, err: err} })
}

func (m *Model) togglePin(a *fleet.Agent) tea.Cmd {
	pins, err := claude.LoadPins(a.Acct)
	if err != nil {
		m.flash(err.Error(), true)
		return nil
	}
	out := pins[:0:0]
	found := false
	for _, id := range pins {
		if id == a.ID {
			found = true
			continue
		}
		out = append(out, id)
	}
	if !found {
		out = append(out, a.ID)
	}
	return cmdErr("", func() error { return claude.WritePins(a.Acct, out) })
}

func (m *Model) toggleDone(a *fleet.Agent) {
	if _, ok := m.store.Overlay.Done[a.Key]; ok {
		delete(m.store.Overlay.Done, a.Key)
		m.flash("moved back: "+a.DisplayName, false)
	} else {
		m.store.Overlay.Done[a.Key] = time.Now()
		m.flash("done: "+a.DisplayName, false)
	}
	_ = m.store.SaveOverlay()
	m.refresh()
}

func (m *Model) nativeView() tea.Cmd {
	c := exec.Command("claude", "agents")
	c.Env = m.store.Config.ActiveAccount().Env()
	return tea.ExecProcess(c, func(err error) tea.Msg { return doneMsg{err: err} })
}
