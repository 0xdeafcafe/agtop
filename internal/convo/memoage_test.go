package convo

import (
	"fmt"
	"testing"
	"time"

	"github.com/0xdeafcafe/agtop/internal/host"
)

// A running turn's finished steps come from the unit memo frame after
// frame, and their rows stay remembered meanwhile: a folded run that gains
// a step draws again, and mustn't work out every step in it anew.
func TestMemoKeepsWhatUnitsDrew(t *testing.T) {
	s := New()
	s.Info.Cwd = "/work/agtop"
	sec := 0
	s.Apply(host.Sent{Text: "go"}, at(sec))
	for i := range 12 {
		sec++
		id := fmt.Sprint("b", i)
		s.Apply(toolUse(id, "Bash", map[string]any{"command": fmt.Sprintf("ls /work/agtop/dir%d", i)}), at(sec))
		s.Apply(toolResult(id, "a\nb", false, map[string]any{"stdout": "a\nb", "stderr": ""}), at(sec))
	}
	s.Turns[len(s.Turns)-1].Live = true
	o := Options{Width: 120, Open: map[string]bool{}}
	for f := range 5 {
		o.Tick, o.Now = f, time.Unix(1e9+int64(f), 0)
		s.Render(o)
	}
	for _, it := range s.Turns[len(s.Turns)-1].Items[:9] {
		if st := it.Step; st != nil && !remembered(s, st) {
			t.Fatalf("step %s's rows were forgotten while the unit memo drew it", st.ID)
		}
	}
}

func remembered(s *Session, st *Step) bool {
	for _, m := range []map[stepKey]string{s.rows, s.rowsOld} {
		for k := range m {
			if k.st == st {
				return true
			}
		}
	}
	return false
}
