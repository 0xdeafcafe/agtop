package ui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/cellw"
	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/0xdeafcafe/agtop/internal/fleet"
)

// The command bar (ctrl+k, or cmd+k where the terminal passes it on) goes
// anywhere from anywhere: a place or a page, an agent, a Session's view, a
// turn of the conversation open, or a match in any agent's transcript.
// What's typed is matched loosely against names; words also search the
// open conversation and, after a pause, every transcript in the background.
//
// ctrl+f is the same bar found where you are: in a Session it finds in that
// chat, in the list in the selected agent's group, in Machine or Settings
// among that place's pages. Each ctrl+f widens it (the chat, its group, all
// agents, everywhere, then round again), as does backspace with nothing
// typed; ctrl+k goes straight to everywhere.

// barScope is how far the bar looks.
type barScope struct {
	kind  string // "chat", "group", "agents", "place"; "" is everywhere
	key   string // the chat's agent
	group string // the list section
	place int    // the place, for "place"
	label string
}

// transcripts reports whether the scope searches agents' transcripts.
func (sc barScope) transcripts() bool {
	return sc.kind == "" || sc.kind == "group" || sc.kind == "agents"
}

// barScopes are the scopes ctrl+f steps through from here, narrowest first;
// the last is always everywhere.
func (m *Model) barScopes() []barScope {
	var out []barScope
	if m.view != placeAgents {
		out = append(out, barScope{kind: "place", place: m.view, label: viewNames[m.view%len(viewNames)]})
	} else {
		if c := m.host; c != nil && (m.paneFocus || m.zen) {
			out = append(out, barScope{kind: "chat", key: c.key, label: "this chat"})
		}
		group, ok := strings.CutPrefix(m.sel, "§")
		if !ok {
			group = m.groupOf[m.sel]
		}
		if group != "" {
			out = append(out, barScope{kind: "group", group: group, label: group})
		}
		out = append(out, barScope{kind: "agents", label: "all agents"})
	}
	return append(out, barScope{label: "everywhere"})
}

type barItem struct {
	section string
	glyph   string // painted
	title   string // plain; lit where the query matched
	lit     []int  // rune indexes of title to light
	meta    string // plain, dim at the right
	score   int
	run     func(m *Model) tea.Cmd
}

type cmdBar struct {
	scopes []barScope
	at     int // the scope in use
	query  []rune
	pos    int
	cursor int
	top    int // first row of the results on screen
	items  []barItem

	// Transcripts, searched in the background for the query's words.
	gen      int
	search   *barSearch
	found    []barFound
	searched int
	toSearch int
	done     bool
	glint    int // where the frame's glint is while searching
}

// spot is somewhere the bar jumped from, for "Back".
type spot struct {
	view, effPage, machinePage, settingsPage int
	zen                                      bool
	key, name                                string
	paneView                                 int
	ref                                      string
}

// barJump is a jump into an agent's conversation waiting for it to open:
// the hit is found again in the session as the pane reads it, by its words
// and snippet, since an agtop session's turns count from its host's replay.
type barJump struct {
	key, name   string
	ref         string
	query, snip string
	at          time.Time
}

func (b *cmdBar) scope() barScope { return b.scopes[b.at] }

// barToggle opens the bar (ctrl+k everywhere, ctrl+f where you are) and,
// while it's open, changes its scope: ctrl+f widens it, ctrl+k goes to
// everywhere and, there already, closes it. ctrl+k is also "cut to the end
// of the line", which it stays while there's something after the cursor to
// cut; on a Claude Code agent's own screen both keys are Claude Code's.
func (m *Model) barToggle(s string) (tea.Cmd, bool) {
	if s != "super+k" && s != "ctrl+k" && s != "ctrl+f" {
		return nil, false
	}
	if b := m.bar; b != nil {
		last := len(b.scopes) - 1
		switch {
		case s == "ctrl+f":
			b.at = (b.at + 1) % len(b.scopes)
		case b.at != last:
			b.at = last
		default:
			m.closeBar()
			return nil, true
		}
		return m.barChanged(), true
	}
	if m.confirm != nil || m.dialog != nil && m.dialog.asking != "" || m.embedded && s != "super+k" {
		return nil, false
	}
	if s == "ctrl+k" && m.canCut() {
		return nil, false
	}
	m.openBar(s == "ctrl+f")
	return nil, true
}

// openBar opens the bar where you are (here) or everywhere.
func (m *Model) openBar(here bool) {
	m.bar = &cmdBar{scopes: m.barScopes()}
	if !here {
		m.bar.at = len(m.bar.scopes) - 1
	}
	m.embedded = false
	m.barRebuild()
}

// canCut reports whether ctrl+k would cut text in the box that has the keys.
func (m *Model) canCut() bool {
	if c := m.host; m.paneFocus && c != nil && m.mode == modeList && m.dialog == nil {
		pos := len(c.input) - c.back
		return lineEnd(c.input, pos) > pos
	}
	if m.acceptsText() {
		pos := len(m.input) - m.back
		return lineEnd(m.input, pos) > pos
	}
	return false
}

func (m *Model) closeBar() {
	if m.bar != nil && m.bar.search != nil {
		m.bar.search.cancel()
	}
	m.bar = nil
}

func (m *Model) barKey(k tea.KeyPressMsg, s string) tea.Cmd {
	b := m.bar
	switch s {
	case "esc", "ctrl+c":
		m.closeBar()
		return nil
	case "up", "ctrl+p":
		b.cursor = roundMove(b.cursor, -1, len(b.items))
		return nil
	case "down", "ctrl+n":
		b.cursor = roundMove(b.cursor, 1, len(b.items))
		return nil
	case "pgup":
		b.cursor = max(0, b.cursor-10)
		return nil
	case "pgdown":
		b.cursor = max(0, min(len(b.items)-1, b.cursor+10))
		return nil
	case "tab", "shift+tab":
		b.cursor = b.nextSection(map[string]int{"tab": 1, "shift+tab": -1}[s])
		return nil
	case "enter":
		if b.cursor >= len(b.items) {
			return nil
		}
		it := b.items[b.cursor]
		from := m.here()
		m.closeBar()
		cmd := it.run(m)
		if it.section != "" && from != nil && it.title != "Back" {
			m.barBack = from
		}
		return tea.Batch(cmd, m.loadPreview())
	}
	// Backspace with nothing typed widens the scope, as ctrl+f does.
	if (s == "backspace" || s == "ctrl+h") && len(b.query) == 0 && b.at < len(b.scopes)-1 {
		b.at++
		return m.barChanged()
	}
	buf, pos, ok := edit(b.query, b.pos, k, s)
	if !ok || strings.Contains(string(buf), "\n") || string(buf) == string(b.query) {
		if ok {
			b.pos = pos
		}
		return nil
	}
	b.query, b.pos = buf, pos
	return m.barChanged()
}

// nextSection is the first row of the next (or previous) section, round.
func (b *cmdBar) nextSection(d int) int {
	if len(b.items) == 0 {
		return 0
	}
	var starts []int
	for i, it := range b.items {
		if i == 0 || it.section != b.items[i-1].section {
			starts = append(starts, i)
		}
	}
	cur := 0
	for j, st := range starts {
		if st <= b.cursor {
			cur = j
		}
	}
	return starts[(cur+d+len(starts))%len(starts)]
}

// barChanged rebuilds the rows for a new query and, once typing pauses,
// searches the transcripts for its words.
func (m *Model) barChanged() tea.Cmd {
	b := m.bar
	b.cursor, b.top = 0, 0
	if b.search != nil {
		b.search.cancel()
		b.search = nil
	}
	b.gen++
	b.found, b.searched, b.toSearch, b.done = nil, 0, 0, false
	m.barRebuild()
	if !searchable(string(b.query)) || !b.scope().transcripts() {
		return nil
	}
	gen := b.gen
	return tea.Tick(200*time.Millisecond, func(time.Time) tea.Msg { return barPauseMsg(gen) })
}

// searchable is a query with enough to it to be worth reading every
// transcript for.
func searchable(q string) bool {
	n := 0
	for _, w := range convo.Words(stripIn(q)) {
		n += len(w)
	}
	return n >= 3
}

type barPauseMsg int

type barFoundMsg struct {
	gen   int
	found []barFound
	done  bool
}

// barFound is one agent's matches.
type barFound struct {
	key, name string
	rank      int // the agent's place in the search order: most recent first
	hits      []convo.Hit
	total     int
}

// barMsg handles the bar's own messages; ok is false for anything else.
func (m *Model) barMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		if m.bar == nil {
			return nil, false
		}
		m.closeBar() // a click anywhere puts it away
		return nil, true
	case tea.MouseWheelMsg:
		if b := m.bar; b != nil && len(b.items) > 0 {
			d := map[tea.MouseButton]int{tea.MouseWheelUp: -3, tea.MouseWheelDown: 3}[msg.Button]
			b.cursor = max(0, min(len(b.items)-1, b.cursor+d))
		}
		return nil, m.bar != nil
	case tea.MouseMotionMsg, tea.MouseReleaseMsg:
		return nil, m.bar != nil
	case barPauseMsg:
		if b := m.bar; b != nil && int(msg) == b.gen && b.search == nil {
			return tea.Batch(m.searchTranscripts(), glintTick(b.gen)), true
		}
		return nil, true
	case barGlintMsg:
		if b := m.bar; b != nil && int(msg) == b.gen && b.search != nil && !b.done {
			b.glint += 3
			return glintTick(b.gen), true
		}
		return nil, true
	case barFoundMsg:
		b := m.bar
		if b == nil || msg.gen != b.gen || b.search == nil {
			return nil, true
		}
		for _, f := range msg.found {
			b.searched++
			if f.total > 0 {
				b.found = append(b.found, f)
			}
		}
		sort.Slice(b.found, func(i, j int) bool { return b.found[i].rank < b.found[j].rank })
		b.done = msg.done
		// Keep the cursor on the row it was on as rows arrive above it.
		var keep *barItem
		if b.cursor < len(b.items) {
			keep = &b.items[b.cursor]
		}
		title, section := "", ""
		if keep != nil {
			title, section = keep.title+keep.meta, keep.section
		}
		m.barRebuild()
		for i, it := range b.items {
			if it.section == section && it.title+it.meta == title {
				b.cursor = i
				break
			}
		}
		if msg.done {
			return nil, true
		}
		return b.search.wait(), true
	}
	return nil, false
}

// barSearch reads transcripts on a few goroutines and hands back what they
// find in batches, so a thousand files cost a handful of redraws.
type barSearch struct {
	gen  int
	ch   chan barFound
	stop chan struct{}
	once sync.Once
}

func (s *barSearch) cancel() { s.once.Do(func() { close(s.stop) }) }

func (s *barSearch) wait() tea.Cmd {
	return func() tea.Msg {
		f, ok := <-s.ch
		if !ok {
			return barFoundMsg{gen: s.gen, done: true}
		}
		batch := []barFound{f}
		soon := time.After(60 * time.Millisecond)
		for {
			select {
			case f, ok := <-s.ch:
				if !ok {
					return barFoundMsg{gen: s.gen, found: batch, done: true}
				}
				batch = append(batch, f)
			case <-soon:
				return barFoundMsg{gen: s.gen, found: batch}
			}
		}
	}
}

// searchTranscripts starts reading every agent's transcript for the query,
// the most recently active first, leaving out the one open in the pane (its
// matches are already under This session). in:<name> narrows the agents.
func (m *Model) searchTranscripts() tea.Cmd {
	b := m.bar
	q, in := splitIn(string(b.query))
	type job struct {
		rank      int
		key, name string
		path      string
	}
	sc := b.scope()
	var agents []*fleet.Agent
	for _, a := range m.snap.Agents {
		// Everywhere, the open chat's matches are under This session.
		if a.TranscriptPath == "" || sc.kind == "" && m.host != nil && a.Key == m.host.key {
			continue
		}
		if sc.kind == "group" && m.groupOf[a.Key] != sc.group {
			continue
		}
		if in != "" && !strings.Contains(strings.ToLower(a.DisplayName), in) {
			continue
		}
		agents = append(agents, a)
	}
	now := m.snap.At
	sort.SliceStable(agents, func(i, j int) bool { return agents[i].Age(now) < agents[j].Age(now) })
	jobs := make([]job, len(agents))
	for i, a := range agents {
		jobs[i] = job{i, a.Key, oneLine(a.DisplayName), a.TranscriptPath}
	}
	s := &barSearch{gen: b.gen, ch: make(chan barFound, 16), stop: make(chan struct{})}
	b.search, b.toSearch = s, len(jobs)
	if len(jobs) == 0 {
		b.done = true
		return nil
	}
	feed := make(chan job)
	go func() {
		var wg sync.WaitGroup
		for range min(4, len(jobs)) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := range feed {
					hits, total, _ := convo.SearchFile(j.path, q, 3)
					select {
					case s.ch <- barFound{key: j.key, name: j.name, rank: j.rank, hits: hits, total: total}:
					case <-s.stop:
						return
					}
				}
			}()
		}
	send:
		for _, j := range jobs {
			select {
			case feed <- j:
			case <-s.stop:
				break send
			}
		}
		close(feed)
		wg.Wait()
		close(s.ch)
	}()
	return s.wait()
}

// splitIn takes in:<name> out of a query.
func splitIn(q string) (rest, in string) {
	var keep []string
	for _, f := range strings.Fields(q) {
		if v, ok := strings.CutPrefix(strings.ToLower(f), "in:"); ok {
			in = v
			continue
		}
		keep = append(keep, f)
	}
	return strings.Join(keep, " "), in
}

func stripIn(q string) string { r, _ := splitIn(q); return r }

// barRebuild lists what the query matches, section by section.
func (m *Model) barRebuild() {
	b := m.bar
	raw := strings.TrimSpace(string(b.query))
	q, in := splitIn(raw)
	words := convo.Words(q)
	// Names are matched by the words alone: is:, file: and turn: only
	// narrow conversations.
	name := strings.Join(words, " ")
	filtered := convo.Filtered(q) || in != ""
	var out []barItem
	add := func(section string, items []barItem, limit int) {
		if name != "" {
			sort.SliceStable(items, func(i, j int) bool { return items[i].score > items[j].score })
		}
		for i := range items {
			items[i].section = section
		}
		out = append(out, items[:min(limit, len(items))]...)
	}
	switch sc := b.scope(); sc.kind {
	case "chat":
		if c := m.host; c != nil && c.key == sc.key {
			add("This chat · "+oneLine(m.hostName(c)), m.barTurns(c, q), 300)
		}
	case "group", "agents":
		if !filtered {
			title := "Agents"
			if sc.kind == "group" {
				title = sc.group
			}
			add(title, m.barAgents(name, sc.group), 40)
		}
		add("Transcripts", m.barTranscripts(words), 60)
	case "place":
		var pages []barItem
		for _, it := range m.barPlaces(name) {
			if strings.HasPrefix(it.title, sc.label+" › ") {
				pages = append(pages, it)
			}
		}
		add(sc.label, pages, 20)
	default:
		if !filtered {
			// Untyped, a few of each: the rest are a letter or two away.
			add("Go to", m.barPlaces(name), 6)
			add("Agents", m.barAgents(name, ""), map[bool]int{true: 5, false: 8}[name == ""])
		}
		if c := m.host; c != nil && in == "" {
			add("This session · "+oneLine(m.hostName(c)), m.barTurns(c, q), 50)
		}
		// Anything else typed can be a new agent's task (#12 is a turn).
		if raw != "" && !filtered && !strings.HasPrefix(raw, "#") {
			add("Start", []barItem{m.barStart(raw)}, 1)
		}
		add("Transcripts", m.barTranscripts(words), 60)
	}
	b.items = out
	b.cursor = min(b.cursor, max(0, len(out)-1))
}

func (m *Model) hostName(c *hostConn) string {
	if a := m.agentByKey(c.key); a != nil {
		return a.DisplayName
	}
	return "this agent"
}

// matchItem scores an item against the words typed; one that doesn't match
// is left out. Matches in the title are lit; keywords only count.
func matchItem(it barItem, q, keywords string) (barItem, bool) {
	if q == "" {
		return it, true
	}
	if s, at, ok := fuzzy(q, it.title); ok {
		it.score, it.lit = s, at
		return it, true
	}
	if s, _, ok := fuzzy(q, it.title+" "+keywords); ok {
		it.score = s - 20
		return it, true
	}
	return it, false
}

// barPlaces are the places, their pages, the open Session's views and a
// few things to do.
func (m *Model) barPlaces(q string) []barItem {
	var items []barItem
	add := func(glyph, title, meta, keywords string, run func(m *Model) tea.Cmd) {
		if it, ok := matchItem(barItem{glyph: glyph, title: title, meta: meta, run: run}, q, keywords); ok {
			items = append(items, it)
		}
	}
	if bk := m.barBack; bk != nil {
		where := bk.where()
		add(paint(cOrange, "↩"), "Back", where, "previous return "+where, func(m *Model) tea.Cmd { return m.goSpot(bk) })
	}
	add(paint(cGreen, "+"), "Start an agent", "in "+tildify(m.startDir()), "new session spawn prompt task", func(m *Model) tea.Cmd {
		m.toPrompt()
		return nil
	})
	add(paint(cSub, "◇"), "Agents", "the list", "home list", func(m *Model) tea.Cmd {
		m.goView(placeAgents)
		m.setZen(false)
		return nil
	})
	zen := "Zen"
	if m.zen {
		zen = "Leave Zen"
	}
	add(paint(cSub, "◇"), zen, "only the agent that needs you", "focus zen", func(m *Model) tea.Cmd {
		m.setZen(!m.zen)
		return nil
	})
	for i, p := range effPages {
		add(paint(cSub, "◇"), "Efficiency › "+p, "", "efficiency tokens savers usage cost "+p, func(m *Model) tea.Cmd {
			m.goView(placeEff)
			m.setEffPage(i)
			return nil
		})
	}
	for i, p := range machinePages {
		add(paint(cSub, "◇"), "Machine › "+p, "", "machine "+p, func(m *Model) tea.Cmd {
			m.goView(placeMachine)
			m.setMachinePage(i)
			return nil
		})
	}
	for i, p := range tabNames {
		add(paint(cSub, "◇"), "Settings › "+p, "", "settings preferences "+p, func(m *Model) tea.Cmd {
			m.goView(placeSettings)
			m.setSettingsPage(i)
			return nil
		})
	}
	if c := m.host; c != nil {
		for i, v := range m.views(c) {
			title := "Session › " + strings.ToUpper(v[:1]) + v[1:]
			add(paint(cBlue, "▤"), title, oneLine(m.hostName(c)), "session view "+v, func(m *Model) tea.Cmd {
				m.goView(placeAgents)
				if c := m.host; c != nil {
					c.view = i
				}
				m.preview, m.paneFocus = true, true
				return nil
			})
		}
		add(paint(cBlue, "⌕"), "Find in this chat", "ctrl+f", "search find session", func(m *Model) tea.Cmd {
			m.goView(placeAgents)
			m.preview, m.paneFocus = true, true
			m.openBar(true)
			return nil
		})
	}
	if a := m.focused(); a != nil && !a.Agtop && !a.Interactive && a.PID != 0 {
		add(paint(cSub, "◇"), "Open full screen", oneLine(a.DisplayName)+" in Claude Code", "attach claude code full screen native", func(m *Model) tea.Cmd {
			return m.attach(a)
		})
	}
	add(paint(cSub, "◇"), "Folder for new sessions", tildify(m.startDir()), "start dir cwd", func(m *Model) tea.Cmd {
		m.goView(placeAgents)
		m.openDirPicker()
		return nil
	})
	add(paint(cSub, "◇"), "All keys", "?", "help shortcuts keys", func(m *Model) tea.Cmd {
		m.goView(placeAgents)
		m.mode = modeHelp
		return nil
	})
	return items
}

// barStart starts a new agent with what's typed as its task, the way the
// Prompt does.
func (m *Model) barStart(task string) barItem {
	return barItem{glyph: paint(cGreen, "+"), title: task, meta: "new agent in " + tildify(m.startDir()), run: func(m *Model) tea.Cmd {
		m.toPrompt()
		m.input, m.back, m.pastes = []rune(task), 0, pastes{}
		return m.submit()
	}}
}

// toPrompt puts the keys in Agents' Prompt, where new sessions start.
func (m *Model) toPrompt() {
	m.goView(placeAgents)
	if m.zen {
		m.setZen(false)
	}
	m.paneFocus, m.embedded = false, false
	if m.inKind != inPrompt {
		m.input, m.inKind = m.input[:0], inPrompt
	}
}

// goView moves to a place unless it's already there: setView starts the
// place afresh, dropping what's typed in the Prompt.
func (m *Model) goView(v int) {
	if m.view != v || m.mode == modeHelp {
		m.setView(v)
	}
}

// barAgents are the agents, those needing you first, then the latest.
func (m *Model) barAgents(q, group string) []barItem {
	now := m.snap.At
	agents := append([]*fleet.Agent{}, m.snap.Agents...)
	sort.SliceStable(agents, func(i, j int) bool {
		a, b := agents[i], agents[j]
		if a.NeedsYou() != b.NeedsYou() {
			return a.NeedsYou()
		}
		return a.Age(now) < b.Age(now)
	})
	var items []barItem
	for _, a := range agents {
		if group != "" && m.groupOf[a.Key] != group {
			continue
		}
		where := strings.Trim(a.Repo+" · "+a.Branch, " ·")
		meta := where
		if s := agentState(a, now); s != "" {
			meta = strings.Trim(where+"  "+s, " ")
		}
		it := barItem{glyph: agentGlyph(a), title: oneLine(a.DisplayName), meta: meta, run: func(m *Model) tea.Cmd { return m.goAgent(a) }}
		if it, ok := matchItem(it, q, a.Repo+" "+a.Branch+" "+a.Group+" "+a.State); ok {
			items = append(items, it)
		}
	}
	return items
}

func agentGlyph(a *fleet.Agent) string {
	switch {
	case a.NeedsYou() || a.Waiting():
		return paint(cYellow, "●")
	case a.Live():
		return paint(cOrange, "●")
	case a.PID != 0:
		return paint(cSub, "○")
	}
	return faint("·")
}

func agentState(a *fleet.Agent, now time.Time) string {
	switch {
	case a.NeedsYou():
		return "needs you"
	case a.Waiting():
		return "waiting"
	case a.Live():
		return "working"
	case a.PID != 0:
		return "idle"
	}
	return age(a.Age(now)) + " ago"
}

// goAgent selects an agent and opens its Session.
func (m *Model) goAgent(a *fleet.Agent) tea.Cmd {
	m.goView(placeAgents)
	if m.zen {
		m.setZen(false)
	}
	m.sel = a.Key
	return m.focusPane(a)
}

// barTurns are the open conversation's turns, the latest first, or its
// matches for the query.
func (m *Model) barTurns(c *hostConn, q string) []barItem {
	var items []barItem
	// #12 is turn 12.
	if n, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(q), "#")); err == nil && strings.HasPrefix(strings.TrimSpace(q), "#") {
		for _, t := range c.sess.Turns {
			if t.N == n {
				ref := fmt.Sprintf("t%d", n)
				items = append(items, barItem{glyph: paint(cSub, "›"), title: oneLine(t.Prompt), meta: fmt.Sprintf("#%d", n), score: 1000, run: func(m *Model) tea.Cmd {
					m.jumpInPane(ref)
					return nil
				}})
			}
		}
		return items
	}
	if len(convo.Words(q)) == 0 && !convo.Filtered(q) {
		for i := len(c.sess.Turns) - 1; i >= 0; i-- {
			t := c.sess.Turns[i]
			text := oneLine(t.Prompt)
			if text == "" {
				continue
			}
			meta := fmt.Sprintf("#%d", t.N)
			if !t.Start.IsZero() {
				meta = age(time.Since(t.Start)) + " ago  " + meta
			}
			ref := fmt.Sprintf("t%d", t.N)
			items = append(items, barItem{glyph: paint(cSub, "›"), title: text, meta: meta, run: func(m *Model) tea.Cmd {
				m.jumpInPane(ref)
				return nil
			}})
		}
		return items
	}
	words := convo.Words(q)
	for _, h := range c.sess.Search(q) {
		ref := h.Ref
		items = append(items, barItem{glyph: whoGlyph(h.Who), title: h.Snippet, lit: litWords(h.Snippet, words), meta: fmt.Sprintf("#%d %s", h.Turn, h.Who), run: func(m *Model) tea.Cmd {
			m.jumpInPane(ref)
			return nil
		}})
	}
	return items
}

func whoGlyph(who string) string {
	switch who {
	case "you":
		return paint(cText, "›")
	case "claude":
		return paint(cOrange, "✻")
	}
	return paint(cSub, "·")
}

// barTranscripts are the matches found in other agents' transcripts so far.
func (m *Model) barTranscripts(words []string) []barItem {
	q := stripIn(string(m.bar.query))
	var items []barItem
	for _, f := range m.bar.found {
		for _, h := range f.hits {
			f, h := f, h
			meta := fmt.Sprintf("%s  #%d", fit(f.name, 24), h.Turn)
			items = append(items, barItem{glyph: whoGlyph(h.Who), title: h.Snippet, lit: litWords(h.Snippet, words), meta: strings.TrimRight(meta, " "), run: func(m *Model) tea.Cmd {
				return m.goFound(f, h, q)
			}})
		}
	}
	return items
}

// goFound opens the agent a transcript match is in and, once its pane has
// the conversation, opens the match there.
func (m *Model) goFound(f barFound, h convo.Hit, q string) tea.Cmd {
	a := m.agentByKey(f.key)
	if a == nil {
		m.flash(f.name+" isn't in the list any more", true)
		return nil
	}
	m.jump = &barJump{key: f.key, name: f.name, ref: h.Ref, query: q, snip: h.Snippet, at: time.Now()}
	cmd := m.goAgent(a)
	m.applyJump()
	return cmd
}

// jumpInPane opens a turn or step of the pane's conversation and selects it.
func (m *Model) jumpInPane(ref string) {
	c := m.host
	if c == nil {
		return
	}
	m.goView(placeAgents)
	turn, _, isStep := strings.Cut(ref, ":")
	c.open[turn] = true
	if isStep {
		c.open[ref] = true
		if p := c.sess.ParentRef(ref); p != "" {
			c.open[p] = true // a subagent's step shows under its opened parent
		}
	}
	c.sel = ref
	c.view, c.selMoved = 0, true
	m.preview, m.paneFocus = true, true
}

// applyJump finishes a jump into a conversation once it's open: the match
// is looked for again as the pane has it, until it's found or a few seconds
// have gone by.
func (m *Model) applyJump() {
	j := m.jump
	if j == nil {
		return
	}
	if time.Since(j.at) > 6*time.Second {
		m.jump = nil
		m.flash("couldn't find that in "+j.name+"'s conversation", true)
		return
	}
	c := m.host
	if c == nil || c.key != j.key || !c.ready {
		return
	}
	ref := ""
	if j.query == "" {
		turn, _, _ := strings.Cut(j.ref, ":")
		for _, t := range c.sess.Turns {
			if fmt.Sprintf("t%d", t.N) == turn {
				ref = j.ref
			}
		}
	} else {
		for _, h := range c.sess.Search(j.query) {
			if h.Snippet != j.snip {
				continue
			}
			if ref == "" || h.Ref == j.ref {
				ref = h.Ref
			}
		}
	}
	if ref == "" {
		return // the host may still be replaying
	}
	m.jump = nil
	m.jumpInPane(ref)
}

// here is where the screen is now, to come back to.
func (m *Model) here() *spot {
	s := &spot{view: m.view, effPage: m.eff.page, machinePage: m.machinePage, settingsPage: m.settingsPage, zen: m.zen, key: m.sel}
	if a := m.agentByKey(m.sel); a != nil {
		s.name = oneLine(a.DisplayName)
	}
	if c := m.host; c != nil && c.key == m.sel && m.paneFocus {
		s.paneView, s.ref = c.view, c.sel
	}
	return s
}

func (s *spot) where() string {
	switch s.view {
	case placeEff:
		return "Efficiency › " + effPages[s.effPage%len(effPages)]
	case placeMachine:
		return "Machine › " + machinePages[s.machinePage%len(machinePages)]
	case placeSettings:
		return "Settings › " + tabNames[s.settingsPage%len(tabNames)]
	}
	if s.zen {
		return "Zen"
	}
	if s.name == "" {
		return "Agents"
	}
	return s.name
}

// goSpot goes back to where the last jump came from, and makes where it
// went the new way back.
func (m *Model) goSpot(s *spot) tea.Cmd {
	m.barBack = m.here()
	switch s.view {
	case placeEff:
		m.goView(placeEff)
		m.setEffPage(s.effPage)
		return nil
	case placeMachine:
		m.goView(placeMachine)
		m.setMachinePage(s.machinePage)
		return nil
	case placeSettings:
		m.goView(placeSettings)
		m.setSettingsPage(s.settingsPage)
		return nil
	}
	m.goView(placeAgents)
	if s.zen != m.zen {
		m.setZen(s.zen)
	}
	a := m.agentByKey(s.key)
	if a == nil || s.zen {
		return nil
	}
	m.sel = a.Key
	if s.ref == "" {
		if c := m.host; c != nil && c.key == a.Key {
			c.view = s.paneView
		}
		return nil
	}
	m.jump = &barJump{key: a.Key, name: s.name, ref: s.ref, at: time.Now()}
	cmd := m.focusPane(a)
	m.applyJump()
	return cmd
}

// fuzzy scores how well q's letters appear, in order, in s: each word of
// q on its own, all of them needed. A run of letters and the start of a
// word score more; a gap scores less. at are the runes of s it matched.
func fuzzy(q, s string) (score int, at []int, ok bool) {
	hay := []rune(strings.ToLower(s))
	orig := []rune(s)
	for _, w := range strings.Fields(strings.ToLower(q)) {
		ws, wat, ok := fuzzyWord([]rune(w), hay, orig)
		if !ok {
			return 0, nil, false
		}
		score += ws
		at = append(at, wat...)
	}
	return score, at, true
}

func fuzzyWord(w, hay, orig []rune) (int, []int, bool) {
	// A whole substring beats letters spread out; the earliest one that
	// starts a word beats the earliest one.
	if i := bestSub(w, hay, orig); i >= 0 {
		at := make([]int, len(w))
		for k := range w {
			at[k] = i + k
		}
		score := 100 + 10*len(w) - min(i, 30)
		if wordStart(hay, orig, i) {
			score += 40
		}
		return score, at, true
	}
	var at []int
	score, k, last := 0, 0, -2
	for i := 0; i < len(hay) && k < len(w); i++ {
		if hay[i] != w[k] {
			continue
		}
		switch {
		case i == last+1:
			score += 6
		case wordStart(hay, orig, i):
			score += 8
		default:
			score -= min(i-last, 6)
		}
		at = append(at, i)
		last = i
		k++
	}
	if k < len(w) {
		return 0, nil, false
	}
	return score, at, true
}

func bestSub(w, hay, orig []rune) int {
	first := -1
	for i := 0; i+len(w) <= len(hay); i++ {
		if string(hay[i:i+len(w)]) != string(w) {
			continue
		}
		if wordStart(hay, orig, i) {
			return i
		}
		if first < 0 {
			first = i
		}
	}
	return first
}

func wordStart(hay, orig []rune, i int) bool {
	if i == 0 {
		return true
	}
	p := hay[i-1]
	if !unicode.IsLetter(p) && !unicode.IsDigit(p) {
		return true
	}
	return unicode.IsLower(orig[i-1]) && unicode.IsUpper(orig[i]) // camelCase
}

// litWords are the runes of s where the query's words are found.
func litWords(s string, words []string) []int {
	var at []int
	for _, w := range words {
		i, j := convo.FindFold(s, w)
		if i < 0 {
			continue
		}
		from := len([]rune(s[:i]))
		for k := range []rune(s[i:j]) {
			at = append(at, from+k)
		}
	}
	return at
}

// barBox draws the bar: the query, then the rows that fit, by section.
func (m *Model) barBox(w, h int) []string {
	b := m.bar
	inner := w - 4
	q := string(b.query)
	cur := paint(cOrange, "▏")
	field := paint(cText, string(b.query[:b.pos])) + cur + paint(cText, string(b.query[b.pos:]))
	sc := b.scope()
	if q == "" {
		field = cur + dim(barHolder(sc))
	}
	head := paint(cOrange+bold, "❯ ") + field
	if sc.kind != "" {
		head = barChip + paint(cOrange, " ⌕ ") + barChip + paint(cText+bold, sc.label+" ") + reset + " " + head
	}
	out := []string{head, faint(strings.Repeat("─", inner))}

	// Rows, with a heading where each section starts.
	type row struct {
		text string
		item int // -1 for a heading
	}
	var rows []row
	for i, it := range b.items {
		if i == 0 || it.section != b.items[i-1].section {
			if i > 0 {
				rows = append(rows, row{"", -1})
			}
			rows = append(rows, row{m.barHeading(it.section, inner), -1})
		}
		rows = append(rows, row{barRow(it, inner, i == b.cursor), i})
	}
	if len(b.items) == 0 {
		rows = append(rows, row{"", -1}, row{dim("  nothing matches"), -1})
		if b.search != nil && !b.done {
			rows[len(rows)-1] = row{dim("  searching transcripts…"), -1}
		}
	}
	avail := max(3, h-len(out)-2)
	sel := 0
	for i, r := range rows {
		if r.item == b.cursor {
			sel = i
		}
	}
	// Keep the selection on screen, with its heading when it's near the top.
	if sel < b.top+1 {
		b.top = max(0, sel-2)
	}
	if sel >= b.top+avail {
		b.top = sel - avail + 1
	}
	b.top = max(0, min(b.top, len(rows)-avail))
	for i := b.top; i < min(len(rows), b.top+avail); i++ {
		out = append(out, rows[i].text)
	}
	out = append(out, "", m.barHint(inner))
	return out
}

const barChip = "\x1b[48;2;64;45;37m"

// barHolder is what the empty box says it takes, in each scope.
func barHolder(sc barScope) string {
	switch sc.kind {
	case "chat":
		return "words in this chat · #12 for turn 12 · is:failed is:edit file:x turn:3-5"
	case "group":
		return "an agent in " + sc.group + ", or words to search their transcripts"
	case "agents":
		return "an agent, or words to search every agent's transcript"
	case "place":
		return "a page of " + sc.label
	}
	return "Go to a place, an agent or a turn · search every transcript"
}

// barHint is the row of keys under the results: it says what ctrl+f and
// ctrl+k will do from here, so the scopes teach themselves.
func (m *Model) barHint(w int) string {
	b := m.bar
	last := len(b.scopes) - 1
	pairs := []string{"↑↓", "choose", "enter", "go"}
	switch {
	case b.at < last-1:
		pairs = append(pairs, "ctrl+f", "wider: "+b.scopes[b.at+1].label, "ctrl+k", "everywhere")
	case b.at == last-1:
		pairs = append(pairs, "ctrl+f · ctrl+k", "everywhere")
	case last > 0:
		pairs = append(pairs, "ctrl+f", "just "+b.scopes[0].label)
	}
	if b.scope().kind == "" {
		pairs = append(pairs, "tab", "next section")
	}
	if b.scope().transcripts() {
		pairs = append(pairs, "in:name is:failed", "narrow")
	}
	return keysFit(w, append(pairs, "esc", "close")...)
}

// barStatus says how the transcript search is going.
func (m *Model) barStatus() string {
	b := m.bar
	switch {
	case b.search == nil && searchable(string(b.query)):
		return dim(" … ")
	case b.search == nil:
		return ""
	case !b.done:
		return paint(cSub, fmt.Sprintf(" searching %d/%d transcripts ", b.searched, b.toSearch))
	}
	n := 0
	for _, f := range b.found {
		n += f.total
	}
	return dim(fmt.Sprintf(" %d in %d of %d transcripts ", n, len(b.found), b.toSearch))
}

func (m *Model) barHeading(section string, w int) string {
	meta := ""
	if section == "Transcripts" {
		shown, total := 0, 0
		for _, f := range m.bar.found {
			shown += len(f.hits)
			total += f.total
		}
		if total > shown {
			meta = fmt.Sprintf("%d more; open one to see all", total-shown)
		}
	}
	return rule(section, meta, w)
}

// barRow is one result: its glyph, the title lit where it matched, and its
// meta at the right edge.
func barRow(it barItem, w int, sel bool) string {
	meta := dim(it.meta)
	mw := min(cellw.String(meta), w/2)
	if mw > 0 {
		meta = ansi.Truncate(meta, mw, "…")
	}
	tw := w - 4 - mw - 2
	title := litTitle(oneLine(it.title), it.lit, tw)
	line := " " + it.glyph + " " + title
	pad := w - cellw.String(line) - mw
	line += strings.Repeat(" ", max(1, pad)) + meta
	if sel {
		return highlight(paint(cOrange, "▍")+line[1:], w)
	}
	return line
}

// litTitle paints title, its matched runes bright, cut to w cells.
func litTitle(title string, lit []int, w int) string {
	if cellw.String(title) > w {
		title = ansi.Truncate(title, w, "…")
	}
	if len(lit) == 0 {
		return paint(cText, title)
	}
	on := map[int]bool{}
	for _, i := range lit {
		on[i] = true
	}
	var sb strings.Builder
	for i, r := range []rune(title) {
		if on[i] {
			sb.WriteString(paint(cOrange+bold, string(r)))
		} else {
			sb.WriteString(paint(cText, string(r)))
		}
	}
	return sb.String()
}

// overlayBar draws the bar over the screen, a little below the top, the way
// a command bar drops down rather than sitting in the middle: a frame that
// glows from orange to ember, its name set into the top edge and the
// transcript search's progress into the bottom one, a light running round it
// while the search is under way, and a shadow on the screen behind.
func (m *Model) overlayBar(base string) string {
	lines := strings.Split(base, "\n")
	bw := min(m.w-6, 110)
	bh := max(8, m.h*2/3)
	body := m.barBox(bw, bh-2)
	inner := bw - 4
	b := m.bar
	e := barEdge{w: bw, h: len(body) + 2, glint: -1}
	if b.search != nil && !b.done {
		e.glint = b.glint
	}
	title, key := paint(cText+bold, " ✻ Go anywhere "), dim(" ctrl+k ")
	if sc := b.scope(); sc.kind != "" {
		title, key = paint(cText+bold, " ⌕ Find in "+sc.label+" "), dim(" ctrl+f ")
	}
	box := []string{e.line(0, "╭", "╮", title, key)}
	for i, l := range body {
		box = append(box, e.cell(0, i+1, "│")+panel(" "+fit(l, inner)+" ")+e.cell(bw-1, i+1, "│"))
	}
	box = append(box, e.line(e.h-1, "╰", "╯", "", m.barStatus()))
	top := max(1, min(m.h/8, len(lines)-len(box)-1))
	left := (m.w - bw - 2) / 2
	plain := make([]string, len(lines))
	for y := range lines {
		plain[y] = ansi.Strip(fit(lines[y], m.w))
		lines[y] = faint(plain[y])
	}
	// The shadow: two cells right of the frame and one row under it, the
	// screen there darker still.
	shade := func(y, from, to int) string {
		return barShadow + strings.ReplaceAll(ansi.Cut(plain[y], from, to), reset, reset+barShadow) + reset
	}
	for i, l := range box {
		y := top + i
		if y >= len(lines) {
			break
		}
		right := faint(ansi.Cut(plain[y], left+bw, m.w))
		if i > 0 {
			right = shade(y, left+bw, left+bw+2) + faint(ansi.Cut(plain[y], left+bw+2, m.w))
		}
		lines[y] = faint(ansi.Cut(plain[y], 0, left)) + l + right
	}
	if y := top + len(box); y < len(lines) {
		lines[y] = faint(ansi.Cut(plain[y], 0, left+2)) + shade(y, left+2, left+bw+2) + faint(ansi.Cut(plain[y], left+bw+2, m.w))
	}
	return strings.Join(lines, "\n")
}

const barShadow = "\x1b[48;2;12;11;10m\x1b[38;2;44;41;38m"

// barEdge colours the bar's frame cell by cell: orange at the top left
// cooling to ember at the bottom right, and, while transcripts are being
// searched, a glint with a fading tail running clockwise round it.
type barEdge struct {
	w, h  int
	glint int // where the glint is along the edge, or -1
}

// around is how far along the edge, clockwise from the top left, a cell is.
func (e barEdge) around(x, y int) int {
	switch {
	case y == 0:
		return x
	case x == e.w-1:
		return e.w - 1 + y
	case y == e.h-1:
		return 2*(e.w-1) + e.h - 1 - x
	}
	return 2*(e.w-1) + 2*(e.h-1) - y
}

func (e barEdge) color(x, y int) string {
	t := 0.65*float64(x)/float64(max(1, e.w-1)) + 0.35*float64(y)/float64(max(1, e.h-1))
	r, g, b := mix([3]float64{232, 128, 92}, [3]float64{110, 52, 40}, t)
	if e.glint >= 0 {
		n := 2*(e.w-1) + 2*(e.h-1)
		d := ((e.glint-e.around(x, y))%n + n) % n
		if d < 14 {
			k := 1 - float64(d)/14
			r, g, b = mix([3]float64{r, g, b}, [3]float64{255, 232, 205}, k*k)
		}
	}
	return rgb(int(r), int(g), int(b))
}

func mix(a, b [3]float64, t float64) (float64, float64, float64) {
	t = max(0, min(1, t))
	return a[0] + (b[0]-a[0])*t, a[1] + (b[1]-a[1])*t, a[2] + (b[2]-a[2])*t
}

func (e barEdge) cell(x, y int, s string) string { return e.color(x, y) + s + reset }

// line is the top or bottom edge, with a label set into it at each end.
func (e barEdge) line(y int, l, r, left, right string) string {
	var sb strings.Builder
	sb.WriteString(e.cell(0, y, l))
	x := 1
	dash := func(n int) {
		for ; n > 0; n-- {
			sb.WriteString(e.cell(x, y, "─"))
			x++
		}
	}
	lw, rw := cellw.String(left), cellw.String(right)
	if lw+rw+6 > e.w {
		left, right, lw, rw = "", "", 0, 0
	}
	if lw > 0 {
		dash(1)
		sb.WriteString(left)
		x += lw
	}
	dash(e.w - 1 - x - rw - map[bool]int{true: 1, false: 0}[rw > 0])
	if rw > 0 {
		sb.WriteString(right)
		x += rw
		dash(1)
	}
	sb.WriteString(e.cell(e.w-1, y, r))
	return sb.String()
}

type barGlintMsg int

// glintTick moves the glint along while a search runs.
func glintTick(gen int) tea.Cmd {
	return tea.Tick(40*time.Millisecond, func(time.Time) tea.Msg { return barGlintMsg(gen) })
}
