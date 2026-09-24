package efficiency

import (
	"sort"
	"time"
)

// Side is one side of a comparison, in figures that don't grow with how
// much you worked: per request, per call and per session.
type Side struct {
	Sessions    int
	Req         int64
	CtxPerReq   float64
	OutPerReq   float64
	ToolPerCall float64 // bytes of tool output per call
	CacheHit    float64 // percent
	CostPerReq  float64
	CostSession float64 // median session
	StartMedian int64
	Cost        float64
}

// Compare is the figures before and after a moment, and, for a saver,
// sessions that used it against those that didn't in the same days: the
// fairer test, since the work is the same kind.
type Compare struct {
	At      time.Time
	Days    int
	Before  Side
	After   Side
	Saver   *Saver
	With    Side
	Without Side
}

// FewSessions is under how many sessions a side is too small to say much.
const FewSessions = 15

type sideAcc struct {
	b     Bucket
	costs []float64
	start []int64
	n     int
}

func (a *sideAcc) side() Side {
	s := Side{Sessions: a.n, Req: a.b.Req, Cost: a.b.Cost(), StartMedian: quantile(a.start, 0.5)}
	if a.b.Req > 0 {
		s.CtxPerReq = float64(a.b.Context()) / float64(a.b.Req)
		s.OutPerReq = float64(a.b.Out) / float64(a.b.Req)
		s.CostPerReq = a.b.Cost() / float64(a.b.Req)
	}
	if c := a.b.Context(); c > 0 {
		s.CacheHit = 100 * float64(a.b.CR) / float64(c)
	}
	var calls, bytes int64
	for i := range NTools {
		calls += int64(a.b.Calls[i])
		bytes += a.b.Bytes[i]
	}
	if calls > 0 {
		s.ToolPerCall = float64(bytes) / float64(calls)
	}
	if len(a.costs) > 0 {
		sort.Float64s(a.costs)
		s.CostSession = a.costs[len(a.costs)/2]
	}
	return s
}

// Compare works out the figures days either side of at, for q's accounts
// and folder.
func (s *Store) Compare(q Query, at time.Time, days int, saver *Saver) Compare {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := Compare{At: at, Days: days, Saver: saver}
	span := time.Duration(days) * 24 * time.Hour
	from, to := at.Add(-span), at.Add(span)
	var before, after, with, without sideAcc
	for _, f := range s.files {
		if !q.in(f) || f.First.After(to) || f.Last.Before(from) {
			continue
		}
		var side *sideAcc
		switch {
		case f.First.Before(at) && !f.First.Before(from):
			side = &before
		case !f.First.Before(at) && f.First.Before(to):
			side = &after
		default:
			continue
		}
		t := f.Totals()
		side.b.Add(&t)
		if f.Sub {
			continue
		}
		side.n++
		side.costs = append(side.costs, t.Cost())
		side.start = append(side.start, f.Start())
		if saver == nil || side != &after {
			continue
		}
		used := false
		for k := range f.Uses {
			used = used || saver.Matches(k)
		}
		split := &without
		if used {
			split = &with
		}
		split.b.Add(&t)
		split.n++
		split.costs = append(split.costs, t.Cost())
		split.start = append(split.start, f.Start())
	}
	c.Before, c.After = before.side(), after.side()
	c.With, c.Without = with.side(), without.side()
	return c
}
