// Package ui is the agtop view: the native layout plus cost, time, CPU/RAM,
// preview, processes, accounts, groups and folder moves.
package ui

import (
	"context"
	"errors"
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
	modeCwd
	modeHelp
)

type inputKind int

const (
	inPrompt inputKind = iota
	inRename
	inGroup
	inReply
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
	hover        string
	hoverAt      time.Time
	rowKeys      []string
	listTop      int
	lastClick    time.Time

	input  []rune
	back   int // cursor distance from the input's end
	anchor int // selection start + 1; 0 when nothing is selected
	// pendingCopy is text to put on the clipboard with the next update.
	pendingCopy string
	// Where the Prompt's box was drawn, so a click can place the cursor.
	promptBox    box
	promptBoxIdx int
	promptBoxY   int
	inKind       inputKind
	// paneFocus sends keys to an agtop-mode session's pane instead of the
	// list and its prompt.
	paneFocus bool
	dragging  bool     // resizing the list by its edge
	images    []string // image files attached to the prompt's next message
	// claudeView is a Claude Code agent's Session view: 0 its live screen,
	// 1 the summary.
	claudeView int
	zen        bool     // the Zen view: only the agent that needs you
	railW      int      // the recent-changes rail's width, 0 when there isn't room
	rail       []string // its lines for the frame being drawn
	frameLen   int      // bytes in the last frame, to size the next
	lastKeyAt  time.Time
	paneTop    int // screen row of the pane's first line, for clicks
	// host is the connection to the agtop-mode session the pane shows.
	host        *hostConn
	hostOpening string
	dirIdx      int

	status    string
	statusErr bool
	statusAt  time.Time
	armed     string
	quitArmed time.Time
	confirm   *confirmation
	dialog    *dialog
	picker    *picker
	embedded  bool
	promptFor string
	listW     int
	pastes    pastes // long pastes in the main box, shown as chips
	blurred   bool   // the terminal says agtop isn't the focused window
	sameFrame bool   // the last message changed nothing on screen
	lastFrame string // what View drew last
	// openFailed is when a Session last failed to open, by agent key; zen
	// skips those for a while rather than sticking on one it can't show.
	openFailed   map[string]time.Time
	localQ       map[string]*localQueue // messages waiting for Claude Code sessions, by agent key
	moveWhenIdle map[string]bool        // agents to move to agtop mode when their turn ends
	divHover     bool                   // the mouse is on the edge between Agents and the Session
	hibernated   map[string]bool
	usageWait    map[string]time.Time
	armedAt      time.Time
	attached     string
	view         int

	procCursor int
	cwdMove    bool
	cwdCursor  int
	cwdFor     string

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
)

type listLine struct {
	kind   lineKind
	title  string
	meta   string
	folded bool
	peek   string
	agent  *fleet.Agent
}

func sectionKey(title string) string { return "§" + title }

func New(store *state.Store, version string) *Model {
	dir, _ := os.Getwd()
	m := &Model{
		store: store, loader: fleet.NewLoader(store), scanner: fleet.NewScanner(),
		launchDir: dir, version: version, previews: map[string]previewEntry{},
		lastState: map[string]string{}, cwdMove: true,
		hibernated: map[string]bool{}, usageWait: map[string]time.Time{},
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
type usageMsg struct {
	dir string
	u   claude.Usage
}
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

func (m *Model) Init() tea.Cmd { return tea.Batch(tick(), m.scan(), m.fetchUsage()) }

// fetchUsage refreshes every account's plan usage from Anthropic, skipping
// accounts that were rate-limited until they may ask again.
func (m *Model) fetchUsage() tea.Cmd {
	var cmds []tea.Cmd
	for _, acct := range m.store.Config.AllAccounts() {
		if time.Now().Before(m.usageWait[acct.ConfigDir]) {
			continue
		}
		acct := acct
		cmds = append(cmds, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			u, err := claude.FetchUsage(ctx, acct)
			if err != nil {
				u = claude.Usage{Problem: err.Error()}
				var rl *claude.ErrRateLimited
				if errors.As(err, &rl) {
					u.Problem = "rate-limited until " + rl.Until.Local().Format("15:04")
				}
			}
			return usageMsg{dir: acct.ConfigDir, u: u}
		})
	}
	return tea.Batch(cmds...)
}

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

func (m *Model) startDir() string { return pickDir(m.startDirs(), m.dirIdx) }

func pickDir(dirs []string, i int) string {
	if len(dirs) == 0 {
		return ""
	}
	return dirs[((i%len(dirs))+len(dirs))%len(dirs)]
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

func (m *Model) rowAt(x, y int) string {
	i := y - m.listTop
	if m.mode != modeList || m.dialog != nil || m.picker != nil || x >= m.listW || i < 0 || i >= len(m.rowKeys) {
		return ""
	}
	return m.rowKeys[i]
}

func (m *Model) mouseMove(x, y int) tea.Cmd {
	k := m.rowAt(x, y)
	if k != m.hover {
		m.hover, m.hoverAt = k, time.Now()
		if k != "" && !strings.HasPrefix(k, "§") {
			return tea.Tick(400*time.Millisecond, func(time.Time) tea.Msg { return hoverMsg{} })
		}
	}
	return nil
}

func (m *Model) mouseClick(x, y int) tea.Cmd {
	if y == m.listTop-1 && x < m.listW && m.mode == modeList && m.dialog == nil {
		if col := m.headerColumn(x); col != "" {
			m.setSort(col)
		}
		return nil
	}
	k := m.rowAt(x, y)
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
	var copyCmd tea.Cmd
	if m.pendingCopy != "" {
		copyCmd, m.pendingCopy = tea.SetClipboard(m.pendingCopy), ""
	}
	return m, tea.Batch(cmd, copyCmd, m.syncLive(), m.syncHost(), m.syncWatch())
}

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case liveOpenMsg:
		return m, m.onLiveOpen(msg)
	case liveMsg:
		return m, m.onLive(msg)
	case hostOpenMsg:
		return m, m.onHostOpen(msg)
	case hostLinesMsg:
		return m, m.onHostLines(msg)
	case growMsg:
		m.onGrow(msg)
		return m, nil
	case movedToAgtopMsg:
		// The old row is finished; the conversation carries on in agtop mode.
		m.store.Overlay.Done[msg.from] = time.Now()
		if n := m.store.Overlay.Names[msg.from]; n != "" {
			m.store.Overlay.Names[state.Key(msg.started.acct, "a:"+msg.started.id)] = n
		}
		_ = m.store.SaveOverlay()
		if a := m.agentByKey(msg.from); a != nil && a.Interactive {
			defer m.flash(a.DisplayName+" carries on in agtop mode · its terminal copy is still open there, now under Done", false)
		}
		return m.update(msg.started)
	case hostStartedMsg:
		// Select the new session and give it the keys.
		m.refresh()
		m.sel = state.Key(msg.acct, "a:"+msg.id)
		m.rebuild()
		m.preview, m.paneFocus = true, true
		m.flash("started "+msg.name, false)
		return m, nil
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return m, nil
	case tickMsg:
		m.tick++
		m.refresh()
		m.zenPick()
		m.followTail()
		m.refreshSubs()
		cmds := []tea.Cmd{tick(), m.flushLocalQueues(), m.movePending()}
		if m.tick%3 == 0 {
			cmds = append(cmds, m.scan())
		}
		if m.tick%300 == 0 {
			cmds = append(cmds, m.fetchUsage())
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
	case usageMsg:
		if strings.HasPrefix(msg.u.Problem, "rate-limited") {
			m.usageWait[msg.dir] = time.Now().Add(15 * time.Minute)
		}
		m.loader.SetFetched(msg.dir, msg.u)
		m.refresh()
		return m, nil
	case hoverMsg:
		return m, m.loadPreview()
	case previewMsg:
		m.previews[msg.key] = msg.e
		return m, nil
	case tea.FocusMsg:
		m.blurred = false
		return m, nil
	case tea.BlurMsg:
		m.blurred = true
		return m, nil
	case editedMsg:
		switch {
		case msg.err != nil:
			m.flash("editor: "+msg.err.Error(), true)
		case msg.pane && m.host != nil:
			c := m.host
			c.input, c.back = applyEdit(&c.pastes, c.input, msg.id, msg.text), 0
		case !msg.pane:
			m.input, m.back = applyEdit(&m.pastes, m.input, msg.id, msg.text), 0
		}
		return m, nil
	case localQueueFailed:
		// Back on the front of the queue, to try again when you say.
		if q := m.localQ[msg.key]; q != nil {
			q.items, q.held = append(msg.items, q.items...), true
		}
		m.flash("couldn't send the queue: "+msg.err.Error()+" · held; alt+h releases it", true)
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
		if !m.embedded {
			msg.Content = cleanPaste(msg.Content)
		}
		if m.embedded {
			m.embedPaste(msg.Content)
			return m, nil
		}
		// Image files dropped onto the terminal arrive as a paste of their
		// paths; they become attachments on whichever box has focus.
		if rest, imgs := extractImages(msg.Content); imgs != nil && m.dialog == nil {
			if c := m.host; c != nil && m.paneFocus {
				c.images = append(c.images, imgs...)
			} else {
				m.images = append(m.images, imgs...)
			}
			m.flash(fmt.Sprintf("attached %d image(s)", len(imgs)), false)
			if rest == "" {
				return m, nil
			}
			msg.Content = rest // the words around them go in as text
		}
		// A paste goes into whichever box has focus, at its cursor, newlines
		// kept so a pasted log or snippet arrives whole.
		// A long one shows as a chip and goes out whole.
		if c := m.host; c != nil && m.paneFocus {
			text := msg.Content
			if isLongPaste(text) {
				text = c.pastes.add(text)
			}
			pos := max(0, len(c.input)-c.back)
			c.input = insert(c.input, pos, []rune(text))
			return m, nil
		}
		if m.acceptsText() {
			if m.dialog != nil {
				m.dialog.input = append(m.dialog.input, []rune(oneLine(msg.Content))...)
			} else {
				text := oneLine(msg.Content)
				if isLongPaste(msg.Content) {
					text = m.pastes.add(msg.Content)
				}
				m.input = insert(m.input, m.cursorPos(), []rune(text))
			}
		}
		return m, nil
	case tea.KeyPressMsg:
		return m, m.key(msg)
	case tea.MouseMotionMsg:
		if m.dragging {
			if msg.Button == tea.MouseLeft {
				m.setSideWidth(msg.X)
				return m, nil
			}
			m.dragging = false
			_ = m.store.SaveConfig()
		}
		on := m.listW > 0 && m.mode == modeList && (msg.X == m.listW || msg.X == m.listW+1)
		var shape tea.Cmd
		if on != m.divHover {
			// OSC 22 sets the pointer's shape in terminals that support it
			// (kitty, ghostty, wezterm, foot); others ignore it.
			shape = tea.Raw("\x1b]22;default\x1b\\")
			if on {
				shape = tea.Raw("\x1b]22;ew-resize\x1b\\")
			}
		}
		hover := m.hover
		changed := on != m.divHover
		m.divHover = on
		cmd := m.mouseMove(msg.X, msg.Y)
		if !changed && m.hover == hover {
			m.sameFrame = true // nothing moved that shows: keep the last frame
		}
		return m, tea.Batch(shape, cmd)
	case tea.MouseReleaseMsg:
		if m.dragging {
			m.dragging = false
			_ = m.store.SaveConfig()
		}
		return m, nil
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft && m.clickBox(msg.X, msg.Y) {
			return m, nil
		}
		// Grabbing the edge between list and pane resizes the list.
		if msg.Button == tea.MouseLeft && m.listW > 0 && m.mode == modeList && (msg.X == m.listW || msg.X == m.listW+1) {
			m.dragging = true
			return m, nil
		}
		if msg.Button == tea.MouseLeft && m.host != nil && (m.listW == 0 || msg.X > m.listW+1) && m.mode == modeList && m.dialog == nil {
			m.paneFocus = true // clicking the Session gives it the keys
			if m.viewName(m.host) == "screen" && m.canEmbed() {
				m.embedded = true
			}
			m.clickRow(m.host, msg.Y)
			return m, nil
		}
		if msg.Button == tea.MouseLeft {
			m.paneFocus, m.embedded = false, false // clicking Agents takes the keys back
			return m, m.mouseClick(msg.X, msg.Y)
		}
	case tea.MouseWheelMsg:
		// The wheel scrolls whatever is under the pointer: over the pane it
		// scrolls the conversation, and never moves the list behind it.
		if _, paneW, _ := m.layout(); m.mode == modeList && m.dialog == nil && paneW > 0 && (m.listW == 0 || msg.X > m.listW) {
			if c := m.host; c != nil {
				switch msg.Button {
				case tea.MouseWheelUp:
					c.scroll += 3
				case tea.MouseWheelDown:
					c.scroll = max(0, c.scroll-3)
				}
			}
			return m, nil
		}
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

var viewNames = []string{"Agents", "Zen", "Processes", "Accounts", "Coding agents", "Settings", "Claude"}

// setView switches the whole screen; tab and shift+tab cycle through them.
func (m *Model) setView(v int) {
	m.view = (v + len(viewNames)) % len(viewNames)
	m.dialog, m.mode, m.picker = nil, modeList, nil
	m.input, m.inKind = m.input[:0], inPrompt
	m.zen = false
	switch m.view {
	case 1:
		m.zen = true
		// Straight to the oldest agent waiting, whatever was selected.
		if q := m.zenQueue(); len(q) > 0 {
			m.sel, m.paneFocus = q[0].Key, true
		}
	case 2:
		m.mode, m.procCursor = modeProcs, 0
	case 3, 4, 5, 6:
		m.openDialog(m.view - 3)
	}
}

// wide is when the preview gets its own half of the screen.
func (m *Model) wide() bool { return m.w >= 170 }

func (m *Model) acceptsText() bool {
	return m.confirm == nil && (m.dialog == nil || m.dialog.asking != "") && (m.mode == modeList || m.mode == modeCwd)
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
		// Not for the agent you're looking at while agtop has focus.
		watching := !m.blurred && m.paneFocus && m.host != nil && m.host.key == a.Key
		if !first && !m.store.Config.Quiet && prev != "" && prev != "blocked" && a.NeedsYou() && !watching {
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
			go actions.Stop(a.Acct, a.ID, a.PID)
		}
	}
}

func (m *Model) agentByKey(key string) *fleet.Agent {
	for _, a := range m.snap.Agents {
		if a.Key == key {
			return a
		}
	}
	return nil
}

func (m *Model) selected() *fleet.Agent {
	for _, a := range m.order {
		if a.Key == m.sel {
			return a
		}
	}
	return nil
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
			add("Idle", 5, a)
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
		sort.SliceStable(g.agents, func(i, j int) bool { return m.sortLess(g.agents[i], g.agents[j]) })
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
		if m.inKind == inReply && m.sel != "" {
			m.inKind = inPrompt
			m.flash("the agent you were replying to has gone", true)
		}
		m.sel = ""
		for _, k := range m.items() {
			if !strings.HasPrefix(k, "§") {
				m.sel = k
				break
			}
		}
		if items := m.items(); m.sel == "" && len(items) > 0 {
			m.sel = items[0]
		}
	}
}

var sortModes = []string{"name", "recent", "cost", "cpu", "ram", "time"}

// sortLess orders rows inside a section. By name a row keeps its place while
// its agent works; the number columns sort biggest first.
func (m *Model) sortLess(a, b *fleet.Agent) bool {
	now := m.snap.At
	byName := func() bool {
		x, y := strings.ToLower(a.DisplayName), strings.ToLower(b.DisplayName)
		if x != y {
			return x < y
		}
		return a.Key < b.Key
	}
	var x, y float64
	switch m.store.Config.SortBy {
	case "recent":
		x, y = float64(-a.Age(now)), float64(-b.Age(now))
	case "cost":
		x, y = a.Spend.Cost, b.Spend.Cost
	case "cpu":
		x, y = a.CPU, b.CPU
	case "ram":
		x, y = float64(a.Mem), float64(b.Mem)
	case "time":
		x, y = float64(a.Elapsed(now)), float64(b.Elapsed(now))
	default:
		return byName()
	}
	if x != y {
		return x > y
	}
	return byName()
}

func (m *Model) setSort(mode string) {
	m.store.Config.SortBy = mode
	_ = m.store.SaveConfig()
	m.rebuild()
	m.flash("sorted by "+mode, false)
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
