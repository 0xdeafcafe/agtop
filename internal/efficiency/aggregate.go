package efficiency

import (
	"sort"
	"strings"
	"time"

	"github.com/0xdeafcafe/agtop/internal/claude"
)

// Metric is a figure the Timeline can draw.
type Metric int

const (
	MetricCtx    Metric = iota // tokens sent per request: what each call carries
	MetricCost                 // dollars, by what they paid for
	MetricTokens               // tokens, by kind
	MetricOut                  // output per request, thinking and text
	MetricCache                // share of input read from cache
	MetricToolKB               // tool output per call, by tool
	MetricStart                // context sessions start with
	MetricReq                  // requests
	NMetrics
)

var MetricNames = [NMetrics]string{"context per request", "cost", "tokens", "output per request", "cache hits", "tool output per call", "context at start", "requests"}

// Query is what a view covers: whose transcripts, which folder, which
// time.
type Query struct {
	Accounts []string // account folders; none is every account
	Project  string   // a folder: sessions that worked in it or below
	From, To time.Time
	Daily    bool // buckets of a local day rather than an hour
}

// Range is one of the spans the place offers.
type Range struct {
	Name  string
	Span  time.Duration
	Daily bool
}

var Ranges = []Range{
	{"24 hours", 24 * time.Hour, false},
	{"7 days", 7 * 24 * time.Hour, true},
	{"30 days", 30 * 24 * time.Hour, true},
	{"90 days", 90 * 24 * time.Hour, true},
}

// Point is one bucket of the view.
type Point struct {
	At     time.Time
	B      Bucket
	Starts []int64 // contexts sessions that began here started with
}

// SaverUse is how much a saver was used in a view.
type SaverUse struct {
	N        int
	Sessions int
	First    time.Time
	Last     time.Time
}

// View is everything the place shows for one query.
type View struct {
	Q      Query
	Points []Point
	Total  Bucket

	Sessions, Subagents int
	StartMedian         int64
	StartP90            int64
	PeakMedian          int64
	Fat                 int // sessions whose context passed FatContext
	Compacts, Autos     int
	Big                 int // tool results over BigResult

	// Rereads are sessions that read one file RereadAt times or more; the
	// worst is the most reads of one file.
	Rereads    int
	WorstRead  string
	WorstReads int

	// Subagents' spend, and how much of it ran on Opus or Fable.
	SubCost, SubBigCost float64
	// Carry estimates what each tool's output cost to keep in context:
	// every token of it is read again by the requests after it.
	Carry [NTools]float64

	Uses map[string]*SaverUse // by saver ID, inside the view
	// FirstUse is when each saver was first seen at all, in any
	// transcript of these accounts: the markers on the Timeline.
	FirstUse map[string]time.Time
	// Scanned is how many transcripts the store knows.
	Scanned int
}

const (
	FatContext = 400_000
	RereadAt   = 4
)

// in reports whether a file is inside the query's accounts and folder.
func (q Query) in(f *File) bool {
	if len(q.Accounts) > 0 {
		ok := false
		for _, a := range q.Accounts {
			ok = ok || f.Account == a
		}
		if !ok {
			return false
		}
	}
	if q.Project != "" && !(f.Project == q.Project || strings.HasPrefix(f.Project, strings.TrimSuffix(q.Project, "/")+"/")) {
		return false
	}
	return true
}

// bucketStart is the start of the bucket t falls in.
func (q Query) bucketStart(t time.Time) time.Time {
	if q.Daily {
		t = t.Local()
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
	}
	return t.Truncate(time.Hour)
}

func (q Query) next(t time.Time) time.Time {
	if q.Daily {
		return t.AddDate(0, 0, 1)
	}
	return t.Add(time.Hour)
}

// NewQuery is a range ending now.
func NewQuery(r Range, now time.Time) Query {
	q := Query{To: now, Daily: r.Daily}
	q.From = q.bucketStart(now.Add(-r.Span))
	if r.Daily {
		q.From = q.next(q.From)
	}
	return q
}

// View works out a query's figures.
func (s *Store) View(q Query) *View {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v := &View{Q: q, Uses: map[string]*SaverUse{}, FirstUse: map[string]time.Time{}, Scanned: len(s.files)}

	index := map[int64]int{}
	for t := q.bucketStart(q.From); !t.After(q.To); t = q.next(t) {
		index[t.Unix()] = len(v.Points)
		v.Points = append(v.Points, Point{At: t})
	}
	slot := func(t time.Time) *Point {
		if i, ok := index[q.bucketStart(t).Unix()]; ok {
			return &v.Points[i]
		}
		return nil
	}
	fromH, toH := hourOf(q.From), hourOf(q.To)
	var starts, peaks []int64

	firstUse := func(key string, u *Use) {
		for i := range Catalog {
			sv := &Catalog[i]
			if !sv.Matches(key) {
				continue
			}
			if t, ok := v.FirstUse[sv.ID]; !ok || u.First.Before(t) {
				v.FirstUse[sv.ID] = u.First
			}
		}
	}
	for _, f := range s.files {
		if !q.in(f) {
			continue
		}
		for k, u := range f.Uses {
			firstUse(k, u)
		}
		if f.Last.Before(q.From) || f.First.After(q.To) {
			continue
		}
		var fileB Bucket
		f.EachHour(func(h int64, b *Bucket) {
			if h < fromH || h > toH {
				return
			}
			if p := slot(time.Unix(h*3600, 0)); p != nil {
				p.B.Add(b)
				fileB.Add(b)
			}
		})
		v.Total.Add(&fileB)
		if f.Sub {
			v.Subagents++
			v.SubCost += fileB.Cost()
			if m := f.Model; strings.Contains(m, "opus") || strings.Contains(m, "fable") || strings.Contains(m, "mythos") {
				v.SubBigCost += fileB.Cost()
			}
		} else if !f.First.Before(q.From) && fileB.Req > 0 {
			v.Sessions++
			starts = append(starts, f.Start())
			peaks = append(peaks, f.Peak())
			if p := slot(f.First); p != nil && f.Start() > 0 {
				p.Starts = append(p.Starts, f.Start())
			}
			if f.Peak() > FatContext {
				v.Fat++
			}
			worst, worstN := "", 0
			for path, n := range f.Reads {
				if n > worstN {
					worst, worstN = path, n
				}
			}
			if worstN >= RereadAt {
				v.Rereads++
				if worstN > v.WorstReads {
					v.WorstRead, v.WorstReads = worst, worstN
				}
			}
		}
		for _, c := range f.Compacts {
			if !c.At.Before(q.From) && !c.At.After(q.To) {
				v.Compacts++
				if c.Auto {
					v.Autos++
				}
			}
		}
		v.Big += f.Big
		// A token of tool output is read again by roughly half the
		// session's later requests, on average, at the cache-read price.
		if fileB.Req > 0 {
			read := cacheReadPrice(f.Model)
			for c := range NTools {
				v.Carry[c] += float64(fileB.Bytes[c]) / 4 * float64(fileB.Req) / 2 * read / 1e6
			}
		}
		for k, u := range f.Uses {
			if u.Last.Before(q.From) || u.First.After(q.To) {
				continue
			}
			for i := range Catalog {
				sv := &Catalog[i]
				if !sv.Matches(k) {
					continue
				}
				su := v.Uses[sv.ID]
				if su == nil {
					su = &SaverUse{}
					v.Uses[sv.ID] = su
				}
				su.N += u.N
				su.Sessions++
				if su.First.IsZero() || u.First.Before(su.First) {
					su.First = u.First
				}
				if u.Last.After(su.Last) {
					su.Last = u.Last
				}
			}
		}
	}
	for acct, r := range s.retired {
		if !q.in(&File{Account: acct}) || q.Project != "" {
			continue
		}
		for k, u := range r.Uses {
			firstUse(k, u)
		}
		for h, b := range r.Hours {
			if h < fromH || h > toH {
				continue
			}
			if p := slot(time.Unix(h*3600, 0)); p != nil {
				p.B.Add(b)
				v.Total.Add(b)
			}
		}
	}
	v.StartMedian, v.StartP90 = quantile(starts, 0.5), quantile(starts, 0.9)
	v.PeakMedian = quantile(peaks, 0.5)
	return v
}

func cacheReadPrice(model string) float64 {
	p, ok := claude.PriceFor(model)
	if !ok {
		p, _ = claude.PriceFor("claude-opus-5-5")
	}
	if p.CacheRead > 0 {
		return p.CacheRead
	}
	return p.Input * 0.1
}

func quantile(xs []int64, q float64) int64 {
	var nz []int64
	for _, x := range xs {
		if x > 0 {
			nz = append(nz, x)
		}
	}
	if len(nz) == 0 {
		return 0
	}
	sort.Slice(nz, func(i, j int) bool { return nz[i] < nz[j] })
	return nz[min(len(nz)-1, int(float64(len(nz))*q))]
}

// Series is one metric's values for each point, split into the parts the
// Timeline stacks, bottom first.
type Series struct {
	Parts  []string
	Values [][]float64 // [point][part]
	Unit   string      // "tok", "$", "%", "B"
}

// Series works out a metric over the view's points.
func (v *View) Series(m Metric) Series {
	s := Series{Values: make([][]float64, len(v.Points))}
	per := func(n, d int64) float64 {
		if d == 0 {
			return 0
		}
		return float64(n) / float64(d)
	}
	switch m {
	case MetricCtx:
		s.Parts, s.Unit = []string{"cache read", "cache write", "input"}, "tok"
	case MetricCost:
		s.Parts, s.Unit = []string{"cache read", "cache write", "input", "output"}, "$"
	case MetricTokens:
		s.Parts, s.Unit = []string{"cache read", "cache write", "input", "output"}, "tok"
	case MetricOut:
		s.Parts, s.Unit = []string{"thinking", "text & tools"}, "tok"
	case MetricCache:
		s.Parts, s.Unit = []string{"from cache"}, "%"
	case MetricToolKB:
		s.Parts, s.Unit = ToolNames[:], "B"
	case MetricStart:
		s.Parts, s.Unit = []string{"median start"}, "tok"
	case MetricReq:
		s.Parts, s.Unit = []string{"requests"}, ""
	}
	for i, p := range v.Points {
		b := &p.B
		switch m {
		case MetricCtx:
			s.Values[i] = []float64{per(b.CR, b.Req), per(b.CW, b.Req), per(b.In, b.Req)}
		case MetricCost:
			s.Values[i] = []float64{b.CCR, b.CCW, b.CIn, b.COut}
		case MetricTokens:
			s.Values[i] = []float64{float64(b.CR), float64(b.CW), float64(b.In), float64(b.Out)}
		case MetricOut:
			think := min(b.Think, b.Out)
			s.Values[i] = []float64{per(think, b.Req), per(b.Out-think, b.Req)}
		case MetricCache:
			s.Values[i] = []float64{100 * per(b.CR, b.Context())}
		case MetricToolKB:
			var calls int64
			for _, c := range b.Calls {
				calls += int64(c)
			}
			vals := make([]float64, NTools)
			for c := range NTools {
				vals[c] = per(b.Bytes[c], calls)
			}
			s.Values[i] = vals
		case MetricStart:
			s.Values[i] = []float64{float64(quantile(p.Starts, 0.5))}
		case MetricReq:
			s.Values[i] = []float64{float64(b.Req)}
		}
	}
	return s
}

// Headline figures for the whole view.
func (v *View) CacheHit() float64 {
	if c := v.Total.Context(); c > 0 {
		return 100 * float64(v.Total.CR) / float64(c)
	}
	return 0
}

func (v *View) CtxPerReq() int64 {
	if v.Total.Req == 0 {
		return 0
	}
	return v.Total.Context() / v.Total.Req
}

func (v *View) OutPerReq() int64 {
	if v.Total.Req == 0 {
		return 0
	}
	return v.Total.Out / v.Total.Req
}

// ToolBytes is all tool output in the view, and Bash's share of it.
func (v *View) ToolBytes() (all int64, share [NTools]float64) {
	for _, n := range v.Total.Bytes {
		all += n
	}
	if all > 0 {
		for c := range NTools {
			share[c] = float64(v.Total.Bytes[c]) / float64(all)
		}
	}
	return all, share
}
