package fleet

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/state"
)

// Target is one agent's transcript as the scanner needs it.
type Target struct {
	Key  string
	Path string
	// Live is an agent that may be writing: its subagents' transcripts are
	// checked every run. Others are only looked at when their own
	// transcript or subagents folder changes.
	Live bool
}

// Scanner owns the cost cache; only its goroutine touches it.
type Scanner struct {
	mu    sync.Mutex // held for a whole scan; the cache is only touched under it
	cache *state.CostCache
	sizes map[string]int64
	seen  map[string]seenTarget
	buf   []byte
	saved time.Time
	day   string
}

// seenTarget is how a target's files stood at its last scan.
type seenTarget struct {
	main   int64     // its transcript's size
	subDir time.Time // its subagents folder's time, which changes as runs start
	subs   []string  // the subagents' transcripts
}

func NewScanner() *Scanner {
	return &Scanner{cache: state.LoadCostCache(), sizes: map[string]int64{}, seen: map[string]seenTarget{}, buf: make([]byte, 0, 64<<10)}
}

// files lists a target's transcripts, and reports false when nothing about
// a target that isn't live has changed since the last scan.
func (s *Scanner) files(t Target) ([]string, bool) {
	var main int64
	if st, err := os.Stat(t.Path); err == nil {
		main = st.Size()
	}
	var subDir time.Time
	if st, err := os.Stat(filepath.Join(strings.TrimSuffix(t.Path, ".jsonl"), "subagents")); err == nil {
		subDir = st.ModTime()
	}
	prev, ok := s.seen[t.Path]
	if ok && !t.Live && prev.main == main && prev.subDir.Equal(subDir) {
		return nil, false
	}
	subs := prev.subs
	if !ok || !prev.subDir.Equal(subDir) {
		subs = claude.SubagentTranscripts(t.Path)
	}
	s.seen[t.Path] = seenTarget{main: main, subDir: subDir, subs: subs}
	return append([]string{t.Path}, subs...), true
}

// Run scans every target whose files grew and returns the new totals.
func (s *Scanner) Run(targets []Target) map[string]Spend {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]Spend{}
	today := claude.Day(time.Now())
	if today != s.day {
		// A new day: today's spend starts again, so everything is summed afresh.
		s.day, s.sizes, s.seen = today, map[string]int64{}, map[string]seenTarget{}
	}
	for _, t := range targets {
		files, maybe := s.files(t)
		if !maybe {
			continue
		}
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
			for _, d := range tot.Dirs {
				addUnique(&sp.Dirs, d)
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

// Flush saves the cost cache unless a scan is mid-way; the cache also saves
// itself every 30s, so skipping one save loses nothing.
func (s *Scanner) Flush() {
	if s.mu.TryLock() {
		_ = s.cache.Save()
		s.mu.Unlock()
	}
}

func addUnique(list *[]string, v string) {
	for _, x := range *list {
		if x == v {
			return
		}
	}
	*list = append(*list, v)
}
