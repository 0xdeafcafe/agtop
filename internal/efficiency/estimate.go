package efficiency

import (
	"sort"
	"time"
)

// Base is the part of the spend a saver acts on, worked out from the
// view's own figures.
type Base int

const (
	BaseShell   Base = iota + 1 // shell output, carried in context
	BaseLook                    // searching and reading code: Read, Grep/Glob and shell searches, carried
	BaseToolOut                 // every tool's output, carried
	BaseMCP                     // MCP tools' output, carried
	BaseText                    // what Claude writes, thinking aside
	BaseThink                   // thinking
	BaseSubBig                  // subagents run on Opus or Fable
	BaseStart                   // what sessions start with, read again by every request
	BaseFat                     // context past FatContext, read again by every request
	BaseAll                     // all of it: savers only measured on the whole bill
)

var baseNames = map[Base]string{
	BaseShell:   "shell output kept in context",
	BaseLook:    "searching and reading code (Read, Grep, and grep/find/cat in the shell)",
	BaseToolOut: "tool output kept in context",
	BaseMCP:     "MCP output kept in context",
	BaseText:    "what Claude writes, thinking aside",
	BaseThink:   "thinking",
	BaseSubBig:  "subagents on Opus or Fable",
	BaseStart:   "the context sessions start with",
	BaseFat:     "context past 400k tokens",
	BaseAll:     "everything",
}

func (b Base) String() string { return baseNames[b] }

// Cut is what a saver can be expected to take off its base: a share, low
// to high, from what's been published about it (Basis says what). A low
// below zero is a saver that has been measured costing more.
type Cut struct {
	Of        Base
	Low, High float64
	Basis     string
}

// Estimate is what a saver might have saved over a view had it been on
// throughout: its cut of what the view spent on its base.
type Estimate struct {
	Base      float64 // dollars the view spent on the base
	Low, High float64 // dollars it might have saved
}

// Mid is the middle of the range, for ordering.
func (e Estimate) Mid() float64 { return (e.Low + e.High) / 2 }

// Spent is what the view spent on a base.
func (v *View) Spent(b Base) float64 {
	t := &v.Total
	switch b {
	case BaseShell:
		return v.Carry[ToolBash]
	case BaseLook:
		return v.Carry[ToolRead] + v.Carry[ToolSearch] + v.LookCarry
	case BaseToolOut:
		var c float64
		for _, x := range v.Carry {
			c += x
		}
		return c
	case BaseMCP:
		return v.Carry[ToolMCP]
	case BaseText, BaseThink:
		if t.Out == 0 {
			return 0
		}
		think := float64(min(t.Think, t.Out)) / float64(t.Out)
		if b == BaseThink {
			return t.COut * think
		}
		return t.COut * (1 - think)
	case BaseSubBig:
		return v.SubBigCost
	case BaseStart:
		return float64(v.StartMedian) * float64(t.Req) * cacheReadPrice("") / 1e6
	case BaseFat:
		return v.FatCarry
	case BaseAll:
		return t.Cost()
	}
	return 0
}

// Estimate is the saver's estimate over the view, or false when it has no
// cut to go by or the view spent nothing on its base.
func (v *View) Estimate(s *Saver) (Estimate, bool) {
	if s.Cut == nil {
		return Estimate{}, false
	}
	base := v.Spent(s.Cut.Of)
	if base <= 0 {
		return Estimate{}, false
	}
	return Estimate{Base: base, Low: base * s.Cut.Low, High: base * s.Cut.High}, true
}

// ByEstimate orders savers by what they might save over the view, most
// first; those without an estimate, or that skip says to leave out (being
// on already, their base has them in), keep their order after them.
func (v *View) ByEstimate(list []*Saver, skip func(*Saver) bool) {
	mid := func(s *Saver) float64 {
		if e, ok := v.Estimate(s); ok && (skip == nil || !skip(s)) {
			return e.Mid()
		}
		return -1e18
	}
	sort.SliceStable(list, func(i, j int) bool { return mid(list[i]) > mid(list[j]) })
}

// Measured is sessions in the view that used a saver against those that
// didn't since it was first seen: what it did here, as far as figures
// that aren't a controlled test can say.
type Measured struct {
	With, Without Side
	Since         time.Time
}

// Measured is the saver's split, or false when it was never seen.
func (v *View) Measured(id string) (Measured, bool) {
	sp := v.split[id]
	if sp == nil {
		return Measured{}, false
	}
	return Measured{With: sp[0].side(), Without: sp[1].side(), Since: v.FirstUse[id]}, true
}

// Verdict is whether a saver that's set up is doing anything here.
type Verdict int

const (
	Unseen Verdict = iota // a setting: acts on every session, nothing in a transcript shows it
	Half                  // found, but not finished: it does nothing
	Silent                // set up, but never seen in these sessions
	Firing                // seen at work
)

// Working is a saver's verdict over the view and, when Compared, how the
// figure it should move differed in sessions with it against those
// without, in percent: negative is better, but for cache hits.
type Working struct {
	Verdict  Verdict
	Metric   Metric
	Change   float64
	Compared bool
	Few      bool // under FewSessions on a side
}

// Working is whether a saver found set up is doing anything in the view.
func (v *View) Working(s *Saver, f Found) Working {
	switch {
	case f.Status == Partial:
		return Working{Verdict: Half}
	case len(s.Uses) == 0:
		return Working{Verdict: Unseen}
	case v.Uses[s.ID] == nil:
		return Working{Verdict: Silent}
	}
	w := Working{Verdict: Firing, Metric: MetricCost}
	if len(s.Moves) > 0 {
		w.Metric = s.Moves[0]
	}
	mz, ok := v.Measured(s.ID)
	if !ok || mz.With.Sessions == 0 || mz.Without.Sessions == 0 {
		return w
	}
	a, b := sideFigure(mz.With, w.Metric), sideFigure(mz.Without, w.Metric)
	if b <= 0 {
		return w
	}
	w.Change, w.Compared = (a-b)/b*100, true
	w.Few = mz.With.Sessions < FewSessions || mz.Without.Sessions < FewSessions
	return w
}

// sideFigure is a side's figure for a metric, per request or per call.
func sideFigure(s Side, m Metric) float64 {
	switch m {
	case MetricToolKB:
		return s.ToolPerCall
	case MetricCtx:
		return s.CtxPerReq
	case MetricOut:
		return s.OutPerReq
	case MetricCache:
		return s.CacheHit
	case MetricStart:
		return float64(s.StartMedian)
	}
	return s.CostPerReq
}
