package codex

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/event"
	"github.com/0xdeafcafe/rush/internal/jsonx"
)

var (
	_ agent.HistoryReader   = Adapter{}
	_ agent.Discoverer      = Adapter{}
	_ agent.TailReader      = Adapter{}
	_ agent.HistoryFollower = Adapter{}
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
	// Its meta, and the first turn_context a few lines on, which says the
	// model the thread's init has, as a whole read finds it.
	var head []byte
	n := 0
	_ = readHeadLines(io.NewSectionReader(f, 0, st.Size()-most), func(b []byte) bool {
		n++
		context := n > 1 && bytes.Contains(b[:min(len(b), 160)], []byte(`"type":"turn_context"`))
		if n == 1 || context {
			head = append(append(head, b...), '\n')
		}
		return !context && n < 64
	})
	// A byte early: the line cut short runs to the first newline, which is
	// that byte when the cut fell between two lines.
	end := bufio.NewReader(io.NewSectionReader(f, st.Size()-most-1, most+1))
	if _, err := end.ReadBytes('\n'); err != nil {
		return nil, false, err
	}
	evs, err := readRollout(io.MultiReader(bytes.NewReader(head), end), time.Time{})
	return evs, true, err
}

// FollowHistory reads s's rollout a line at a time, as it grows: each
// read goes on from the last whole line the one before took.
func (Adapter) FollowHistory(s agent.Session, stop *atomic.Bool) func() ([]event.Event, error) { //nolint:gocritic // as History
	var r replay
	var off int64
	return func() ([]event.Event, error) {
		f, err := openRollout(s)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			return nil, err
		}
		if st.Size() < off {
			r, off = replay{}, 0 // rewritten
		}
		end := wholeLines(f, off, st.Size())
		err = readLines(io.NewSectionReader(f, off, end-off), func(b []byte) bool {
			var l rolloutLine
			if jsonx.Unmarshal(b, &l) == nil {
				r.line(l)
			}
			return !stop.Load()
		})
		if err == nil && stop.Load() {
			err = errStopped
		}
		if err != nil {
			r, off = replay{}, 0 // part read: all of it again next time
			return nil, err
		}
		off = end
		// The turn still going, as a whole read ends it, on a copy: the
		// rest of it is read next time.
		live := r
		live.out, live.turn = slices.Clip(r.out), slices.Clone(r.turn)
		live.flush()
		return live.out, nil
	}
}

var errStopped = errors.New("codex: stopped reading")

// wholeLines is where the last whole line of f before size ends, looked
// for back from size as far as from: a line still being written waits.
func wholeLines(f *os.File, from, size int64) int64 {
	buf := make([]byte, 64<<10)
	for at := size; at > from; {
		n := min(int64(len(buf)), at-from)
		at -= n
		if _, err := f.ReadAt(buf[:n], at); err != nil {
			return from
		}
		if i := bytes.LastIndexByte(buf[:n], '\n'); i >= 0 {
			return at + int64(i) + 1
		}
	}
	return from
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
