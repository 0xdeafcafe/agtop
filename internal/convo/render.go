package convo

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/0xdeafcafe/agtop/internal/headless"
)

// Line is one drawn row. Ref names what it belongs to, for selection and the
// mouse: "t13" for a turn heading, "t13:s:<tool id>" for a step,
// "t13:run:4" for a folded run of steps. Empty for rows you can't act on.
type Line struct {
	Text string
	Ref  string
}

// Options say how to draw.
type Options struct {
	Width    int
	Now      time.Time
	Tick     int
	Open     map[string]bool // fold overrides by ref; absent means the default
	Verbose  bool            // ctrl+o: open everything, trim nothing
	Selected string
	Focused  bool
}

const (
	capRow   = 124 // numbers never drift further right than this
	capProse = 100 // prose wraps here however wide the pane
	gutter   = 8   // the work axis: narration, step labels, output
)

type cached struct {
	key   cacheKey
	lines []Line
}

// cacheKey is everything that affects a turn's drawing: its content, the
// width, its folds, the selection inside it, and for a live turn the clock.
type cacheKey struct {
	width, ver int
	open, verb bool
	folds, sel string
	focused    bool
	tick       int
	now        int64
	clock      bool
}

// Render draws every turn, oldest first.
func (s *Session) Render(o Options) []Line {
	if o.Width < 20 {
		o.Width = 20
	}
	s.memoTurn()
	folds := foldsByTurn(o.Open)
	parts := make([][]Line, len(s.Turns))
	n := 0
	for i, t := range s.Turns {
		recent := i >= len(s.Turns)-2
		parts[i] = s.turn(t, o, recent, folds)
		n += len(parts[i])
	}
	out := make([]Line, 0, n)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// foldsByTurn groups the fold overrides by the turn they're in, each
// turn's sorted, so a turn's cache key names only its own.
func foldsByTurn(open map[string]bool) map[string]string {
	if len(open) == 0 {
		return nil
	}
	by := map[string][]string{}
	for k, v := range open {
		t, _, _ := strings.Cut(k, ":")
		by[t] = append(by[t], k+"="+strconv.FormatBool(v))
	}
	out := make(map[string]string, len(by))
	for t, fs := range by {
		sort.Strings(fs)
		out[t] = strings.Join(fs, ",")
	}
	return out
}

func (s *Session) turn(t *Turn, o Options, recent bool, folds map[string]string) []Line {
	if t.ref == "" {
		t.ref = "t" + strconv.Itoa(t.N)
	}
	ref := t.ref
	open := recent || t.Live || t.Err != ""
	if v, ok := o.Open[ref]; ok {
		open = v
	}
	key := s.cacheKey(t, o, ref, open, folds)
	if c, ok := s.cache[t]; ok && c.key == key {
		return c.lines
	}
	d := drawer{s: s, t: t, o: o, ref: ref, cw: min(o.Width, capRow)}
	if open {
		d.open()
	} else {
		d.folded()
	}
	// The gap between turns is plain: the running turn's rail ends with it.
	d.lines = append(d.lines, Line{Text: row("", "", "", d.o.Width, d.cw)})
	s.cache[t] = cached{key: key, lines: d.lines}
	return d.lines
}

func (s *Session) cacheKey(t *Turn, o Options, ref string, open bool, folds map[string]string) cacheKey {
	k := cacheKey{width: o.Width, ver: t.ver, open: open, verb: o.Verbose, folds: folds[ref]}
	if o.Selected == ref || strings.HasPrefix(o.Selected, ref) && strings.HasPrefix(o.Selected[len(ref):], ":") {
		k.sel, k.focused = o.Selected, o.Focused
	}
	if t.Live || waiting(t) {
		k.clock, k.tick, k.now = true, o.Tick, o.Now.Unix()
	}
	return k
}

// waiting is whether a step in the turn waits on you. Every change to a
// step touches its turn, so the answer holds until the turn changes.
func waiting(t *Turn) bool {
	if t.waitVer == t.ver+1 {
		return t.waits
	}
	t.waits = false
	for _, st := range t.steps {
		if st.Status == Waiting {
			t.waits = true
			break
		}
	}
	t.waitVer = t.ver + 1
	return t.waits
}

type drawer struct {
	s     *Session
	t     *Turn
	o     Options
	ref   string
	cw    int
	lines []Line
}

var (
	spineLive = paint(cOrange, "▏")
	spineErr  = paint(cRed, "▏")
)

func (d *drawer) spine() string {
	switch {
	case d.t.Live:
		return spineLive
	case d.t.Err != "":
		return spineErr
	}
	return " "
}

// add appends a row; a selected row gets the selection surface and marker.
func (d *drawer) add(ref, b, left, right string) {
	if ref != "" && ref == d.o.Selected {
		b = bgSelU
		mark := faint("▍")
		if d.o.Focused {
			b, mark = bgSel, paint(cOrange, "▍")
		}
		// The marker takes column 0, where the spine would be.
		left = mark + strings.TrimPrefix(left, d.spine())
	}
	d.lines = append(d.lines, Line{Text: row(b, left, right, d.o.Width, d.cw), Ref: ref})
}

func (d *drawer) blank() {
	d.lines = append(d.lines, Line{Text: row("", d.spine(), "", d.o.Width, d.cw)})
}

func (d *drawer) mark() string {
	t := d.t
	switch {
	case t.Live:
		return paint(cOrange, "✻")
	case t.Stopped:
		return dim("⏹")
	case t.Err != "":
		return paint(cRed, "✗")
	}
	return paint(cGreen, "✓")
}

func (d *drawer) meta() string {
	t := d.t
	end := t.End
	if t.Live {
		end = d.o.Now
	}
	var parts []string
	if n := t.Steps(); n > 0 {
		parts = append(parts, plural(n, "step"))
	}
	if !t.Start.IsZero() {
		parts = append(parts, dur(end.Sub(t.Start)))
	}
	if m := money(t.Cost); m != "" {
		parts = append(parts, m)
	}
	return strings.Join(parts, "   ")
}

// folded is one row: your ask, then how it came out.
func (d *drawer) folded() {
	t := d.t
	ask := oneLine(t.Prompt)
	if ask == "" {
		ask = "picked up on its own"
	}
	outcome := t.Outcome()
	if t.Err != "" {
		outcome = paint(cRed, t.Err)
	} else {
		outcome = text(outcome)
	}
	askW := max(16, d.cw*2/5)
	if w := len([]rune(ask)); w > askW {
		ask = string([]rune(ask)[:askW-1]) + "…"
	}
	who := styledAsk(ask, cSub)
	if strings.TrimSpace(t.Prompt) == "" && t.From == "" {
		who = dim("◌ picked up on its own")
	}
	if t.From != "" {
		who = dim("◌ "+t.From+" · ") + sub(ask)
	}
	left := "  " + faint("▸") + " " + d.mark() + " " + dim(fmt.Sprintf("#%d", t.N)) + "  " + who
	if outcome != "" && outcome != text("") {
		left += "  " + dim("→") + " " + outcome
	}
	d.add(d.ref, "", left, dim(d.meta()))
}

func (d *drawer) open() {
	t := d.t
	band := bgWell
	switch {
	case t.Live:
		band = bgLive
	case t.Err != "":
		band = bgErr
	}
	right := dim(fmt.Sprintf("#%d", t.N))
	if m := d.meta(); m != "" {
		if t.Live {
			right += "  " + paint(cOrange, m)
		} else {
			right += "  " + dim(m)
		}
	}
	ask := strings.TrimSpace(t.Prompt)
	if ask == "" && len(t.Images) > 0 {
		ask = "(images)"
	}
	if ask == "" {
		ask = "picked up on its own"
	}
	headW := max(20, d.cw-11-len([]rune(stripANSI(right)))-2)
	styled, label := styledAsk(oneLine(ask), cText+bold), dim("you")
	if strings.TrimSpace(t.Prompt) == "" && t.From == "" && len(t.Images) == 0 {
		styled, label = dim("picked up on its own"), dim("◌")
	}
	if t.From != "" {
		styled, label = sub(oneLine(ask)), dim("◌ "+t.From)
	}
	rows := wrap(styled, min(headW-len([]rune(stripANSI(label)))+3, capProse))
	if len(rows) > 3 {
		rows = append(rows[:2], rows[2]+dim(" …"))
	}
	for i, r := range rows {
		if i == 0 {
			d.add(d.ref, band, d.spine()+" "+faint("▾")+" "+d.mark()+" "+label+"  "+r, right)
		} else {
			d.add(d.ref, band, d.spine()+"          "+r, "")
		}
	}
	if len(t.Images) > 0 {
		var chips []string
		for _, im := range t.Images {
			chips = append(chips, paint(cBlue, "▣ ")+sub(im))
		}
		d.add(d.ref, band, d.spine()+"          "+strings.Join(chips, "   "), "")
	}
	d.blank()

	items := t.Items
	// A running turn keeps its last few steps in view; everything clean
	// before them folds, so a busy agent doesn't flood the screen.
	keep := len(items)
	if t.Live {
		seen := 0
		for i := len(items) - 1; i >= 0; i-- {
			if items[i].Kind == KStep {
				seen++
				if seen == 3 {
					keep = i
					break
				}
			}
		}
		if seen < 3 {
			keep = 0
		}
	}
	for i := 0; i < len(items); i++ {
		it := items[i]
		// A run of three or more clean steps folds to one row; a failure
		// never folds.
		if !d.o.Verbose && it.Kind == KStep && i < keep {
			j := i
			for j < keep && j < len(items) && items[j].Kind == KStep && foldable(items[j].Step) {
				j++
			}
			if j-i >= 3 {
				runRef := fmt.Sprintf("%s:run:%d", d.ref, i)
				if !d.o.Open[runRef] {
					d.run(runRef, items[i:j])
					i = j - 1
					continue
				}
			}
		}
		switch it.Kind {
		case KText:
			if it.Answer {
				d.answer(it.Text)
			} else {
				d.prose(it.Text, gutter, cSub)
			}
		case KThinking:
			// Thinking shows while it happens; afterwards only in verbose.
			switch {
			case t.Live && i == len(items)-1:
				d.add("", "", d.spine()+strings.Repeat(" ", gutter-1)+dim("✻ thinking"), "")
			case d.o.Verbose:
				d.add("", "", d.spine()+strings.Repeat(" ", gutter-1)+dim("✻ thought"), "")
				if strings.TrimSpace(it.Text) != "" {
					d.prose(it.Text, gutter+2, cDim)
				}
			}
		case KInterject:
			for k, r := range wrap(text(oneLine(it.Text)), min(d.cw-10, capProse)) {
				lead := dim("you") + "  "
				if k > 0 {
					lead = "     "
				}
				d.add("", "", d.spine()+"   "+lead+r, "")
			}
		case KStep:
			d.step(it.Step, 0)
		}
	}
	switch {
	case t.Stopped:
		d.add("", "", d.spine()+"   "+dim("⏹ stopped"), "")
	case t.Err != "" && !t.Live:
		d.add("", bgErr, d.spine()+"   "+paint(cRed, "✗ "+t.Err), dim("your next message picks it up"))
	}
	// The rail stops at the last thing drawn, never on an empty row.
	for n := len(d.lines); n > 1 && strings.TrimSpace(stripANSI(d.lines[n-1].Text)) == strings.TrimSpace(stripANSI(d.spine())) && d.lines[n-1].Ref == ""; n-- {
		d.lines = d.lines[:n-1]
	}
}

func (d *drawer) run(ref string, items []*Item) {
	counts := map[string]int{}
	var order []string
	var first, last time.Time
	for _, it := range items {
		g := glyphFor(it.Step.Tool)
		if counts[g] == 0 {
			order = append(order, g)
		}
		counts[g]++
		if first.IsZero() || it.Step.Start.Before(first) {
			first = it.Step.Start
		}
		if it.Step.End.After(last) {
			last = it.Step.End
		}
	}
	left := d.spine() + "   " + paint(cGreen, "✓") + " " + dim(plural(len(items), "step"))
	for _, g := range order {
		left += "   " + glyphColor(g) + " " + dim(fmt.Sprint(counts[g]))
	}
	right := dim("all ok")
	if !first.IsZero() && !last.IsZero() {
		right += "   " + dim(dur(last.Sub(first)))
	}
	d.add(ref, "", left, right)
}

// prose is Claude's narration: the work axis, secondary colour.
func (d *drawer) prose(s string, indent int, c string) {
	for _, para := range strings.Split(strings.TrimSpace(s), "\n") {
		if strings.TrimSpace(para) == "" {
			continue
		}
		// A paragraph already drawn this way comes from the memo, so text
		// streaming in only wraps its last paragraph again.
		k := memoKey{text: para, style: c, spine: d.spine(), n: indent, width: d.o.Width, cw: d.cw}
		if ls, ok := d.s.memoGet(k); ok {
			d.lines = append(d.lines, ls...)
			continue
		}
		from := len(d.lines)
		for _, r := range wrap(paint(c, inline(para, c)), min(d.cw-indent-1, capProse)) {
			d.add("", "", d.spine()+blanks(indent-1)+r, "")
		}
		d.s.memoPut(k, d.lines[from:])
	}
}

// answer is the turn's final words: the conversation axis, full text colour,
// with light markdown.
func (d *drawer) answer(s string) {
	if n := len(d.lines); n > 0 && strings.TrimSpace(stripANSI(d.lines[n-1].Text)) != strings.TrimSpace(stripANSI(d.spine())) {
		d.blank()
	}
	w := min(d.cw-5, capProse)
	inFence := false
	for _, ln := range strings.Split(strings.TrimRight(s, " \t\n"), "\n") {
		trim := strings.TrimSpace(ln)
		if strings.HasPrefix(trim, "```") {
			inFence = !inFence
			continue
		}
		pad := d.spine() + "   "
		switch {
		case inFence:
			d.add("", bgWell, pad+" "+sub(ln), "")
			continue
		case trim == "":
			d.blank()
			continue
		case strings.HasPrefix(trim, "#"):
			d.add("", "", pad+paint(cWhite+bold, strings.TrimSpace(strings.TrimLeft(trim, "#"))), "")
			continue
		}
		k := memoKey{text: trim, style: "answer", spine: d.spine(), width: d.o.Width, cw: d.cw}
		if ls, ok := d.s.memoGet(k); ok {
			d.lines = append(d.lines, ls...)
			continue
		}
		from := len(d.lines)
		lead, body := "", trim
		if strings.HasPrefix(trim, "- ") || strings.HasPrefix(trim, "* ") {
			lead, body = dim("•")+" ", trim[2:]
		} else if m := numbered.FindStringSubmatch(trim); m != nil {
			lead, body = dim(m[1])+" ", m[2]
		}
		leadW := len([]rune(stripANSI(lead)))
		for k, r := range wrap(text(inline(body, cText)), w-leadW) {
			if k > 0 && lead != "" {
				r = blanks(leadW) + r
			} else {
				r = lead + r
			}
			d.add("", "", pad+r, "")
		}
		d.s.memoPut(k, d.lines[from:])
	}
}

// memoKey names a paragraph as drawn: its text, how, and at what width.
type memoKey struct {
	text, style, spine string
	n, width, cw       int
}

// memoTurn ages the paragraph memo: what the last two renders drew stays,
// the rest goes.
func (s *Session) memoTurn() {
	s.memoOld, s.memo = s.memo, make(map[memoKey][]Line, len(s.memo))
}

func (s *Session) memoGet(k memoKey) ([]Line, bool) {
	if ls, ok := s.memo[k]; ok {
		return ls, true
	}
	if ls, ok := s.memoOld[k]; ok && s.memo != nil {
		s.memo[k] = ls
		return ls, true
	}
	return nil, false
}

func (s *Session) memoPut(k memoKey, ls []Line) {
	if s.memo != nil {
		s.memo[k] = append([]Line(nil), ls...)
	}
}

const spaces = "                                                                                                                                "

// blanks is n spaces.
func blanks(n int) string {
	if n <= 0 {
		return ""
	}
	if n <= len(spaces) {
		return spaces[:n]
	}
	return strings.Repeat(" ", n)
}

// styledAsk draws your words with the things that act singled out: a
// shell command tinted, slash commands and @files in bold white, image and
// paste markers as chips, links underlined.
func styledAsk(s, base string) string {
	if cmd, ok := strings.CutPrefix(s, "! "); ok {
		return paint(cWhite+bold, "$ ") + tint(cmd)
	}
	s = specialRe.ReplaceAllStringFunc(s, func(m string) string {
		switch {
		case strings.HasPrefix(m, "[Image"):
			return reset + paint(cBlue, "▣ "+strings.Trim(m, "[]")) + base
		case strings.HasPrefix(m, "[Pasted"):
			return reset + paint(cBlue, "▤ "+strings.Trim(m, "[]")) + base
		case strings.HasPrefix(m, "http"):
			return reset + link(m) + base
		}
		return reset + paint(cWhite+bold, m) + base
	})
	return paint(base, inline(s, base))
}

var specialRe = regexp.MustCompile(`\[Image #\d+\]|\[Pasted text #\d+[^\]]*\]|https?://[^\s)>\]]+|(^|\s)/[a-z][\w:-]*(?:$|[\s.,;:!?)])|@[\w./-]+`)

// link underlines a URL and makes it clickable in terminals that support
// OSC 8 hyperlinks.
func link(url string) string {
	return "\x1b]8;;" + url + "\x1b\\" + paint(cBlue+"\x1b[4m", url) + "\x1b]8;;\x1b\\"
}

var numbered = regexp.MustCompile(`^(\d+\.)\s+(.*)$`)

// inline styles **bold** and `code`, returning to base colour after each,
// and links URLs. Byte scans, the same as replacing \*\*([^*]+)\*\*,
// `([^`]+)` and https?://[^\s)>\]"'`]+ in turn.
func inline(s, base string) string {
	if !strings.ContainsAny(s, "*`h") {
		return s
	}
	s = pairs(s, "**", bold, reset+base)
	s = pairs(s, "`", cWhite, reset+base)
	return links(s, base)
}

// pairs wraps text between two delimiters in open and close, dropping the
// delimiters, where the text is at least one character and holds no
// delimiter character.
func pairs(s, delim, open, close string) string {
	d := delim[0]
	var b strings.Builder
	last := 0
	for p := 0; ; {
		i := strings.Index(s[p:], delim)
		if i < 0 {
			break
		}
		i += p
		from := i + len(delim)
		j := strings.IndexByte(s[from:], d)
		if j <= 0 || !strings.HasPrefix(s[from+j:], delim) {
			p = i + 1
			continue
		}
		if b.Len() == 0 {
			b.Grow(len(s) + 32)
		}
		b.WriteString(s[last:i])
		b.WriteString(open)
		b.WriteString(s[from : from+j])
		b.WriteString(close)
		last = from + j + len(delim)
		p = last
	}
	if last == 0 {
		return s
	}
	b.WriteString(s[last:])
	return b.String()
}

// links makes each URL a link, returning to base colour after it.
func links(s, base string) string {
	var b strings.Builder
	last := 0
	for p := 0; ; {
		i := strings.Index(s[p:], "http")
		if i < 0 {
			break
		}
		i += p
		j := i + 4
		if j < len(s) && s[j] == 's' && strings.HasPrefix(s[j+1:], "://") {
			j += 4
		} else if strings.HasPrefix(s[j:], "://") {
			j += 3
		} else {
			p = i + 1
			continue
		}
		end := j
		for end < len(s) && !urlStop(s[end]) {
			end++
		}
		if end == j {
			p = i + 1
			continue
		}
		b.WriteString(s[last:i])
		b.WriteString(reset)
		b.WriteString(link(s[i:end]))
		b.WriteString(base)
		last, p = end, end
	}
	if last == 0 {
		return s
	}
	b.WriteString(s[last:])
	return b.String()
}

func urlStop(c byte) bool {
	switch c {
	case '\t', '\n', '\f', '\r', ' ', ')', '>', ']', '"', '\'', '`':
		return true
	}
	return false
}

// stripANSI drops colour codes (ESC [ digits and semicolons m), as ansiRe
// would, with a byte scan.
func stripANSI(s string) string {
	i := strings.IndexByte(s, 0x1b)
	if i < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i >= 0 {
		b.WriteString(s[:i])
		j := i + 1
		if j < len(s) && s[j] == '[' {
			j++
			for j < len(s) && (s[j] >= '0' && s[j] <= '9' || s[j] == ';') {
				j++
			}
			if j < len(s) && s[j] == 'm' {
				s = s[j+1:]
				i = strings.IndexByte(s, 0x1b)
				continue
			}
		}
		b.WriteByte(0x1b)
		s = s[i+1:]
		i = strings.IndexByte(s, 0x1b)
	}
	b.WriteString(s)
	return b.String()
}

// foldable steps are the clean, routine ones a finished turn can fold into
// a single row. Edits never fold: they're what you'd review.
func foldable(st *Step) bool {
	if st.Status != OK || hidden(st) {
		return false
	}
	switch glyphFor(st.Tool) {
	case "✎", "⇉", "◆":
		return false
	}
	return true
}

// hidden steps are bookkeeping the task line already shows.
func hidden(st *Step) bool {
	switch st.Tool {
	case "TodoWrite", "TaskCreate", "TaskUpdate", "TaskList", "TaskGet":
		return true
	}
	return false
}

func (d *drawer) statusMark(st *Step) string {
	switch st.Status {
	case Running:
		return paint(cOrange, spinner[(d.o.Tick+len(st.ID))%len(spinner)])
	case OK:
		return paint(cGreen, "✓")
	case Failed:
		return paint(cRed, "✗")
	case Waiting:
		return paint(cYellow, "●")
	case Denied:
		return dim("⊘")
	default:
		return dim("◌")
	}
}

func (d *drawer) step(st *Step, depth int) {
	if hidden(st) {
		return
	}
	ref := d.ref + ":s:" + st.ID
	indent := 4 + depth*4
	left := d.spine() + strings.Repeat(" ", indent-1) + d.statusMark(st) + " " + d.label(st)
	d.add(ref, "", left, d.cells(st))

	open := st.Status == Failed || d.o.Verbose
	if v, ok := d.o.Open[ref]; ok {
		open = v
	}
	if open {
		d.body(st, indent+4)
	}
	// A subagent shows its own steps while it works, or when opened.
	if len(st.Children) > 0 && (st.Status == Running || open) {
		for _, c := range st.Children {
			d.step(c, depth+1)
		}
	}
}

func (d *drawer) cells(st *Step) string {
	var parts []string
	if st.Status == Waiting {
		wait := ""
		if !st.Start.IsZero() {
			wait = "  " + dim(dur(d.o.Now.Sub(st.Start)))
		}
		return paint(cYellow, "waiting on you") + wait
	}
	if s := d.summary(st); s != "" {
		parts = append(parts, s)
	}
	switch {
	case st.Status == Running && !st.Start.IsZero():
		parts = append(parts, paint(cOrange, dur(d.o.Now.Sub(st.Start))))
	case !st.End.IsZero() && !st.Start.IsZero() && st.End.Sub(st.Start) >= 100*time.Millisecond:
		parts = append(parts, dim(dur(st.End.Sub(st.Start))))
	}
	if st.Tool == "Bash" && st.Exit > 0 {
		switch st.Exit {
		case 124:
			parts = append(parts, paint(cRed, "timed out"))
		case 137, 143:
			parts = append(parts, paint(cRed, "killed"))
		default:
			parts = append(parts, paint(cRed, fmt.Sprintf("exit %d", st.Exit)))
		}
	}
	return strings.Join(parts, "   ")
}

// --- labels ---

type input map[string]any

func (in input) str(k string) string {
	v, _ := in[k].(string)
	return v
}

func readInput(raw json.RawMessage) input {
	var m input
	_ = json.Unmarshal(raw, &m)
	return m
}

func glyphFor(tool string) string {
	switch tool {
	case "Bash":
		return "$"
	case "Edit", "MultiEdit", "Write", "NotebookEdit":
		return "✎"
	case "Read":
		return "◧"
	case "Grep", "Glob":
		return "⌕"
	case "Task", "Agent":
		return "⇉"
	case "WebFetch", "WebSearch":
		return "↗"
	case "Artifact":
		return "◆"
	case "Skill", "SlashCommand":
		return "✦"
	}
	return "•"
}

func glyphColor(g string) string {
	switch g {
	case "$":
		return paint(cWhite, g)
	case "✎", "⇉", "◆", "↗":
		return paint(cBlue, g)
	}
	return sub(g)
}

// rel shortens a path to the session's folder when it's inside it, looking
// through symlinks such as macOS's /tmp → /private/tmp.
func (d *drawer) rel(p string) string {
	for _, base := range d.s.bases() {
		if r, err := filepath.Rel(base, p); err == nil && !strings.HasPrefix(r, "..") {
			return r
		}
	}
	return p
}

func (d *drawer) label(st *Step) string {
	in := readInput(st.Input)
	g := glyphColor(glyphFor(st.Tool))
	switch st.Tool {
	case "Bash":
		// What the command is for reads faster than the command; the command
		// itself follows, quieter, and shows whole when the row is opened.
		if desc := oneLine(in.str("description")); desc != "" {
			return g + " " + text(desc) + "  " + faint(program(in.str("command")))
		}
		return g + " " + d.command(in.str("command"))
	case "Edit", "MultiEdit", "Write", "NotebookEdit":
		p := in.str("file_path")
		if p == "" {
			p = in.str("notebook_path")
		}
		return g + " " + text(d.rel(p))
	case "Read":
		return g + " " + text(d.rel(in.str("file_path")))
	case "Grep":
		l := g + " " + text(in.str("pattern"))
		if p := in.str("path"); p != "" {
			l += dim(" in ") + text(d.rel(p))
		}
		return l
	case "Glob":
		return g + " " + text(in.str("pattern"))
	case "Task", "Agent":
		kind := in.str("subagent_type")
		if kind == "" {
			kind = "subagent"
		}
		return g + " " + text(kind) + "  " + sub(oneLine(in.str("description")))
	case "WebFetch":
		return g + " " + text(in.str("url"))
	case "WebSearch":
		return g + " " + text(in.str("query"))
	case "Skill", "SlashCommand":
		name := firstNonEmpty(in.str("skill"), in.str("command"), in.str("name"))
		return paint(cWhite, "✦") + " " + paint(cWhite+bold, name) + "  " + dim(oneLine(in.str("args")))
	case "AskUserQuestion":
		q := ""
		if qs, ok := in["questions"].([]any); ok && len(qs) > 0 {
			if m, ok := qs[0].(map[string]any); ok {
				q, _ = m["question"].(string)
			}
		}
		return paint(cYellow, "?") + " " + text(oneLine(q))
	case "Artifact":
		t := in.str("title")
		if t == "" {
			t = filepath.Base(in.str("file_path"))
		}
		return g + " " + text(t)
	}
	name := st.Tool
	if strings.HasPrefix(name, "mcp__") {
		parts := strings.SplitN(strings.TrimPrefix(name, "mcp__"), "__", 2)
		if len(parts) == 2 {
			name = parts[1] + dim(" · "+parts[0])
		}
	}
	return g + " " + text(name) + "  " + sub(firstValue(in))
}

func firstValue(in input) string {
	for _, k := range []string{"description", "prompt", "query", "path", "url", "name", "skill"} {
		if v := in.str(k); v != "" {
			return oneLine(v)
		}
	}
	return ""
}

var cdRe = regexp.MustCompile(`^cd\s+("[^"]+"|'[^']+'|\S+)\s*&&\s*`)

// command tints a shell command so its shape shows: the program bright,
// flags quieter, strings green, variables blue, operators orange.
func (d *drawer) command(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	// Paths inside the session's folder read better relative to it.
	for _, base := range d.s.bases() {
		cmd = strings.ReplaceAll(cmd, base+"/", "")
	}
	lines := strings.Split(cmd, "\n")
	first := lines[0]
	chip := ""
	if m := cdRe.FindStringSubmatch(first); m != nil {
		dir := strings.Trim(m[1], `"'`)
		first = first[len(m[0]):]
		if dir != d.s.Info.Cwd && dir != "." {
			chip = bgSel + dim(" in ") + sub(d.rel(dir)+" ") + reset + " "
		}
	}
	out := chip + tint(first)
	if n := len(lines) - 1; n > 0 {
		out += "  " + bgSel + dim(fmt.Sprintf(" +%d lines ", n)) + reset
	}
	return out
}

// program is a command's first word, or first two for git, go, npm and the
// like, so a row can say "git status" without the whole pipeline.
func program(cmd string) string {
	cmd = strings.TrimSpace(cdRe.ReplaceAllString(strings.TrimSpace(cmd), ""))
	f := strings.Fields(strings.SplitN(cmd, "\n", 2)[0])
	// Leading VAR=value assignments aren't the program.
	for len(f) > 1 && strings.Contains(f[0], "=") && !strings.HasPrefix(f[0], "-") {
		f = f[1:]
	}
	if len(f) == 0 {
		return ""
	}
	p := f[0]
	switch p {
	case "git", "go", "npm", "pnpm", "yarn", "cargo", "docker", "kubectl", "gh", "make", "uv", "bun":
		if len(f) > 1 && !strings.HasPrefix(f[1], "-") {
			p += " " + f[1]
		}
	}
	if strings.ContainsAny(cmd, "|;&") {
		p += " …"
	}
	return p
}

var shOps = map[string]bool{"&&": true, "||": true, "|": true, ";": true, ">": true, ">>": true, "<": true, "2>&1": true, "&": true}

func tint(cmd string) string {
	var b strings.Builder
	head := true
	for _, tok := range shellTokens(cmd) {
		switch {
		case strings.TrimSpace(tok) == "":
			b.WriteString(tok)
		case shOps[tok]:
			b.WriteString(paint(cOrange, tok))
			head = true
		case strings.HasPrefix(tok, `"`) || strings.HasPrefix(tok, `'`):
			b.WriteString(paint(cGreen, tok))
			head = false
		case strings.HasPrefix(tok, "$"):
			b.WriteString(paint(cBlue, tok))
			head = false
		case head:
			b.WriteString(paint(cText+bold, tok))
			head = false
		case strings.HasPrefix(tok, "-"):
			b.WriteString(sub(tok))
		default:
			b.WriteString(text(tok))
		}
	}
	return b.String()
}

// shellTokens splits on spaces, keeping quoted strings and spaces whole.
func shellTokens(s string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' || c == '\'':
			flush()
			j := strings.IndexByte(s[i+1:], c)
			if j < 0 {
				out = append(out, s[i:])
				return out
			}
			out = append(out, s[i:i+j+2])
			i += j + 1
		case c == ' ' || c == '\t':
			flush()
			out = append(out, string(c))
		case c == ';' || c == '|' || c == '&':
			flush()
			if i+1 < len(s) && (s[i+1] == c) {
				out = append(out, s[i:i+2])
				i++
			} else {
				out = append(out, string(c))
			}
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return out
}

// --- summaries ---

var (
	goOK   = regexp.MustCompile(`(?m)^ok\s+\S+`)
	goFail = regexp.MustCompile(`(?m)^(FAIL\s+\S+|--- FAIL)`)
	jsSum  = regexp.MustCompile(`Tests?\s+(?:\x1b\[[0-9;]*m)?(?:(\d+) failed[^|\n]*\|\s*)?(\d+) passed`)
)

func (d *drawer) summary(st *Step) string {
	switch st.Tool {
	case "Bash":
		out := bashOut(st)
		if n := len(goFail.FindAllString(out, -1)); n > 0 {
			return paint(cRed, fmt.Sprintf("%d failed", n))
		}
		if m := jsSum.FindStringSubmatch(out); m != nil {
			if m[1] != "" && m[1] != "0" {
				return paint(cRed, m[1]+" failed") + dim(" · "+m[2]+" passed")
			}
			return sub(m[2] + " passed")
		}
		if n := len(goOK.FindAllString(out, -1)); n > 0 {
			return sub(fmt.Sprintf("%d ok", n))
		}
		if st.Status == OK {
			if n := countLines(out); n > 1 {
				return dim(fmt.Sprintf("%d lines", n))
			}
		}
	case "Edit", "MultiEdit", "Write":
		var r struct {
			Type    string `json:"type"`
			Content string `json:"content"`
		}
		_ = json.Unmarshal(st.Result, &r)
		if r.Type == "create" {
			return paint(cGreen, fmt.Sprintf("new · %d lines", countLines(r.Content)))
		}
		add, del := 0, 0
		for _, p := range headless.Patches(st.Result) {
			for _, l := range p.Lines {
				switch {
				case strings.HasPrefix(l, "+"):
					add++
				case strings.HasPrefix(l, "-"):
					del++
				}
			}
		}
		if add+del > 0 {
			return paint(cGreen, fmt.Sprintf("+%d", add)) + " " + paint(cRed, fmt.Sprintf("−%d", del))
		}
	case "Read":
		var r struct {
			File struct {
				NumLines   int `json:"numLines"`
				StartLine  int `json:"startLine"`
				TotalLines int `json:"totalLines"`
			} `json:"file"`
		}
		if json.Unmarshal(st.Result, &r) == nil && r.File.NumLines > 0 {
			f := r.File
			if f.NumLines < f.TotalLines {
				return dim(fmt.Sprintf("lines %d–%d", f.StartLine, f.StartLine+f.NumLines-1))
			}
			return dim(fmt.Sprintf("%d lines", f.TotalLines))
		}
	case "Grep", "Glob":
		if st.Status == OK {
			n := countLines(st.Output)
			if strings.HasPrefix(strings.TrimSpace(st.Output), "No ") {
				n = 0
			}
			noun := "results"
			if st.Tool == "Glob" {
				noun = "files"
			}
			return dim(fmt.Sprintf("%d %s", n, noun))
		}
	case "Task", "Agent":
		if n := len(st.Children); n > 0 {
			return dim(plural(n, "step"))
		}
	}
	return ""
}

func bashOut(st *Step) string {
	var r struct {
		Stdout string `json:"stdout"`
		Stderr string `json:"stderr"`
	}
	if json.Unmarshal(st.Result, &r) == nil && (r.Stdout != "" || r.Stderr != "") {
		return r.Stdout + "\n" + r.Stderr
	}
	return st.Output
}

func countLines(s string) int {
	s = strings.TrimRight(s, "\n")
	if strings.TrimSpace(s) == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

// --- bodies ---

func (d *drawer) body(st *Step, indent int) {
	switch st.Tool {
	case "Edit", "MultiEdit", "Write":
		if d.diff(st, indent) {
			return
		}
	case "Bash":
		if cmd := readInput(st.Input).str("command"); cmd != "" && readInput(st.Input).str("description") != "" {
			pad := d.spine() + strings.Repeat(" ", indent-1)
			for _, l := range strings.Split(strings.TrimSpace(cmd), "\n") {
				d.add("", bgWell, pad+faint("$ ")+tint(truncateCells(expandTabs(l), d.cw-indent-4)), "")
			}
		}
		var r struct {
			Stdout string `json:"stdout"`
			Stderr string `json:"stderr"`
		}
		if json.Unmarshal(st.Result, &r) == nil && (r.Stdout != "" || r.Stderr != "") {
			d.output(r.Stdout, indent, st.Status == Failed && r.Stderr == "")
			if strings.TrimSpace(r.Stderr) != "" {
				d.add("", "", d.spine()+strings.Repeat(" ", indent-1)+dim(fmt.Sprintf("stderr · %d lines", countLines(r.Stderr))), "")
				d.output(r.Stderr, indent, st.Exit != 0)
			}
			return
		}
	}
	d.output(strings.TrimLeft(exitRe.ReplaceAllString(st.Output, ""), "\n"), indent, st.Status == Failed)
}

// output draws text in a well: head and tail when it's long, all of it in
// verbose mode, tinted red when it's a failure.
func (d *drawer) output(s string, indent int, failed bool) {
	s = collapseCR(strings.TrimRight(s, "\n"))
	if strings.TrimSpace(s) == "" {
		return
	}
	lines := strings.Split(s, "\n")
	b, edge := bgWell, faint("▏")
	if failed {
		b, edge = bgErr, paint(cRed, "▎")
	}
	pad := d.spine() + strings.Repeat(" ", indent-1)
	w := d.cw - indent - 2
	emit := func(l string) {
		d.add("", b, pad+edge+sub(truncateCells(expandTabs(l), w)), "")
	}
	if !d.o.Verbose && len(lines) > 15 {
		for _, l := range lines[:3] {
			emit(l)
		}
		d.add("", b, pad+edge+dim(fmt.Sprintf("… %d lines · ctrl+o shows all", len(lines)-15)), "")
		for _, l := range lines[len(lines)-12:] {
			emit(l)
		}
		return
	}
	for i, l := range lines {
		if i >= 2000 {
			d.add("", b, pad+edge+dim(fmt.Sprintf("… %d more lines", len(lines)-i)), "")
			break
		}
		emit(l)
	}
}

func (d *drawer) diff(st *Step, indent int) bool {
	var r struct {
		Type    string `json:"type"`
		Content string `json:"content"`
	}
	_ = json.Unmarshal(st.Result, &r)
	pad := d.spine() + strings.Repeat(" ", indent-1)
	w := d.cw - indent - 9
	if r.Type == "create" {
		lines := strings.Split(strings.TrimRight(r.Content, "\n"), "\n")
		for i, l := range lines {
			if i >= 20 && !d.o.Verbose {
				d.add("", bgWell, pad+paint(cGreen, "▏")+dim(fmt.Sprintf("… %d more lines", len(lines)-i)), "")
				break
			}
			d.add("", bgWell, pad+paint(cGreen, "▏")+dim(fmt.Sprintf("%5d ", i+1))+sub(truncateCells(expandTabs(l), w)), "")
		}
		return true
	}
	patches := headless.Patches(st.Result)
	if len(patches) == 0 {
		return false
	}
	shown := 0
	for pi, p := range patches {
		if pi > 0 {
			d.add("", bgWell, pad+dim("  ..."), "")
		}
		oldN, newN := p.OldStart, p.NewStart
		for _, l := range p.Lines {
			if shown >= 30 && !d.o.Verbose {
				d.add("", bgWell, pad+dim("  … ctrl+o shows the rest"), "")
				return true
			}
			shown++
			if l == "" {
				l = " "
			}
			body := truncateCells(expandTabs(l[1:]), w)
			switch l[0] {
			case '+':
				d.add("", bgAdd, pad+dim(fmt.Sprintf("%5d ", newN))+paint(cGreen, "+")+" "+text(body), "")
				newN++
			case '-':
				d.add("", bgDel, pad+dim(fmt.Sprintf("%5d ", oldN))+paint(cRed, "−")+" "+text(body), "")
				oldN++
			default:
				d.add("", bgWell, pad+dim(fmt.Sprintf("%5d ", newN))+"  "+sub(body), "")
				oldN++
				newN++
			}
		}
	}
	return true
}

// collapseCR keeps only the last state of lines redrawn with carriage
// returns, so progress bars show where they ended.
func collapseCR(s string) string {
	if !strings.Contains(s, "\r") {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if j := strings.LastIndex(strings.TrimRight(l, "\r"), "\r"); j >= 0 {
			lines[i] = l[j+1:]
		}
	}
	return strings.Join(lines, "\n")
}

func expandTabs(s string) string { return strings.ReplaceAll(stripANSI(s), "\t", "    ") }

func truncateCells(s string, w int) string {
	if w < 4 {
		w = 4
	}
	r := []rune(s)
	if len(r) > w {
		return string(r[:w-1]) + "›"
	}
	return s
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
