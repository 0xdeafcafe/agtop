package convo

import "testing"

// A lookup landing (lookupsGen moving) redraws only the turns that read
// one still out; the rest of an all-open history is kept as drawn.
func TestLookupLandedRedrawsOnlyWaitingTurns(t *testing.T) {
	s := benchSession(3)
	dir := t.TempDir()
	s.Info.Cwd = dir
	realDirs.Store(dir, "") // looked up, not in yet: realDir answers "not yet"
	o := Options{Width: 120, Now: at(100000), Open: map[string]bool{}}
	first := s.Turns[0]
	s.Render(o)
	if !s.cache[first].waits {
		t.Fatal("drawn before its folder resolved, the turn should wait on it")
	}
	realDirs.Store(dir, dir)
	lookupsGen.Add(1)
	s.Render(o)
	c := s.cache[first]
	if c.waits {
		t.Fatal("drawn once its folder resolved, the turn waits on nothing")
	}
	lookupsGen.Add(1)
	s.Render(o)
	if again := s.cache[first]; &again.lines[0] != &c.lines[0] {
		t.Fatal("a lookup landing redrew a turn that read none")
	}
}
