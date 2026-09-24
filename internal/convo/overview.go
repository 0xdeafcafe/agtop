package convo

import (
	"github.com/charmbracelet/x/ansi"
	"math"

	"fmt"
	"github.com/0xdeafcafe/agtop/internal/cellw"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/0xdeafcafe/agtop/internal/claude"
)

// ColdStart is a request that wrote the prompt cache instead of reading it.
// Most are expected: a session's first request, each subagent's first, a
// model switch. The ones worth a look are the others.
type ColdStart struct {
	At       time.Time
	Agent    string
	Gap      time.Duration // since that agent's previous request
	Written  int           // tokens written to the cache
	Reason   string
	Expected bool // part of how sessions and subagents work
}

// cacheHour is how long the prompt cache lives (Claude Code writes the
// one-hour cache).
const cacheHour = time.Hour

func (s *Session) ColdStarts() []ColdStart {
	type last struct {
		at    time.Time
		model string
	}
	prev := map[string]last{}
	var out []ColdStart
	for _, r := range s.Requests {
		u := r.Usage
		total := u.CacheReadInputTokens + u.CacheCreationInputTokens
		cold := u.CacheCreationInputTokens > 4096 && u.CacheReadInputTokens*5 < total
		p, seen := prev[r.Run]
		prev[r.Run] = last{r.At, r.Model}
		if !cold {
			continue
		}
		c := ColdStart{At: r.At, Agent: r.Agent, Written: u.CacheCreationInputTokens}
		switch {
		case !seen && r.Run != "":
			c.Reason, c.Expected = "new subagent: every subagent starts its own cache", true
		case !seen:
			c.Reason, c.Expected = "session start", true
		case p.model != "" && r.Model != "" && p.model != r.Model:
			c.Gap = r.At.Sub(p.at)
			c.Reason, c.Expected = "model changed, and each model has its own cache", true
		default:
			c.Gap = r.At.Sub(p.at)
			if c.Gap >= cacheHour-5*time.Minute {
				c.Reason, c.Expected = "idle past the cache's hour", true
			} else {
				c.Reason = "the cache was dropped early"
			}
		}
		out = append(out, c)
	}
	return out
}

// Totals across the whole session.
type Totals struct {
	Cost      float64
	Turns     int
	Working   time.Duration
	ToolCalls int
	Failed    int
	In, Out   int
	CacheRead int
	CacheOut  int
	Requests  int
}

func (s *Session) Totals(now time.Time) Totals {
	var t Totals
	for _, tn := range s.Turns {
		t.Turns++
		t.Cost += tn.Cost
		if !tn.Start.IsZero() {
			end := tn.End
			if tn.Live {
				end = now
			}
			t.Working += end.Sub(tn.Start)
		}
	}
	if s.Info.CostUSD > t.Cost {
		t.Cost = s.Info.CostUSD
	}
	for _, ts := range s.Tools {
		t.ToolCalls += ts.Calls
		t.Failed += ts.Failed
	}
	for _, r := range s.Requests {
		t.Requests++
		t.In += r.Usage.InputTokens
		t.Out += r.Usage.OutputTokens
		t.CacheRead += r.Usage.CacheReadInputTokens
		t.CacheOut += r.Usage.CacheCreationInputTokens
	}
	return t
}

var modelDate = regexp.MustCompile(`-\d{8}$`)

// PrettyModel turns claude-opus-5-5[1m] into "opus 5.5 · 1M".
func PrettyModel(m string) string {
	if m == "" {
		return "?"
	}
	long := strings.Contains(m, "[1m]")
	m = strings.TrimPrefix(strings.ReplaceAll(m, "[1m]", ""), "claude-")
	m = modelDate.ReplaceAllString(m, "")
	parts := strings.Split(m, "-")
	out := parts[0]
	if len(parts) > 1 {
		out += " " + strings.Join(parts[1:], ".")
	}
	if long {
		out += " · 1M"
	}
	return out
}

func tokens(n int) string {
	switch {
	case n >= 1_000_000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1e6), ".0") + "M"
	case n >= 1000:
		return fmt.Sprintf("%.0fk", float64(n)/1e3)
	}
	return fmt.Sprint(n)
}

func bar(frac float64, w int, c string) string {
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	n := int(frac*float64(w) + 0.5)
	return paint(c, strings.Repeat("▰", n)) + faint(strings.Repeat("▱", w-n))
}

// Overview draws the session's numbers as sections: what's running now,
// totals, model and effort over time, tools, the cache, and subagents.
func (s *Session) Overview(o Options) []Line {
	w := min(o.Width, capRow)
	var out []Line
	add := func(left, right string) {
		out = append(out, Line{Text: row("", left, right, o.Width, w)})
	}
	section := func(title, meta string) {
		if len(out) > 0 {
			add("", "")
		}
		head := "  " + faint("▾") + " " + paint(cSub+bold, title)
		if meta != "" {
			head += "  " + dim(meta)
		}
		n := w - len([]rune(stripANSI(head))) - 2
		add(head+" "+faint(strings.Repeat("─", max(0, n))), "")
	}
	label := func(k string) string { return "    " + dim(fmt.Sprintf("%-13s", k)) }

	t := s.Totals(o.Now)
	model := s.Info.Model
	for i := len(s.Requests) - 1; i >= 0; i-- {
		if s.Requests[i].Agent == "" && s.Requests[i].Model != "" {
			model = s.Requests[i].Model
			break
		}
	}
	model = firstNonEmpty(model, s.Model)
	effort := firstNonEmpty(s.Info.Effort, "default")

	// --- tiles: the numbers that matter, at a glance ---
	hit := 0.0
	if all := t.CacheRead + t.CacheOut + t.In; all > 0 {
		hit = float64(t.CacheRead) / float64(all)
	}
	ctx, ctxLabel := "—", "context"
	ctxCol := cText
	if s.Context > 0 && model != "" {
		win := claude.ContextWindow(model)
		pct := float64(s.Context) / float64(win)
		ctx, ctxLabel = fmt.Sprintf("%.0f%%", pct*100), "context of "+tokens(int(win))
		switch {
		case pct >= 0.8:
			ctxCol = cRed
		case pct >= 0.5:
			ctxCol = cYellow
		}
	}
	calls := paint(cText+bold, fmt.Sprint(t.ToolCalls))
	if t.Failed > 0 {
		calls += "  " + paint(cRed, fmt.Sprintf("✗%d", t.Failed))
	}
	spent := t.Cost
	if spent == 0 {
		spent = s.Cost() // a transcript carries no totals: price each call
	}
	tiles := []struct{ value, label string }{
		{paint(cText+bold, firstNonEmpty(money(spent), "$0")), dim("spent")},
		{paint(cText+bold, dur(t.Working)), dim("working")},
		{paint(cText+bold, fmt.Sprint(t.Turns)), dim("turns")},
		{calls, dim("tool calls")},
		{paint(ctxCol+bold, ctx), dim(ctxLabel)},
		{paint(cText+bold, fmt.Sprintf("%.0f%%", hit*100)), dim("from cache")},
	}
	per := max(3, min(len(tiles), (w-4)/16))
	tw := (w - 4) / per
	for start := 0; start < len(tiles); start += per {
		var top, bottom strings.Builder
		for _, tl := range tiles[start:min(len(tiles), start+per)] {
			top.WriteString(" " + fitTo(tl.value, tw-2) + " ")
			bottom.WriteString(" " + fitTo(tl.label, tw-2) + " ")
		}
		out = append(out, Line{Text: row(bgWell, "  "+top.String(), "", o.Width, w)}, Line{Text: row(bgWell, "  "+bottom.String(), "", o.Width, w)})
		out = append(out, Line{Text: ""})
	}
	perm := ""
	if s.Info.PermissionMode != "" {
		perm = dim(" · permissions ") + text(s.Info.PermissionMode)
	}
	add("  "+dim("now  ")+text(PrettyModel(model))+dim(" · effort ")+text(effort)+perm+
		dim("   in ")+text(tokens(t.In+t.CacheRead+t.CacheOut))+dim(" · out ")+text(tokens(t.Out))+dim(fmt.Sprintf(" · %d requests", t.Requests)), "")
	for _, st := range s.byID {
		if (st.Tool == "Task" || st.Tool == "Agent") && st.Status == Running {
			add("  "+paint(cOrange, "⇉ ")+text(agentName(st))+dim("   "+oneLine(readInput(st.Input).str("description"))),
				paint(cOrange, dur(o.Now.Sub(st.Start))))
		}
	}

	// --- per turn: cost as bars in the model's colour, then where the
	// model and effort changed, in words ---
	if len(s.Turns) > 1 {
		costs := s.turnCosts()
		// A turn that asked nothing (a stop, a slash command) has no model
		// of its own: it goes with the one before it.
		models, efforts := make([]string, len(s.Turns)), make([]string, len(s.Turns))
		for i, tn := range s.Turns {
			if tn.Model != "" && !strings.HasPrefix(tn.Model, "<") { // not <synthetic>
				models[i], efforts[i] = PrettyModel(tn.Model), firstNonEmpty(tn.Effort, "default")
			}
			if models[i] == "" && i > 0 {
				models[i], efforts[i] = models[i-1], efforts[i-1]
			}
		}
		for i := len(s.Turns) - 2; i >= 0; i-- {
			if models[i] == "" {
				models[i], efforts[i] = models[i+1], efforts[i+1]
			}
		}
		n := len(s.Turns)
		avail := w - 12
		colW := max(1, min(4, avail/n)) // a bar and its gap
		cols := min(n, avail/colW)
		from := n - cols
		most, top := 0.0, s.Turns[from]
		for _, tn := range s.Turns[from:] {
			if c := costs[tn]; c > most {
				most, top = c, tn
			}
		}
		meta := plural(n, "turn")
		if cols < n {
			meta = fmt.Sprintf("last %d of %d turns", cols, n)
		}
		if most > 0 {
			meta += fmt.Sprintf(" · most %s, on #%d", money(most), top.N)
		}
		section("Per turn", meta)
		if most > 0 {
			const height = 3 // rows, eight steps each
			levels := []rune(" ▁▂▃▄▅▆▇█")
			bw := colW
			if colW > 1 {
				bw = colW - 1
			}
			for r := height - 1; r >= 0; r-- {
				var b strings.Builder
				for i := from; i < n; i++ {
					v := int(costs[s.Turns[i]]/most*height*8 + 0.5)
					if costs[s.Turns[i]] > 0 {
						v = max(v, 1) // a cheap turn still shows
					}
					g := string(levels[max(0, min(8, v-r*8))])
					b.WriteString(paint(modelColour(models[i]), strings.Repeat(g, bw)))
					if colW > 1 {
						b.WriteByte(' ')
					}
				}
				add("    "+b.String(), "")
			}
			span := cols*colW - (colW - bw)
			first, last := fmt.Sprintf("#%d", s.Turns[from].N), fmt.Sprintf("#%d", s.Turns[n-1].N)
			axis := first
			if gap := span - len(first) - len(last); gap > 0 {
				axis += blanks(gap) + last
			}
			add("    "+dim(axis), "")
		}
		// Runs of the same value, as "medium #1–4 · default #5–13".
		runs := func(vals []string, show func(string) string) string {
			type run struct {
				v        string
				from, to int
			}
			var rs []run
			for i, v := range vals {
				if k := len(rs); k > 0 && rs[k-1].v == v {
					rs[k-1].to = s.Turns[i].N
					continue
				}
				rs = append(rs, run{v, s.Turns[i].N, s.Turns[i].N})
			}
			if len(rs) == 1 {
				return show(rs[0].v)
			}
			var parts []string
			for i, r := range rs {
				if i == 5 && len(rs) > 6 && !o.Verbose {
					parts = append(parts, dim(fmt.Sprintf("%d more changes", len(rs)-5)))
					break
				}
				at := fmt.Sprintf("#%d", r.from)
				if r.to != r.from {
					at = fmt.Sprintf("#%d–%d", r.from, r.to)
				}
				parts = append(parts, show(r.v)+" "+dim(at))
			}
			return strings.Join(parts, dim(" · "))
		}
		add(label("model")+runs(models, func(m string) string {
			if m == "" {
				return dim("unknown")
			}
			return paint(modelColour(m), "■ ") + text(m)
		}), "")
		add(label("effort")+runs(efforts, text), "")
	}

	// --- tools ---
	var ts []*ToolStat
	for _, v := range s.Tools {
		if v.Calls > 0 {
			ts = append(ts, v)
		}
	}
	sort.Slice(ts, func(i, j int) bool {
		if ts[i].Calls != ts[j].Calls {
			return ts[i].Calls > ts[j].Calls
		}
		return ts[i].Name < ts[j].Name
	})
	if len(ts) > 0 {
		section("Tools", plural(t.ToolCalls, "call"))
		nameW := 8
		for _, v := range ts {
			nameW = max(nameW, min(26, cellw.String(v.Name)))
		}
		top := math.Sqrt(float64(ts[0].Calls))
		barW := max(10, min(30, w-nameW-30))
		for i, v := range ts {
			if i >= 12 && !o.Verbose {
				add("    "+dim(fmt.Sprintf("… %d more tools · ctrl+o shows all", len(ts)-i)), "")
				break
			}
			right := ""
			if v.Time >= 100*time.Millisecond {
				right = dim(dur(v.Time))
			}
			if v.Failed > 0 {
				right = paint(cRed, fmt.Sprintf("%d failed", v.Failed)) + "   " + right
			}
			// Square-root scale, so the second tool isn't an empty bar.
			add("    "+text(fitTo(truncateCells(v.Name, nameW), nameW))+"  "+bar(math.Sqrt(float64(v.Calls))/top, barW, cSub)+" "+text(fmt.Sprint(v.Calls)), right)
		}
	}

	// --- cache ---
	if t.Requests > 0 {
		cold := s.ColdStarts()
		expected, idle, odd := 0, 0, 0
		kinds := map[string]int{}
		for _, c := range cold {
			switch {
			case !c.Expected:
				odd++
			case strings.HasPrefix(c.Reason, "idle"):
				idle++
			default:
				expected++
				kinds[strings.SplitN(c.Reason, ":", 2)[0]]++
			}
		}
		meta := fmt.Sprintf("%.0f%% of input read from cache", hit*100)
		if n := len(cold); n > 0 {
			meta += fmt.Sprintf(" · %d cold starts", n)
			if odd == 0 {
				meta += ", all expected"
			}
		}
		section("Cache", meta)
		add(label("hit rate")+bar(hit, 20, cGreen)+" "+text(fmt.Sprintf("%.0f%%", hit*100)), "")
		add(label("read")+text(tokens(t.CacheRead))+dim("   written ")+text(tokens(t.CacheOut)), "")
		if expected > 0 {
			var parts []string
			for _, k := range []string{"new subagent", "session start", "model changed, and each model has its own cache"} {
				if n := kinds[k]; n > 0 {
					name := strings.SplitN(k, ",", 2)[0]
					parts = append(parts, fmt.Sprintf("%d × %s", n, name))
				}
			}
			add(label("expected")+dim(strings.Join(parts, " · ")+" — a new cache is how these begin"), "")
		}
		shown := 0
		for _, c := range cold {
			if c.Expected && !strings.HasPrefix(c.Reason, "idle") {
				continue
			}
			if shown >= 6 && !o.Verbose {
				add("    "+dim(fmt.Sprintf("… %d more", idle+odd-shown)), "")
				break
			}
			shown++
			who := "main"
			if c.Agent != "" {
				who = c.Agent
			}
			col := cDim
			if !c.Expected {
				col = cYellow
			}
			add(label(c.At.Local().Format("15:04"))+paint(col, c.Reason)+dim(fmt.Sprintf(" · %s idle · %s · rewrote ", dur(c.Gap), who))+text(tokens(c.Written)), "")
		}
		if len(cold) == 0 {
			add(label("went cold")+dim("never"), "")
		}
	}

	// --- subagents ---
	type agg struct {
		runs, steps int
		model       string
	}
	subs := map[string]*agg{}
	var names []string
	for _, st := range s.byID {
		if st.Tool != "Task" && st.Tool != "Agent" {
			continue
		}
		kind := agentName(st)
		a := subs[kind]
		if a == nil {
			a = &agg{}
			subs[kind] = a
			names = append(names, kind)
		}
		a.runs++
		a.steps += len(st.Children)
	}
	if len(names) > 0 {
		for _, r := range s.Requests {
			if a := subs[r.Agent]; a != nil && r.Model != "" {
				a.model = r.Model
			}
		}
		sort.Strings(names)
		section("Subagents", "")
		nameW := 8
		for _, n := range names {
			nameW = max(nameW, min(30, cellw.String(n)))
		}
		for _, n := range names {
			a := subs[n]
			right := ""
			if a.steps > 0 {
				right = dim(plural(a.steps, "step"))
			}
			m := ""
			if a.model != "" {
				m = text(PrettyModel(a.model))
			}
			add("    "+text(fitTo(truncateCells(n, nameW), nameW))+dim(fmt.Sprintf("  ×%-4d ", a.runs))+m, right)
		}
	}
	return out
}

// Cost prices every model call in the session.
func (s *Session) Cost() float64 {
	var total float64
	for _, r := range s.Requests {
		u := r.Usage
		total += claude.Cost(r.Model, claude.TokenUsage{
			Input: int64(u.InputTokens), Output: int64(u.OutputTokens),
			CacheRead: int64(u.CacheReadInputTokens), CacheWrite1h: int64(u.CacheCreationInputTokens),
		}, false)
	}
	return total
}

// LastWords is the first line of the latest thing the session said.
func (s *Session) LastWords() string {
	for i := len(s.Turns) - 1; i >= 0; i-- {
		items := s.Turns[i].Items
		for j := len(items) - 1; j >= 0; j-- {
			if items[j].Kind == KText && strings.TrimSpace(items[j].Text) != "" {
				return firstPlain(items[j].Text)
			}
		}
	}
	return ""
}

// Tokens formats a token count the way the overview does.
func Tokens(n int) string { return tokens(n) }

// modelColour tells models apart in the strip.
func modelColour(m string) string {
	switch {
	case strings.Contains(strings.ToLower(m), "opus"):
		return cOrange
	case strings.Contains(strings.ToLower(m), "sonnet"):
		return cBlue
	case strings.Contains(strings.ToLower(m), "haiku"):
		return cGreen
	case strings.Contains(strings.ToLower(m), "fable"):
		return cYellow
	}
	return cSub
}

// fitTo pads or cuts styled text to exactly w cells.
func fitTo(s string, w int) string {
	n := cellw.String(stripANSI(s))
	if n > w {
		return ansi.Truncate(s, w, "…")
	}
	return s + blanks(w-n)
}

// turnCosts prices each turn: what the Result said, or else every model
// call (subagents' too) made while it ran.
func (s *Session) turnCosts() map[*Turn]float64 {
	out := make(map[*Turn]float64, len(s.Turns))
	ti := 0
	for _, r := range s.Requests {
		for ti+1 < len(s.Turns) && !r.At.Before(s.Turns[ti+1].Start) {
			ti++
		}
		if ti < len(s.Turns) {
			u := r.Usage
			out[s.Turns[ti]] += claude.Cost(r.Model, claude.TokenUsage{
				Input: int64(u.InputTokens), Output: int64(u.OutputTokens),
				CacheRead: int64(u.CacheReadInputTokens), CacheWrite1h: int64(u.CacheCreationInputTokens),
			}, false)
		}
	}
	for _, t := range s.Turns {
		if t.Cost > 0 {
			out[t] = t.Cost
		}
	}
	return out
}
