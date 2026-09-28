package efficiency

import (
	"sort"
	"time"
)

// SessionCost is one transcript's figures inside a query: what the advisor
// is given to decide which conversations are worth reading.
type SessionCost struct {
	Path     string // the transcript
	Project  string
	Model    string
	Sub      bool
	First    time.Time
	Last     time.Time
	B        Bucket
	StartCtx int64
	PeakCtx  int64
	Compacts int
	Big      int
	// WorstRead is the file read most often, and how many times.
	WorstRead  string
	WorstReads int
}

// TopSessions are the n transcripts that cost most inside q, costliest
// first.
func (s *Store) TopSessions(q Query, n int) []SessionCost {
	s.mu.RLock()
	defer s.mu.RUnlock()
	fromH, toH := hourOf(q.From), hourOf(q.To)
	var out []SessionCost
	for p, f := range s.files {
		if !q.in(f) || f.Last.Before(q.From) || f.First.After(q.To) {
			continue
		}
		c := SessionCost{Path: p, Project: f.Project, Model: f.Model, Sub: f.Sub, First: f.First, Last: f.Last,
			StartCtx: f.StartCtx, PeakCtx: f.PeakCtx, Compacts: len(f.Compacts), Big: f.Big}
		f.EachHour(func(h int64, b *Bucket) {
			if h >= fromH && h <= toH {
				c.B.Add(b)
			}
		})
		if c.B.Req == 0 {
			continue
		}
		for file, k := range f.Reads {
			if k > c.WorstReads || k == c.WorstReads && file < c.WorstRead {
				c.WorstRead, c.WorstReads = file, k
			}
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if ci, cj := out[i].B.Cost(), out[j].B.Cost(); ci != cj {
			return ci > cj
		}
		return out[i].Path < out[j].Path
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}
