package codex

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/agent/event"
)

var (
	_ agent.HistoryReader = Adapter{}
	_ agent.Discoverer    = Adapter{}
)

// History reads the rollout at s.Transcript as the events a live thread
// sends. When before isn't zero, it stops at the first line written at or
// after it.
func (Adapter) History(s agent.Session, before time.Time) ([]event.Event, error) {
	if s.Transcript == "" {
		return nil, fmt.Errorf("codex: session %s has no rollout", s.ID)
	}
	f, err := os.Open(s.Transcript)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readRollout(f, before)
}

func readRollout(f io.Reader, before time.Time) ([]event.Event, error) {
	var r replay
	err := readLines(f, func(b []byte) bool {
		var l rolloutLine
		if json.Unmarshal(b, &l) != nil {
			return true // a line cut short while Codex writes it
		}
		if !before.IsZero() {
			if t := parseTime(l.Timestamp); !t.IsZero() && !t.Before(before) {
				return false
			}
		}
		r.line(l)
		return true
	})
	r.flush()
	return r.out, err
}
