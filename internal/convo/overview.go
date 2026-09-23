package convo

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/0xdeafcafe/agtop/internal/claude"
)

// ColdStart is a request that had to write the prompt cache again instead
// of reading it, usually because the session sat idle past the cache's life.
type ColdStart struct {
	At      time.Time
	Agent   string
	Gap     time.Duration // since that agent's previous request
	Written int           // tokens written to the cache
}

// ColdStarts finds them. The first request of each agent is expected to be
// cold and doesn't count.
func (s *Session) ColdStarts() []ColdStart {
	prev := map[string]time.Time{}
	var out []ColdStart
	for _, r := range s.Requests {
		u := r.Usage
		total := u.CacheReadInputTokens + u.CacheCreationInputTokens
		if last, ok := prev[r.Agent]; ok && u.CacheCreationInputTokens > 4096 && u.CacheReadInputTokens*5 < total {
			out = append(out, ColdStart{At: r.At, Agent: r.Agent, Gap: r.At.Sub(last), Written: u.CacheCreationInputTokens})
		}
		prev[r.Agent] = r.At
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

	// --- now ---
	section("Now", "")
	model := s.Info.Model
	if n := len(s.Requests); n > 0 {
		for i := n - 1; i >= 0; i-- {
			if s.Requests[i].Agent == "" && s.Requests[i].Model != "" {
				model = s.Requests[i].Model
				break
			}
		}
	}
	if model == "" {
		model = s.Model
	}
	effort := s.Info.Effort
	if effort == "" {
		effort = "default"
	}
	main := text(PrettyModel(model)) + dim(" · effort ") + text(effort)
	if s.Context > 0 {
		win := int(claude.ContextWindow(model))
		pct := float64(s.Context) / float64(win)
		main += dim("   context ") + bar(pct, 10, cGreen) + " " + sub(fmt.Sprintf("%.0f%% of %s", pct*100, tokens(win)))
	}
	add(label("main agent")+main, "")
	for _, st := range s.byID {
		if (st.Tool == "Task" || st.Tool == "Agent") && st.Status == Running {
			kind := readInput(st.Input).str("subagent_type")
			m := ""
			for i := len(s.Requests) - 1; i >= 0; i-- {
				if s.Requests[i].Agent == kind {
					m = s.Requests[i].Model
					break
				}
			}
			add(label("⇉ "+kind)+text(PrettyModel(m))+dim("   "+oneLine(readInput(st.Input).str("description"))),
				paint(cOrange, dur(o.Now.Sub(st.Start))))
		}
	}
	if s.Info.PermissionMode != "" {
		add(label("permissions")+text(s.Info.PermissionMode), "")
	}

	// --- totals ---
	t := s.Totals(o.Now)
	section("Totals", "")
	add(label("spent")+paint(cText+bold, money(t.Cost))+dim("   "+plural(t.Turns, "turn")+" · "+dur(t.Working)+" working"), "")
	add(label("tool calls")+text(fmt.Sprint(t.ToolCalls))+dim(fmt.Sprintf("   %d failed", t.Failed)), "")
	add(label("tokens")+dim("in ")+text(tokens(t.In+t.CacheRead+t.CacheOut))+dim("   out ")+text(tokens(t.Out))+dim(fmt.Sprintf("   %d requests", t.Requests)), "")

	// --- model and effort by turn ---
	if len(s.Turns) > 0 {
		section("Model and effort", "by turn")
		type span struct {
			from, to      int
			model, effort string
		}
		var spans []span
		for _, tn := range s.Turns {
			m, e := PrettyModel(tn.Model), tn.Effort
			if e == "" {
				e = "default"
			}
			if n := len(spans); n > 0 && spans[n-1].model == m && spans[n-1].effort == e {
				spans[n-1].to = tn.N
				continue
			}
			spans = append(spans, span{tn.N, tn.N, m, e})
		}
		for _, sp := range spans {
			r := fmt.Sprintf("#%d", sp.from)
			if sp.to != sp.from {
				r = fmt.Sprintf("#%d–#%d", sp.from, sp.to)
			}
			add("    "+dim(fmt.Sprintf("%-13s", r))+text(sp.model)+dim("   effort ")+text(sp.effort), "")
		}
	}

	// --- tools ---
	var ts []*ToolStat
	if len(s.Tools) > 0 {
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
	}
	if len(ts) > 0 {
		section("Tools", "most used: "+ts[0].Name)
		top := ts[0].Calls
		for i, v := range ts {
			if i >= 10 && !o.Verbose {
				add("    "+dim(fmt.Sprintf("… %d more tools", len(ts)-i)), "")
				break
			}
			right := ""
			if v.Time >= 100*time.Millisecond {
				right = dim(dur(v.Time))
			}
			if v.Failed > 0 {
				right = paint(cRed, fmt.Sprintf("%d failed", v.Failed)) + "   " + right
			}
			add("    "+text(fmt.Sprintf("%-13s", truncateCells(v.Name, 13)))+bar(float64(v.Calls)/float64(top), 24, cSub)+" "+text(fmt.Sprint(v.Calls)), right)
		}
	}

	// --- cache ---
	if t.Requests > 0 {
		cold := s.ColdStarts()
		hit := 0.0
		if all := t.CacheRead + t.CacheOut + t.In; all > 0 {
			hit = float64(t.CacheRead) / float64(all)
		}
		section("Cache", fmt.Sprintf("%.0f%% of input read from cache", hit*100))
		add(label("read")+text(tokens(t.CacheRead))+dim("   written ")+text(tokens(t.CacheOut)), "")
		if len(cold) == 0 {
			add(label("went cold")+dim("never"), "")
		}
		for i, c := range cold {
			if i >= 8 && !o.Verbose {
				add("    "+dim(fmt.Sprintf("… %d more", len(cold)-i)), "")
				break
			}
			who := "main"
			if c.Agent != "" {
				who = c.Agent
			}
			add(label(c.At.Local().Format("15:04"))+paint(cYellow, "cold")+dim(fmt.Sprintf(" after %s idle · %s · rewrote ", dur(c.Gap), who))+text(tokens(c.Written)), "")
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
		kind := readInput(st.Input).str("subagent_type")
		if kind == "" {
			kind = "subagent"
		}
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
		for _, n := range names {
			a := subs[n]
			add("    "+text(fmt.Sprintf("%-13s", truncateCells(n, 13)))+dim(fmt.Sprintf("×%d   ", a.runs))+text(PrettyModel(a.model)), dim(plural(a.steps, "step")))
		}
	}
	return out
}
