package advisor

import (
	"fmt"
	"strings"
	"time"

	"github.com/0xdeafcafe/agtop/internal/efficiency"
)

// Input is what a pass is given: the figures, worked out by agtop, so the
// model reads a few kilobytes rather than the transcripts themselves.
type Input struct {
	View     *efficiency.View
	Found    map[string]efficiency.Found
	Findings []efficiency.Finding // what the Efficiency place already says
	Top      []efficiency.SessionCost
	Briefs   []string // a brief of each of Top, by index; "" where there's none
	Known    []string // the advisor's own findings so far
	// Settled are the IDs Opus decided on or you put away: never checked
	// again.
	Settled []string
	// Reserve, when set, counts each review against the day's cap before
	// it runs; an error stops the reviews.
	Reserve func() error
	// Pending are earlier candidates Opus hasn't checked yet: checked
	// before new ones worth less.
	Pending []Finding
}

// Digest is the input in words, compact enough for a cheap model to read
// every pass.
func Digest(in Input) string {
	v := in.View
	t := &v.Total
	var b strings.Builder
	p := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	m := efficiency.Money

	p("## Figures, %s to %s", v.Q.From.Format("2 Jan"), v.Q.To.Format("2 Jan 15:04"))
	if t.Req == 0 {
		p("No requests.")
		return b.String()
	}
	cr := 0.0
	if ctx := t.Context(); ctx > 0 {
		cr = float64(t.CR) / float64(ctx) * 100
	}
	p("Spend %s over %d requests, %d sessions and %d subagent runs (list prices).", m(t.Cost()), t.Req, v.Sessions, v.Subagents)
	p("Cost by kind: input %s, output %s, cache reads %s, cache writes %s. Cache hits %.0f%% of input.", m(t.CIn), m(t.COut), m(t.CCR), m(t.CCW), cr)
	p("Per request: %s tokens sent, %s out (%s of it thinking).", efficiency.Tokens(t.Context()/t.Req), efficiency.Tokens(t.Out/t.Req), efficiency.Tokens(t.Think/t.Req))
	p("Context: sessions start at %s (median; p90 %s) and peak at %s (median). %d passed %s; carrying that cost about %s. %d compactions, %d of them automatic.",
		efficiency.Tokens(v.StartMedian), efficiency.Tokens(v.StartP90), efficiency.Tokens(v.PeakMedian), v.Fat, efficiency.Tokens(efficiency.FatContext), m(v.FatCarry), v.Compacts, v.Autos)

	all, share := v.ToolBytes()
	var tools []string
	for i, name := range efficiency.ToolNames {
		if t.Calls[i] == 0 {
			continue
		}
		tools = append(tools, fmt.Sprintf("%s %d calls, %s (%.0f%%), carried ≈%s", name, t.Calls[i], efficiency.Bytes(t.Bytes[i]), share[i]*100, m(v.Carry[i])))
	}
	p("Tool output, %s in all: %s.", efficiency.Bytes(all), strings.Join(tools, "; "))
	if t.LookCalls > 0 {
		p("Shell commands that searched or read code: %d, %s, carried ≈%s.", t.LookCalls, efficiency.Bytes(t.Look), m(v.LookCarry))
	}
	p("Tool results over %s: %d.", efficiency.Bytes(efficiency.BigResult), v.Big)
	if v.Rereads > 0 {
		p("%d sessions read one file %d+ times; worst %s ×%d.", v.Rereads, efficiency.RereadAt, v.WorstRead, v.WorstReads)
	}
	if v.SubCost > 0 {
		p("Subagents cost %s, %s of it on Opus or Fable.", m(v.SubCost), m(v.SubBigCost))
	}

	var on, off, half []string
	for i := range efficiency.Catalog {
		s := &efficiency.Catalog[i]
		switch in.Found[s.ID].Status {
		case efficiency.On:
			on = append(on, s.ID)
		case efficiency.Partial:
			half = append(half, s.ID)
		default:
			off = append(off, s.ID)
		}
	}
	p("\n## Savers (fix ids)")
	p("On: %s.", list(on))
	if len(half) > 0 {
		p("Half set up: %s.", list(half))
	}
	for i := range efficiency.Catalog {
		s := &efficiency.Catalog[i]
		if in.Found[s.ID].Status == efficiency.Off {
			p("- %s: %s", s.ID, s.About)
		}
	}

	if len(in.Findings)+len(in.Known) > 0 {
		p("\n## Already said (don't repeat these)")
		for _, f := range in.Findings {
			p("- %s", f.Title)
		}
		for _, k := range in.Known {
			p("- %s", k)
		}
	}

	p("\n## Costliest sessions")
	for i, s := range in.Top {
		kind := ""
		if s.Sub {
			kind = " subagent"
		}
		p("%d. %s, %d req, %s, %s%s in %s, start %s, peak %s, %d compactions, %d big results",
			i+1, m(s.B.Cost()), s.B.Req, span(s.Last.Sub(s.First)), s.Model, kind, s.Project,
			efficiency.Tokens(s.StartCtx), efficiency.Tokens(s.PeakCtx), s.Compacts, s.Big)
		if s.WorstReads >= efficiency.RereadAt {
			p("   read %s ×%d", s.WorstRead, s.WorstReads)
		}
		if i < len(in.Briefs) && in.Briefs[i] != "" {
			p("   brief: %s", in.Briefs[i])
		}
		p("   transcript: %s", s.Path)
	}
	return b.String()
}

func list(s []string) string {
	if len(s) == 0 {
		return "none"
	}
	return strings.Join(s, ", ")
}

// span is a session's length, short: 98h15m, 40m.
func span(d time.Duration) string {
	d = d.Round(time.Minute)
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}
