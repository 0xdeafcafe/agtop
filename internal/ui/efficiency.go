package ui

import (
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/cellw"
	"github.com/0xdeafcafe/agtop/internal/efficiency"
)

// The Efficiency place: where tokens go, the savers that cut them, and
// whether they did. Overview sums it up; Timeline draws it with a marker
// wherever a saver was installed, changed or first used; Savers installs
// and removes them; Findings is what's worth doing, most at stake first.

var effPages = []string{"Overview", "Timeline", "Savers", "Findings"}

const (
	effOverview = iota
	effTimeline
	effSaversPage
	effFindings
)

var effScopes = []string{"all accounts", "this account", "this folder"}

type effState struct {
	page    int
	started bool
	scope   int
	rng     int // efficiency.Ranges
	metric  efficiency.Metric
	cursor  int // Timeline: the bar under the cursor; -1 is the newest
	saver   int // Savers: the row
	finding int // Findings: the row

	store    *efficiency.Store
	loading  bool
	loaded   time.Time
	view     *efficiency.View
	found    map[string]efficiency.Found
	findings []efficiency.Finding
	events   []efficiency.Event
	gains    []efficiency.Gain
	gainsAt  time.Time

	// An install or removal: its plan waits on y, then its output shows
	// until esc.
	plan    *efficiency.Plan
	running bool
	ran     bool
	failed  bool
	log     []string

	cmp *efficiency.Compare

	noting bool
	note   []rune
}

// typing is whether keys go into a note being written.
func (e *effState) typing() bool { return e.noting }

type effLoadedMsg struct {
	store   *efficiency.Store
	view    *efficiency.View
	found   map[string]efficiency.Found
	events  []efficiency.Event
	gains   []efficiency.Gain
	gainsAt time.Time
}

type effRanMsg struct {
	log []string
	err error
}

// setEffPage shows one of Efficiency's pages.
func (m *Model) setEffPage(p int) {
	e := &m.eff
	if !e.started {
		e.started, e.rng, e.cursor = true, 1, -1
	}
	e.page = (p + len(effPages)) % len(effPages)
	m.mode = modeEff
}

// effOpen starts reading when the place opens with nothing read yet.
func (m *Model) effOpen() tea.Cmd {
	if m.mode != modeEff || m.eff.view != nil {
		return nil
	}
	return m.effLoad(true)
}

// effFolder is the folder "this folder" means: the selected agent's, or
// where agtop was started.
func (m *Model) effFolder() string {
	if a := m.selected(); a != nil && a.Cwd != "" {
		return a.Cwd
	}
	return m.launchDir
}

func (m *Model) effQuery() efficiency.Query {
	e := &m.eff
	q := efficiency.NewQuery(efficiency.Ranges[e.rng], time.Now())
	switch e.scope {
	case 1:
		q.Accounts = []string{m.store.Config.ActiveAccount().ConfigDir}
	case 2:
		q.Project = m.effFolder()
	}
	return q
}

// effLoad works the view out again, reading new transcript lines first
// when scan is set; the first load reads them all, which takes seconds.
func (m *Model) effLoad(scan bool) tea.Cmd {
	e := &m.eff
	if e.loading {
		return nil
	}
	e.loading = true
	store := e.store
	accts := m.store.Config.AllAccounts()
	active := m.store.Config.ActiveAccount()
	q := m.effQuery()
	gains := time.Since(e.gainsAt) > 15*time.Minute
	return func() tea.Msg {
		if store == nil {
			store, scan = efficiency.Open(), true
		}
		if scan {
			store.Refresh(accts)
		}
		msg := effLoadedMsg{store: store, view: store.View(q)}
		msg.found = efficiency.Observe(efficiency.LoadEnv(active))
		msg.events = efficiency.LoadEvents()
		if gains {
			msg.gains, _ = efficiency.RTKGains()
			msg.gainsAt = time.Now()
		}
		return msg
	}
}

func (m *Model) onEffLoaded(msg effLoadedMsg) {
	e := &m.eff
	e.loading, e.loaded = false, time.Now()
	e.store, e.view, e.found, e.events = msg.store, msg.view, msg.found, msg.events
	if !msg.gainsAt.IsZero() {
		e.gains, e.gainsAt = msg.gains, msg.gainsAt
	}
	e.findings = efficiency.Findings(e.view, e.found)
	e.finding = min(e.finding, max(0, len(e.findings)-1))
	if e.cursor >= len(e.view.Points) {
		e.cursor = -1
	}
}

// effSavers are the catalog in the Savers page's order: by kind.
func effSavers() []*efficiency.Saver {
	out := make([]*efficiency.Saver, len(efficiency.Catalog))
	for i := range efficiency.Catalog {
		out[i] = &efficiency.Catalog[i]
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out
}

// effPlan shows what installing (or removing) a saver would do.
func (m *Model) effPlan(s *efficiency.Saver, remove bool) {
	p := efficiency.NewPlan(efficiency.LoadEnv(m.store.Config.ActiveAccount()), s, remove)
	e := &m.eff
	e.plan, e.ran, e.failed, e.log = &p, false, false, nil
}

func (m *Model) effRun() tea.Cmd {
	e := &m.eff
	if e.plan == nil || e.running || e.ran || e.plan.Blocked != "" {
		return nil
	}
	e.running = true
	p := *e.plan
	acct := m.store.Config.ActiveAccount()
	return func() tea.Msg {
		log, err := p.Run(efficiency.LoadEnv(acct))
		return effRanMsg{log: log, err: err}
	}
}

func (m *Model) onEffRan(msg effRanMsg) tea.Cmd {
	e := &m.eff
	e.running, e.ran, e.log = false, true, msg.log
	e.failed = msg.err != nil
	if e.plan == nil {
		return nil
	}
	name := e.plan.Saver.Name
	switch {
	case msg.err != nil:
		e.log = append(e.log, "✗ "+msg.err.Error())
		m.flash(name+": "+msg.err.Error(), true)
	case e.plan.Remove:
		m.flash("removed "+name+" · sessions started from now go without it", false)
	default:
		m.flash("set up "+name+" · sessions started from now have it; running ones when they restart", false)
	}
	return m.effLoad(false)
}

// effMarks are the events inside the view's accounts as chart markers,
// and the first use of each saver.
func (m *Model) effMarks() []effMark {
	var out []effMark
	for _, ev := range m.effEvents() {
		out = append(out, effMark{at: ev.At, glyph: effGlyph(ev.Kind), color: effColor(ev.Kind)})
	}
	return out
}

// effEvents are the logged events that belong to the view, with each
// saver's first use added, oldest first.
func (m *Model) effEvents() []efficiency.Event {
	e := &m.eff
	if e.view == nil {
		return nil
	}
	accts := map[string]bool{}
	for _, a := range e.view.Q.Accounts {
		accts[a] = true
	}
	var out []efficiency.Event
	for _, ev := range e.events {
		if len(accts) == 0 || accts[ev.Account] || ev.Account == "" {
			out = append(out, ev)
		}
	}
	for id, at := range e.view.FirstUse {
		out = append(out, efficiency.Event{At: at, Kind: "used", Saver: id, Source: "transcripts"})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

func effGlyph(kind string) string {
	switch kind {
	case "install":
		return "▲"
	case "remove":
		return "▼"
	case "setting":
		return "◆"
	case "used":
		return "●"
	case "note":
		return "✎"
	}
	return "·"
}

func effColor(kind string) string {
	switch kind {
	case "install":
		return cGreen
	case "remove":
		return cRed
	case "setting":
		return cBlue
	case "used":
		return cOrange
	}
	return cSub
}

// effEventLine is an event in words.
func effEventLine(ev efficiency.Event) string {
	name := ev.Saver
	if s := efficiency.Find(ev.Saver); s != nil {
		name = s.Name
	}
	var what string
	switch ev.Kind {
	case "install":
		what = name + " set up"
	case "remove":
		what = name + " removed"
	case "setting":
		what = ev.Detail
		ev.Detail = ""
	case "used":
		what = name + " first used"
	case "note":
		what = "“" + ev.Detail + "”"
		ev.Detail = ""
	}
	s := paint(effColor(ev.Kind), effGlyph(ev.Kind)) + " " + dim(ev.At.Local().Format("2 Jan 15:04")) + "  " + what
	if ev.Source != "" && ev.Source != "you" {
		s += dim("  " + ev.Source)
	}
	if ev.Detail != "" {
		s += faint("  " + ev.Detail)
	}
	return s
}

func (m *Model) effKey(k tea.KeyPressMsg, s string) tea.Cmd {
	e := &m.eff
	if e.noting {
		switch s {
		case "enter":
			text := strings.TrimSpace(string(e.note))
			e.noting, e.note = false, nil
			if text == "" {
				return nil
			}
			at := time.Now()
			if v := e.view; v != nil && e.cursor >= 0 && e.cursor < len(v.Points) {
				at = v.Points[e.cursor].At
			}
			if err := efficiency.AddEvent(efficiency.Event{At: at, Kind: "note", Source: "you", Detail: text}); err != nil {
				m.flash("note: "+err.Error(), true)
			}
			return m.effLoad(false)
		case "esc":
			e.noting, e.note = false, nil
		case "backspace", "ctrl+h":
			if len(e.note) > 0 {
				e.note = e.note[:len(e.note)-1]
			}
		default:
			if k.Text != "" {
				e.note = append(e.note, []rune(k.Text)...)
			}
		}
		return nil
	}
	if e.plan != nil {
		switch s {
		case "y", "enter":
			if !e.ran {
				return m.effRun()
			}
			e.plan = nil
		case "esc", "q", "n":
			if !e.running {
				e.plan, e.log = nil, nil
			}
		}
		return nil
	}
	switch s {
	case "esc", "q":
		if e.cmp != nil {
			e.cmp = nil
			return nil
		}
		m.setView(placeAgents)
		return m.loadPreview()
	case "s":
		e.scope = (e.scope + 1) % len(effScopes)
		e.cmp = nil
		return m.effLoad(false)
	case "[", "]":
		d := map[string]int{"[": -1, "]": 1}[s]
		e.rng = min(max(e.rng+d, 0), len(efficiency.Ranges)-1)
		e.cursor, e.cmp = -1, nil
		return m.effLoad(false)
	case "r":
		return m.effLoad(true)
	}
	switch e.page {
	case effOverview:
		switch s {
		case "t":
			m.setEffPage(effTimeline)
		case "f", "enter":
			m.setEffPage(effFindings)
		case "i":
			m.setEffPage(effSaversPage)
		}
	case effTimeline:
		return m.effTimelineKey(s)
	case effSaversPage:
		list := effSavers()
		switch s {
		case "up", "k":
			e.saver = roundMove(e.saver, -1, len(list))
		case "down", "j":
			e.saver = roundMove(e.saver, 1, len(list))
		case "enter", "i":
			sv := list[e.saver]
			if e.found[sv.ID].Status == efficiency.On && sv.Setting == nil {
				m.flash(sv.Name+" is on · x removes it", false)
				return nil
			}
			m.effPlan(sv, false)
		case "x":
			sv := list[e.saver]
			if e.found[sv.ID].Status == efficiency.Off {
				m.flash(sv.Name+" isn't set up", false)
				return nil
			}
			m.effPlan(sv, true)
		case "o":
			if u := list[e.saver].URL; u != "" {
				return browse(u)
			}
		}
	case effFindings:
		switch s {
		case "up", "k":
			e.finding = roundMove(e.finding, -1, len(e.findings))
		case "down", "j":
			e.finding = roundMove(e.finding, 1, len(e.findings))
		case "enter":
			if e.finding < len(e.findings) {
				if sv := efficiency.Find(e.findings[e.finding].Fix); sv != nil {
					m.effPlan(sv, false)
				}
			}
		}
	}
	return nil
}

func (m *Model) effTimelineKey(s string) tea.Cmd {
	e := &m.eff
	v := e.view
	if v == nil || len(v.Points) == 0 {
		return nil
	}
	n := len(v.Points)
	cur := e.cursor
	if cur < 0 {
		cur = n - 1
	}
	switch s {
	case "left", "h":
		e.cursor = max(0, cur-1)
	case "right", "l":
		e.cursor = min(n-1, cur+1)
		if e.cursor == n-1 {
			e.cursor = -1
		}
	case "m":
		e.metric = (e.metric + 1) % efficiency.NMetrics
	case "M":
		e.metric = (e.metric + efficiency.NMetrics - 1) % efficiency.NMetrics
	case "e", "E":
		// To the next (or previous) bar with an event.
		evs := m.effEvents()
		idx := func(t time.Time) int { return m.effBucket(t) }
		best := -1
		for _, ev := range evs {
			i := idx(ev.At)
			if i < 0 || i >= n {
				continue
			}
			if s == "e" && i > cur && (best < 0 || i < best) || s == "E" && i < cur && i > best {
				best = i
			}
		}
		if best >= 0 {
			e.cursor = best
		}
	case "b":
		if e.cmp != nil {
			e.cmp = nil
			return nil
		}
		m.effCompare(cur)
	case "+", "=":
		if e.cmp != nil {
			m.effCompareDays(e.cmp.Days + 1)
		}
	case "-":
		if e.cmp != nil && e.cmp.Days > 1 {
			m.effCompareDays(e.cmp.Days - 1)
		}
	case "n":
		e.noting, e.note = true, nil
	}
	return nil
}

// effBucket is which of the view's bars t falls in, -1 when none.
func (m *Model) effBucket(t time.Time) int {
	v := m.eff.view
	if v == nil || len(v.Points) == 0 {
		return -1
	}
	for i := len(v.Points) - 1; i >= 0; i-- {
		if !t.Before(v.Points[i].At) {
			if i == len(v.Points)-1 && t.After(v.Q.To) {
				return -1
			}
			return i
		}
	}
	return -1
}

// effCompare compares the days either side of the cursor: of the event in
// its bar when there is one, with that saver's sessions split out.
func (m *Model) effCompare(cur int) {
	e := &m.eff
	v := e.view
	at := v.Points[cur].At
	var saver *efficiency.Saver
	for _, ev := range m.effEvents() {
		if m.effBucket(ev.At) == cur && ev.Kind != "note" {
			at, saver = ev.At, efficiency.Find(ev.Saver)
			break
		}
	}
	c := e.store.Compare(v.Q, at, 7, saver)
	e.cmp = &c
}

func (m *Model) effCompareDays(days int) {
	e := &m.eff
	c := e.store.Compare(e.view.Q, e.cmp.At, days, e.cmp.Saver)
	e.cmp = &c
}

// effHint is the keys at the foot of the page.
func (m *Model) effHint() string {
	e := &m.eff
	w := m.w - 4
	switch {
	case e.noting:
		return paint(cOrange, "note ❯ ") + paint(cText, string(e.note)) + paint(cOrange, "▏") + dim("   enter keep · esc cancel")
	case e.plan != nil && e.running:
		return paint(cOrange, spinner[m.tick%len(spinner)]) + dim(" running…")
	case e.plan != nil && e.ran:
		return keysFit(w, "esc", "close")
	case e.plan != nil && e.plan.Blocked != "":
		return keysFit(w, "esc", "back")
	case e.plan != nil:
		verb := "install"
		switch {
		case e.plan.Remove:
			verb = "remove"
		case e.plan.Saver.Setting != nil:
			verb = "change it"
		}
		return keysFit(w, "y", verb, "esc", "cancel")
	}
	common := []string{"s", effScopes[(e.scope+1)%len(effScopes)], "[ ]", "range", "tab", effPages[(e.page+1)%len(effPages)], "esc", "back"}
	switch e.page {
	case effOverview:
		return keysFit(w, append([]string{"t", "timeline", "enter", "findings", "i", "savers"}, common...)...)
	case effTimeline:
		if e.cmp != nil {
			return keysFit(w, append([]string{"+ -", "days each side", "b", "close", "←→", "move"}, common...)...)
		}
		return keysFit(w, append([]string{"←→", "move", "m", "metric", "e", "next event", "b", "before/after", "n", "note"}, common...)...)
	case effSaversPage:
		return keysFit(w, append([]string{"↑↓", "choose", "enter", "set up", "x", "remove", "o", "its page"}, common...)...)
	}
	return keysFit(w, append([]string{"↑↓", "choose", "enter", "fix it"}, common...)...)
}

// effBody is the page's lines.
func (m *Model) effBody() []string {
	e := &m.eff
	w := m.w - 4
	v := e.view
	head := paint(cText+bold, effPages[e.page])
	scope := effScopes[e.scope]
	if e.scope == 2 {
		scope = tildify(m.effFolder())
	}
	meta := scope + " · last " + efficiency.Ranges[e.rng].Name
	if v != nil {
		meta += fmt.Sprintf(" · %d sessions, %d subagent runs", v.Sessions, v.Subagents)
	}
	if e.loading {
		meta += "  " + paint(cOrange, spinner[m.tick%len(spinner)])
	}
	out := []string{head + dim("   "+meta), ""}
	if e.plan != nil {
		return append(out, m.effPlanBody(w)...)
	}
	if v == nil {
		return append(out, paint(cOrange, spinner[m.tick%len(spinner)])+dim(" reading every transcript for the first time; later it's only what's new…"))
	}
	switch e.page {
	case effOverview:
		out = append(out, m.effOverview(w)...)
	case effTimeline:
		out = append(out, m.effTimeline(w)...)
	case effSaversPage:
		out = append(out, m.effSaversBody(w)...)
	case effFindings:
		out = append(out, m.effFindingsBody(w)...)
	}
	return out
}

func (m *Model) effOverview(w int) []string {
	e := &m.eff
	v := e.view
	t := &v.Total
	cost := t.Cost()
	tiles := [][2]string{
		{money(cost), "spent"},
		{fmt.Sprintf("%.1f%%", v.CacheHit()), "from cache"},
		{tokens(v.CtxPerReq()), "context/request"},
		{tokens(v.StartMedian), "at the start"},
		{tokens(v.OutPerReq()), "output/request"},
		{m.effOnCount(), "savers on"},
	}
	tw := max(12, min(22, w/len(tiles)))
	var vals, labels strings.Builder
	for _, tl := range tiles {
		vals.WriteString(fit(paint(cText+bold, tl[0]), tw-2) + "  ")
		labels.WriteString(fit(dim(tl[1]), tw-2) + "  ")
	}
	out := []string{vals.String(), labels.String(), ""}

	out = append(out, rule("Where the money goes", "", w))
	parts := []struct {
		name string
		c    float64
		col  string
		why  string
	}{
		{"cache read", t.CCR, cSub, "the context, read again by every request"},
		{"cache write", t.CCW, cYellow, "new context, and cold starts writing it all again"},
		{"output", t.COut, cOrange, "what Claude writes, thinking included"},
		{"input", t.CIn, cBlue, "input not cached"},
	}
	barW := max(10, min(40, w-60))
	for _, p := range parts {
		share := 0.0
		if cost > 0 {
			share = p.c / cost
		}
		n := int(math.Round(share * float64(barW)))
		b := paint(p.col, strings.Repeat("━", n)) + faint(strings.Repeat("─", barW-n))
		pct := fmt.Sprintf("%3.0f%%", share*100)
		if share > 0 && share < 0.01 {
			pct = " <1%"
		}
		out = append(out, "  "+fit(p.name, 12)+b+"  "+right(pct, 4)+"  "+right(money(p.c), 8)+"   "+dim(p.why))
	}
	out = append(out, "")

	// Cost by bar, with the markers under it.
	out = append(out, rule("Cost", "▲ set up  ▼ removed  ◆ setting  ● first used  ✎ note", w))
	series := v.Series(efficiency.MetricCost)
	vals2 := make([]float64, len(series.Values))
	top := 0.0
	for i, p := range series.Values {
		for _, x := range p {
			vals2[i] += x
		}
		top = math.Max(top, vals2[i])
	}
	cw := w - 10
	per := min(16, cw/max(1, len(vals2)))
	width := cw
	var chart []string
	if per >= 2 {
		width = per * len(vals2)
		chart = stackBars(effWrap(vals2), []string{cOrange}, niceTop(top), 4, per-1, 1)
	} else {
		chart = braille(vals2, cw, 4, cOrange)
	}
	at := func(i int) int { return i * width / max(1, len(vals2)) }
	for i, l := range chart {
		label := ""
		switch i {
		case 0:
			label = axisLabel(niceTop(top), "$")
		case len(chart) - 1:
			label = "$0"
		}
		out = append(out, right(dim(label), 7)+" "+faint("│")+l)
	}
	out = append(out, blanks(9)+markLane(m.effMarks(), m.effBucket, width, at))
	out = append(out, blanks(9)+m.effAxis(width, len(vals2)), "")

	out = append(out, rule("Savers", "", w))
	var on []string
	for _, sv := range effSavers() {
		f := e.found[sv.ID]
		if f.Status == efficiency.Off {
			continue
		}
		s := effStatusGlyph(f.Status) + " " + sv.Name
		if u := v.Uses[sv.ID]; u != nil {
			s += dim(fmt.Sprintf(" %d× in %d sessions", u.N, u.Sessions))
		}
		on = append(on, s)
	}
	if len(on) == 0 {
		out = append(out, dim("  none set up · i shows what there is"))
	} else {
		out = append(out, "  "+strings.Join(on, "   "))
	}
	out = append(out, "")

	out = append(out, rule("Worth doing", fmt.Sprintf("%d findings", len(e.findings)), w))
	for i, f := range e.findings {
		if i == 4 {
			out = append(out, dim(fmt.Sprintf("  and %d more · enter", len(e.findings)-4)))
			break
		}
		out = append(out, "  "+m.effFindingLine(f, w-2))
	}
	if len(e.findings) == 0 {
		out = append(out, dim("  nothing stands out"))
	}
	return out
}

func effWrap(vals []float64) [][]float64 {
	out := make([][]float64, len(vals))
	for i, v := range vals {
		out[i] = []float64{v}
	}
	return out
}

// effAxis is the time axis under n bars across width cells: a date at the
// start, the middle and the end.
func (m *Model) effAxis(width, n int) string {
	v := m.eff.view
	if v == nil || n == 0 || width <= 0 {
		return ""
	}
	cells := []rune(strings.Repeat(" ", width))
	format := "2 Jan"
	if !v.Q.Daily {
		format = "15:04"
	}
	put := func(i int) {
		label := []rune(v.Points[i].At.Local().Format(format))
		x := min(i*width/n, width-len(label))
		if x < 0 {
			return
		}
		for j := range label {
			if cells[x+j] != ' ' {
				return
			}
		}
		copy(cells[x:], label)
	}
	put(0)
	put(n - 1)
	put(n / 2)
	return dim(string(cells))
}

func (m *Model) effOnCount() string {
	on, half := 0, 0
	for _, f := range m.eff.found {
		switch f.Status {
		case efficiency.On:
			on++
		case efficiency.Partial:
			half++
		}
	}
	if half > 0 {
		return fmt.Sprintf("%d · %d half", on, half)
	}
	return fmt.Sprint(on)
}

func effStatusGlyph(s efficiency.Status) string {
	switch s {
	case efficiency.On:
		return paint(cGreen, "●")
	case efficiency.Partial:
		return paint(cYellow, "◐")
	}
	return faint("○")
}

func (m *Model) effFindingLine(f efficiency.Finding, w int) string {
	right := ""
	if f.Cost > 0 {
		right = paint(cYellow, "≈"+efficiency.Money(f.Cost))
	}
	if sv := efficiency.Find(f.Fix); sv != nil {
		right += dim("  → " + sv.Name)
	}
	title := fit(f.Title, max(10, w-cellw.String(right)-2))
	return title + "  " + right
}

func (m *Model) effTimeline(w int) []string {
	e := &m.eff
	v := e.view
	series := v.Series(e.metric)
	n := len(series.Values)
	colors := []string{cSub, cYellow, cBlue, cOrange, cGreen, cRed}
	if e.metric == efficiency.MetricOut {
		colors = []string{cDim, cOrange}
	}
	var legend []string
	for i, p := range series.Parts {
		legend = append(legend, paint(colors[i%len(colors)], "■")+" "+dim(p))
	}
	out := []string{paint(cText, efficiency.MetricNames[e.metric]) + faint("  m") + "     " + strings.Join(legend, "  "), ""}

	top := 0.0
	for _, p := range series.Values {
		s := 0.0
		for _, x := range p {
			s += x
		}
		top = math.Max(top, s)
	}
	switch series.Unit {
	case "%":
		top = 100
	case "B":
		top = niceTop(top/1024) * 1024
	default:
		top = niceTop(top)
	}
	avail := w - 10
	per := min(16, max(2, avail/max(1, n)))
	if per*n > avail {
		per = max(1, avail/max(1, n))
	}
	barW, gap := max(1, per-1), 1
	if per == 1 {
		barW, gap = 1, 0
	}
	h := max(6, min(14, m.h-30))
	if e.cmp != nil {
		h = max(5, min(8, m.h-40))
	}
	chart := stackBars(series.Values, colors, top, h, barW, gap)
	cur := e.cursor
	if cur < 0 {
		cur = n - 1
	}
	for i, l := range chart {
		label := ""
		switch i {
		case 0:
			label = axisLabel(top, series.Unit)
		case h / 2:
			label = axisLabel(top/2, series.Unit)
		case h - 1:
			label = axisLabel(0, series.Unit)
		}
		out = append(out, right(dim(label), 7)+" "+faint("│")+l)
	}
	// The cursor, under its bar, then the markers and the dates.
	cursorLine := []rune(strings.Repeat(" ", (barW+gap)*n))
	for j := range barW {
		if x := cur*(barW+gap) + j; x < len(cursorLine) {
			cursorLine[x] = '▔'
		}
	}
	out = append(out, blanks(9)+paint(cText, string(cursorLine)))
	out = append(out, blanks(9)+markLane(m.effMarks(), m.effBucket, (barW+gap)*n, func(i int) int { return i * (barW + gap) }))
	out = append(out, blanks(9)+m.effAxis((barW+gap)*n, n), "")

	// What's under the cursor.
	if cur < len(v.Points) {
		p := v.Points[cur]
		b := &p.B
		when := p.At.Local().Format("Mon 2 Jan")
		if !v.Q.Daily {
			when = p.At.Local().Format("Mon 15:04")
		}
		read := ""
		if b.Req > 0 {
			hit := 0.0
			if c := b.Context(); c > 0 {
				hit = 100 * float64(b.CR) / float64(c)
			}
			read = fmt.Sprintf("%s requests · %s context each · %.1f%% from cache · %s", thousands(b.Req), tokens(b.Context()/b.Req), hit, money(b.Cost()))
		} else {
			read = "nothing ran"
		}
		for _, ev := range m.effEvents() {
			if m.effBucket(ev.At) == cur {
				read += "   " + effEventLine(ev)
			}
		}
		out = append(out, paint(cOrange, "▸ ")+paint(cText, when)+"  "+read, "")
	}

	if e.cmp != nil {
		return append(out, m.effCompareBody(w)...)
	}
	evs := m.effEvents()
	out = append(out, rule("Events", "e jumps to the next · n writes a note at the cursor", w))
	shown := 0
	for i := len(evs) - 1; i >= 0 && shown < 8; i-- {
		if evs[i].At.Before(v.Q.From) {
			continue
		}
		out = append(out, "  "+effEventLine(evs[i]))
		shown++
	}
	if shown == 0 {
		out = append(out, dim("  none in these days: savers set up here, settings changed and notes show as markers"))
	}
	return out
}

func (m *Model) effCompareBody(w int) []string {
	c := m.eff.cmp
	title := "Before and after " + c.At.Local().Format("2 Jan 15:04")
	if c.Saver != nil {
		title = "Before and after " + c.Saver.Name + ", " + c.At.Local().Format("2 Jan")
	}
	out := []string{rule(title, fmt.Sprintf("%d days each side · sessions that started then", c.Days), w)}
	type row struct {
		name   string
		a, b   float64
		format func(float64) string
		good   int // -1: lower is better, 1: higher is
	}
	tok := func(x float64) string { return tokens(int64(x)) }
	usd := func(x float64) string { return money(x) }
	pct := func(x float64) string { return fmt.Sprintf("%.1f%%", x) }
	kb := func(x float64) string { return bytesShort(int64(x)) }
	cost := func(x float64) string {
		if x < 1 {
			return fmt.Sprintf("$%.3f", x)
		}
		return money(x)
	}
	rows := func(a, b efficiency.Side) []row {
		return []row{
			{"context per request", a.CtxPerReq, b.CtxPerReq, tok, -1},
			{"read from cache", a.CacheHit, b.CacheHit, pct, 1},
			{"written per request", a.OutPerReq, b.OutPerReq, tok, -1},
			{"tool output per call", a.ToolPerCall, b.ToolPerCall, kb, -1},
			{"cost per request", a.CostPerReq, b.CostPerReq, cost, -1},
			{"median session", a.CostSession, b.CostSession, usd, -1},
			{"context at the start", float64(a.StartMedian), float64(b.StartMedian), tok, -1},
		}
	}
	table := func(la, lb string, a, b efficiency.Side) {
		out = append(out, "  "+fit("", 24)+right(dim(la), 12)+right(dim(lb), 12)+right(dim("change"), 10))
		out = append(out, "  "+fit(dim("sessions"), 24)+right(fmt.Sprint(a.Sessions), 12)+right(fmt.Sprint(b.Sessions), 12))
		for _, r := range rows(a, b) {
			change := ""
			if r.a > 0 && r.b > 0 {
				d := (r.b - r.a) / r.a * 100
				col := cSub
				if math.Abs(d) >= 5 {
					if (d < 0) == (r.good < 0) {
						col = cGreen
					} else {
						col = cRed
					}
				}
				change = paint(col, fmt.Sprintf("%+.0f%%", d))
			}
			out = append(out, "  "+fit(r.name, 24)+right(r.format(r.a), 12)+right(r.format(r.b), 12)+right(change, 10))
		}
	}
	table("before", "after", c.Before, c.After)
	if c.Saver != nil && c.With.Sessions+c.Without.Sessions > 0 {
		out = append(out, "", dim(fmt.Sprintf("  In the days after, sessions that used %s against those that didn't: the same kind of days, so fairer", c.Saver.Name)))
		table("with", "without", c.With, c.Without)
	}
	var notes []string
	if c.Before.Sessions < efficiency.FewSessions || c.After.Sessions < efficiency.FewSessions {
		notes = append(notes, fmt.Sprintf("under %d sessions on a side: too few to say much", efficiency.FewSessions))
	}
	var others []string
	for _, ev := range m.effEvents() {
		if ev.At.Equal(c.At) || ev.At.Before(c.At.AddDate(0, 0, -c.Days)) || ev.At.After(c.At.AddDate(0, 0, c.Days)) {
			continue
		}
		name := ev.Saver
		if s := efficiency.Find(ev.Saver); s != nil {
			name = s.Name
		}
		if ev.Kind == "note" {
			name = "“" + ev.Detail + "”"
		}
		others = append(others, name)
	}
	if len(others) > 0 {
		notes = append(notes, "other changes in these days: "+strings.Join(others, ", "))
	}
	if c.Saver != nil && c.Saver.ID == "rtk" && len(m.eff.gains) > 0 {
		var saved int64
		cmds := 0
		for _, g := range m.eff.gains {
			d, err := time.ParseInLocation("2006-01-02", g.Day, time.Local)
			if err == nil && !d.Before(c.At.AddDate(0, 0, -1)) && d.Before(c.At.AddDate(0, 0, c.Days)) {
				saved += g.Saved
				cmds += g.Commands
			}
		}
		notes = append(notes, fmt.Sprintf("rtk's own count for these days: %s tokens saved over %d commands (its estimate: shell output bytes ÷ 4)", tokens(saved), cmds))
	}
	notes = append(notes, "what you worked on changes these as much as any saver: read them as a hint, not a result")
	out = append(out, "")
	for _, n := range notes {
		out = append(out, dim("  · "+n))
	}
	return out
}

func (m *Model) effSaversBody(w int) []string {
	e := &m.eff
	list := effSavers()
	e.saver = min(max(e.saver, 0), len(list)-1)
	var out []string
	kind := efficiency.Kind(-1)
	for i, sv := range list {
		if sv.Kind != kind {
			kind = sv.Kind
			if i > 0 {
				out = append(out, "")
			}
			out = append(out, dim(efficiency.KindNames[kind]))
		}
		f := e.found[sv.ID]
		state := ""
		switch f.Status {
		case efficiency.On:
			state = paint(cGreen, "on")
			if sv.Setting != nil {
				state = paint(cGreen, f.Value)
			}
		case efficiency.Partial:
			state = paint(cYellow, "half")
		default:
			state = faint("–")
			if len(sv.Install) == 0 && sv.Setting == nil {
				state = faint("by hand")
			}
		}
		use := ""
		if u := e.view.Uses[sv.ID]; u != nil {
			use = dim(fmt.Sprintf("%d× in %d sessions", u.N, u.Sessions))
		}
		line := " " + effStatusGlyph(f.Status) + " " + fit(sv.Name, 28) + fit(state, 9) + fit(dim(sv.About), max(10, w-70)) + "  " + use
		if i == e.saver {
			line = highlight(paint(cOrange, "▍")+line[1:], w)
		}
		out = append(out, line)
	}
	out = append(out, "", rule(list[e.saver].Name, list[e.saver].URL, w))
	return append(out, m.effSaverDetail(list[e.saver], w)...)
}

func (m *Model) effSaverDetail(sv *efficiency.Saver, w int) []string {
	e := &m.eff
	f := e.found[sv.ID]
	var out []string
	add := func(label, text string) {
		if text == "" {
			return
		}
		for i, l := range wrap(text, w-16) {
			if i == 0 {
				out = append(out, "  "+fit(dim(label), 14)+l)
			} else {
				out = append(out, "  "+blanks(14)+l)
			}
		}
	}
	add("what", sv.About)
	if st := sv.Setting; st != nil {
		name := st.Key
		if st.Env {
			name = "env." + st.Key
		}
		add("setting", fmt.Sprintf("%s: %v in settings.json · unset, Claude Code uses %s", name, st.Value, st.Default))
	}
	add("they say", sv.Claim)
	add("measured", sv.Measured)
	add("careful", sv.Note)
	switch f.Status {
	case efficiency.Off:
		add("here", "not set up")
	default:
		here := strings.Join(f.Parts, ", ")
		if f.Wants != "" {
			here += " · " + f.Wants
		}
		if !f.Since.IsZero() {
			here += " · since " + f.Since.Local().Format("2 Jan 2006")
		}
		add("here", here)
	}
	if u := e.view.Uses[sv.ID]; u != nil {
		add("used", fmt.Sprintf("%d times in %d sessions, first %s, last %s", u.N, u.Sessions, u.First.Local().Format("2 Jan 15:04"), u.Last.Local().Format("2 Jan 15:04")))
	} else if len(sv.Uses) > 0 && f.Status != efficiency.Off {
		add("used", "not seen in these days")
	}
	if sv.ID == "rtk" && len(e.gains) > 0 {
		var saved int64
		cmds := 0
		for _, g := range e.gains {
			saved += g.Saved
			cmds += g.Commands
		}
		add("rtk says", fmt.Sprintf("%s tokens saved over %d commands (its estimate)", tokens(saved), cmds))
	}
	if len(sv.Manual) > 0 {
		add("by hand", strings.Join(sv.Manual, "  ·  "))
	}
	return out
}

func (m *Model) effFindingsBody(w int) []string {
	e := &m.eff
	if len(e.findings) == 0 {
		return []string{dim("Nothing stands out in these days.")}
	}
	var out []string
	for i, f := range e.findings {
		line := " " + m.effFindingLine(f, w-1)
		if i == e.finding {
			line = highlight(paint(cOrange, "▍")+line[1:], w)
		}
		out = append(out, line)
		for _, l := range wrap(f.Detail, w-6) {
			out = append(out, "    "+dim(l))
		}
		out = append(out, "")
	}
	out = append(out, faint("≈ is roughly the dollars involved in these days, from list prices; carrying costs assume half a session's requests re-read each token."))
	return out
}

func (m *Model) effPlanBody(w int) []string {
	e := &m.eff
	p := e.plan
	verb := "Set up "
	if p.Remove {
		verb = "Remove "
	}
	out := []string{paint(cText+bold, verb+p.Saver.Name) + dim("   "+p.Saver.About), ""}
	if p.Blocked != "" {
		out = append(out, paint(cYellow, "Can't here: ")+p.Blocked)
		if p.Saver.URL != "" {
			out = append(out, dim("  "+p.Saver.URL))
		}
		return out
	}
	if p.Saver.Note != "" {
		out = append(out, paint(cYellow, "Careful: ")+p.Saver.Note, "")
	}
	if len(p.Cmds) > 0 {
		out = append(out, dim("Will run, in order, as "+m.store.Config.ActiveAccount().Name+":"))
		for i, c := range p.Cmds {
			out = append(out, fmt.Sprintf("  %d  %s", i+1, paint(cText, strings.Join(c, " "))))
		}
		out = append(out, "")
	}
	if len(p.Changes) > 0 {
		out = append(out, dim("Will change:"))
		for _, c := range p.Changes {
			out = append(out, "  "+c)
		}
		out = append(out, "")
	}
	out = append(out, dim("First, settings.json, CLAUDE.md and .claude.json are copied to "+tildify(filepath.Join(efficiency.Dir(), "backups"))+"."))
	if p.Undo != "" {
		out = append(out, dim("Undo: "+p.Undo))
	}
	out = append(out, dim("Sessions pick it up when they next start."))
	if e.running || e.ran {
		out = append(out, "")
		if e.running {
			out = append(out, paint(cOrange, spinner[m.tick%len(spinner)])+dim(" running…"))
		}
		for _, l := range e.log {
			out = append(out, fit(dim(l), w))
		}
		if e.ran && !e.failed {
			out = append(out, paint(cGreen, "✓ done")+dim(" · the event is on the Timeline"))
		}
	}
	return out
}
