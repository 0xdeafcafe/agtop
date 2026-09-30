package codex

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/event"
	"github.com/0xdeafcafe/rush/internal/jsonx"
)

var (
	_ agent.HistoryReader = Adapter{}
	_ agent.Discoverer    = Adapter{}
	_ agent.TailReader    = Adapter{}
)

// History reads the rollout at s.Transcript, or else the one of thread
// s.ID in s.Profile, as the events a live thread sends. When before isn't zero, it stops at the first line written at or
// after it.
func (Adapter) History(s agent.Session, before time.Time) ([]event.Event, error) {
	f, err := openRollout(s)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readRollout(f, before)
}

// HistoryTail is History read from the rollout's first line, the thread's
// meta, then the first whole line of its last most bytes on.
func (Adapter) HistoryTail(s agent.Session, most int64) ([]event.Event, bool, error) { //nolint:gocritic // as History
	f, err := openRollout(s)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() <= most {
		evs, err := readRollout(f, time.Time{})
		return evs, false, err
	}
	head, _ := bufio.NewReader(f).ReadBytes('\n')
	// A byte early: the line cut short runs to the first newline, which is
	// that byte when the cut fell between two lines.
	end := bufio.NewReader(io.NewSectionReader(f, st.Size()-most-1, most+1))
	if _, err := end.ReadBytes('\n'); err != nil {
		return nil, false, err
	}
	evs, err := readRollout(io.MultiReader(bytes.NewReader(head), end), time.Time{})
	return evs, true, err
}

// openRollout opens s's rollout, found by its thread when that's all
// there is to go on.
func openRollout(s agent.Session) (*os.File, error) { //nolint:gocritic // as History
	if s.Transcript == "" && s.ID != "" {
		// Known by its thread alone: its rollout is named after it.
		m, _ := filepath.Glob(filepath.Join(s.Profile.Dir, "sessions", "*", "*", "*", "rollout-*-"+s.ID+".jsonl"))
		if len(m) > 0 {
			s.Transcript = m[len(m)-1]
		}
	}
	if s.Transcript == "" {
		return nil, fmt.Errorf("codex: session %s has no rollout", s.ID)
	}
	return os.Open(s.Transcript)
}

func readRollout(f io.Reader, before time.Time) ([]event.Event, error) {
	var r replay
	err := readLines(f, func(b []byte) bool {
		var l rolloutLine
		if jsonx.Unmarshal(b, &l) != nil {
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
