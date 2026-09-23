package convo

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
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
	key   string
	lines []Line
}

// Render draws every turn, oldest first.
func (s *Session) Render(o Options) []Line {
	if o.Width < 20 {
		o.Width = 20
	}
	var out []Line
	for i, t := range s.Turns {
		recent := i >= len(s.Turns)-2
		out = append(out, s.turn(t, o, recent)...)
	}
	return out
}

func (s *Session) turn(t *Turn, o Options, recent bool) []Line {
	ref := fmt.Sprintf("t%d", t.N)
	open := recent || t.Live || t.Err != ""
	if v, ok := o.Open[ref]; ok {
		open = v
	}
	key := s.cacheKey(t, o, ref, open)
	if c, ok := s.cache[t]; ok && c.key == key {
		return c.lines
	}
	d := drawer{s: s, t: t, o: o, ref: ref, cw: min(o.Width, capRow)}
	if open {
		d.open()
	} else {
		d.folded()
	}
	d.blank()
	s.cache[t] = cached{key: key, lines: d.lines}
	return d.lines
}

// cacheKey changes whenever anything that affects this turn's drawing does:
// its content, the width, its folds, the selection inside it, and for a live
// turn the clock.
func (s *Session) cacheKey(t *Turn, o Options, ref string, open bool) string {
	var folds []string
	for k, v := range o.Open {
		if k == ref || strings.HasPrefix(k, ref+":") {
			folds = append(folds, fmt.Sprintf("%s=%v", k, v))
		}
	}
	sort.Strings(folds)
	sel := ""
	if o.Selected == ref || strings.HasPrefix(o.Selected, ref+":") {
		sel = fmt.Sprintf("%s/%v", o.Selected, o.Focused)
	}
	clock := ""
	if t.Live || waiting(t) {
		clock = fmt.Sprintf("%d/%d", o.Tick, o.Now.Unix())
	}
	return fmt.Sprintf("%d|%d|%v|%v|%s|%s|%s", o.Width, t.ver, open, o.Verbose, strings.Join(folds, ","), sel, clock)
}

func waiting(t *Turn) bool {
	for _, st := range t.steps {
		if st.Status == Waiting {
			return true
		}
	}
	return false
}

type drawer struct {
	s     *Session
	t     *Turn
	o     Options
	ref   string
	cw    int
	lines []Line
}

func (d *drawer) spine() string {
	switch {
	case d.t.Live:
		return paint(cOrange, "▏")
	case d.t.Err != "":
		return paint(cRed, "▏")
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
		ask = "(resumed)"
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
	left := "  " + faint("▸") + " " + d.mark() + " " + dim(fmt.Sprintf("#%d", t.N)) + "  " + sub(ask)
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
	if ask == "" {
		ask = "(resumed)"
	}
	headW := max(20, d.cw-11-len([]rune(stripANSI(right)))-2)
	rows := wrap(paint(cWhite+bold, oneLine(ask)), min(headW, capProse))
	if len(rows) > 3 {
		rows = append(rows[:2], rows[2]+dim(" …"))
	}
	for i, r := range rows {
		if i == 0 {
			d.add(d.ref, band, d.spine()+" "+faint("▾")+" "+d.mark()+" "+dim("you")+"  "+r, right)
		} else {
			d.add(d.ref, band, d.spine()+"          "+r, "")
		}
	}
	d.blank()

	items := t.Items
	for i := 0; i < len(items); i++ {
		it := items[i]
		// A run of three or more clean steps folds to one row once the turn
		// is over; a failure never folds.
		if !t.Live && !d.o.Verbose && it.Kind == KStep {
			j := i
			for j < len(items) && items[j].Kind == KStep && foldable(items[j].Step) {
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
		for _, r := range wrap(paint(c, inline(para, c)), min(d.cw-indent-1, capProse)) {
			d.add("", "", d.spine()+strings.Repeat(" ", indent-1)+r, "")
		}
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
		lead, body := "", trim
		if strings.HasPrefix(trim, "- ") || strings.HasPrefix(trim, "* ") {
			lead, body = dim("•")+" ", trim[2:]
		} else if m := numbered.FindStringSubmatch(trim); m != nil {
			lead, body = dim(m[1])+" ", m[2]
		}
		for k, r := range wrap(text(inline(body, cText)), w-len([]rune(stripANSI(lead)))) {
			if k > 0 && lead != "" {
				r = strings.Repeat(" ", len([]rune(stripANSI(lead)))) + r
			} else {
				r = lead + r
			}
			d.add("", "", pad+r, "")
		}
	}
}

var (
	numbered = regexp.MustCompile(`^(\d+\.)\s+(.*)$`)
	boldRe   = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	codeRe   = regexp.MustCompile("`([^`]+)`")
	ansiRe   = regexp.MustCompile(`\x1b\[[0-9;]*m`)
)

// inline styles **bold** and `code`, returning to base colour after each.
func inline(s, base string) string {
	s = boldRe.ReplaceAllString(s, bold+"$1"+reset+base)
	s = codeRe.ReplaceAllString(s, cWhite+"$1"+reset+base)
	return s
}

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

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
