package convo

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/agtop/internal/headless"
)

// TestDump writes everything the renderer draws, escapes and all, for the
// bench session and a few real transcripts, to $AGTOP_DUMP: diff two dumps
// to check a change to the renderer drew nothing differently.
func TestDump(t *testing.T) {
	out := os.Getenv("AGTOP_DUMP")
	if out == "" {
		t.Skip("set AGTOP_DUMP=<file> to dump the renderer's output")
	}
	var b strings.Builder
	dump := func(name string, lines []Line) {
		fmt.Fprintf(&b, "=== %s (%d)\n", name, len(lines))
		for _, l := range lines {
			fmt.Fprintf(&b, "%q %q\n", l.Ref, l.Text)
		}
	}
	views := func(name string, s *Session, now time.Time) {
		for _, w := range []int{20, 60, 120, 250} {
			for tick := 0; tick < 2; tick++ {
				o := Options{Width: w, Now: now, Tick: tick, Open: map[string]bool{}}
				dump(fmt.Sprintf("%s w%d tick%d", name, w, tick), s.Render(o))
			}
			o := Options{Width: w, Now: now, Open: map[string]bool{"t1": true, "t2": false}, Verbose: true}
			dump(fmt.Sprintf("%s w%d verbose", name, w), s.Render(o))
			if len(s.Turns) > 0 {
				o = Options{Width: w, Now: now, Open: map[string]bool{}, Selected: fmt.Sprintf("t%d", len(s.Turns)), Focused: true}
				dump(fmt.Sprintf("%s w%d selected", name, w), s.Render(o))
			}
			o = Options{Width: w, Now: now, Open: map[string]bool{}}
			dump(fmt.Sprintf("%s w%d overview", name, w), s.Overview(o))
			dump(fmt.Sprintf("%s w%d rail", name, w), s.RecentEdits(o, 80))
			dump(fmt.Sprintf("%s w%d search", name, w), s.SearchView("the", o))
		}
	}
	s := benchSession(40)
	views("bench", s, at(100000))
	s.Apply(headless.Delta{Text: "and then some more words"}, at(100001))
	views("bench+delta", s, at(100002))
	views("fixture", session(), at(40))

	home, _ := os.UserHomeDir()
	paths, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", "*.jsonl"))
	sort.Strings(paths)
	n := 0
	for _, p := range paths {
		if st, err := os.Stat(p); err != nil || st.Size() > 3<<20 || st.Size() < 200<<10 || st.ModTime().After(time.Now().Add(-time.Hour)) {
			continue
		}
		tl := NewTail(p)
		if _, err := tl.Read(); err != nil {
			continue
		}
		views(filepath.Base(p), tl.Sess, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
		if n++; n == 6 {
			break
		}
	}
	if err := os.WriteFile(out, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}
