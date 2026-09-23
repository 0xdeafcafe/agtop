package fleet

import (
	"os"
	"time"

	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/state"
)

// Target is one agent's transcript as the scanner needs it.
type Target struct {
	Key  string
	Path string
}

// Scanner owns the cost cache; only its goroutine touches it.
type Scanner struct {
	cache *state.CostCache
	sizes map[string]int64
	buf   []byte
	saved time.Time
	day   string
}

func NewScanner() *Scanner {
	return &Scanner{cache: state.LoadCostCache(), sizes: map[string]int64{}, buf: make([]byte, 0, 64<<10)}
}

// Run scans every target whose files grew and returns the new totals.
func (s *Scanner) Run(targets []Target) map[string]Spend {
	out := map[string]Spend{}
	today := claude.Day(time.Now())
	if today != s.day {
		s.day, s.sizes = today, map[string]int64{}
	}
	for _, t := range targets {
		files := append([]string{t.Path}, claude.SubagentTranscripts(t.Path)...)
		changed := false
		for _, f := range files {
			st, err := os.Stat(f)
			if err != nil {
				continue
			}
			if s.sizes[f] != st.Size() {
				changed = true
			}
		}
		if !changed && len(s.sizes) > 0 {
			if _, ok := s.sizes[t.Path]; ok {
				continue
			}
		}
		var sp Spend
		for _, f := range files {
			tot := s.cache.Get(f)
			before := tot.Offset
			s.buf, _ = claude.Scan(f, tot, s.buf)
			if tot.Offset != before {
				s.cache.MarkDirty()
			}
			s.sizes[f] = tot.Size
			c, u := tot.Spend()
			sp.Cost += c
			sp.Usage.Add(u)
			sp.Today += tot.DaySpend(today)
			if f == t.Path {
				sp.Model = tot.LastModel
				sp.First, sp.Last = tot.First, tot.Last
			}
			if tot.Last.After(sp.Last) {
				sp.Last = tot.Last
			}
			for _, p := range tot.PRs {
				addUnique(&sp.PRs, p)
			}
		}
		if cap(s.buf) > 8<<20 {
			s.buf = make([]byte, 0, 64<<10)
		}
		sp.Ready = true
		out[t.Key] = sp
	}
	if time.Since(s.saved) > 30*time.Second {
		_ = s.cache.Save()
		s.saved = time.Now()
	}
	return out
}

func (s *Scanner) Flush() { _ = s.cache.Save() }

func addUnique(list *[]string, v string) {
	for _, x := range *list {
		if x == v {
			return
		}
	}
	*list = append(*list, v)
}
