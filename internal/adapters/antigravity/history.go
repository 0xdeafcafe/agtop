package antigravity

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/event"
	"github.com/0xdeafcafe/rush/internal/state"
)

type savedEvent struct {
	At    time.Time       `json:"at"`
	Event json.RawMessage `json:"event"`
}

func historyPath(id string) string {
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, "/\\\x00") {
		return ""
	}
	return filepath.Join(state.Dir(), "antigravity-history", id+".jsonl")
}

// Rush keeps its own normalized transcript; agy's native conversations are
// resumed by ID and are never rewritten or mistaken for Gemini CLI sessions.
func (c *conn) remember(ev event.Event) error {
	switch ev.(type) {
	case event.Message, event.Init, event.TurnEnd, event.CallUpdated:
	default:
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if init, ok := ev.(event.Init); ok && init.SessionID != "" && init.SessionID != c.session {
		next := historyPath(init.SessionID)
		if next == "" {
			return errors.New("invalid Antigravity conversation ID")
		}
		if c.journal != nil {
			if _, err := os.Stat(next); err == nil {
				return errors.New("Antigravity returned an existing conversation ID for a new session")
			}
			_ = c.journal.Close()
			if err := os.Rename(c.journal.Name(), next); err != nil {
				return err
			}
			c.journal = nil
		}
		c.session = init.SessionID
	}
	return c.saveEvent(ev)
}

// saveEvent is called with mu held.
func (c *conn) saveEvent(ev event.Event) error {
	path := historyPath(c.session)
	if path == "" {
		return errors.New("missing Antigravity conversation ID")
	}
	if c.journal == nil {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		c.journal = f
	}
	raw, err := event.Marshal(ev)
	if err != nil {
		return err
	}
	b, err := json.Marshal(savedEvent{At: time.Now(), Event: raw})
	if err != nil {
		return err
	}
	_, err = c.journal.Write(append(b, '\n'))
	return err
}
func (Adapter) History(s agent.Session, before time.Time) ([]event.Event, error) {
	return readHistory(s, 0, before)
}
func (Adapter) HistoryTail(s agent.Session, most int64) ([]event.Event, bool, error) {
	path := historyPath(s.ID)
	st, err := os.Stat(path)
	if err != nil {
		return nil, false, err
	}
	offset := max(0, st.Size()-max(1, most))
	evs, err := readHistory(s, offset, time.Time{})
	return evs, offset > 0, err
}
func readHistory(s agent.Session, offset int64, before time.Time) ([]event.Event, error) {
	path := historyPath(s.ID)
	if path == "" {
		return nil, errors.New("invalid Antigravity conversation ID")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if offset > 0 {
		if _, err = f.Seek(offset-1, io.SeekStart); err != nil {
			return nil, err
		}
	}
	r := bufio.NewReader(f)
	if offset > 0 {
		for {
			_, e := r.ReadSlice('\n')
			if e != bufio.ErrBufferFull {
				break
			}
		}
	}
	scan := bufio.NewScanner(r)
	scan.Buffer(make([]byte, 64<<10), 8<<20)
	var out []event.Event
	for scan.Scan() {
		var s savedEvent
		if json.Unmarshal(scan.Bytes(), &s) != nil {
			continue
		}
		if !before.IsZero() && !s.At.Before(before) {
			break
		}
		ev, err := event.Unmarshal(s.Event)
		if err == nil {
			out = append(out, ev)
		}
	}
	return out, scan.Err()
}

func (Adapter) TranscriptPath(_ agent.Profile, _ string, sid string) string { return historyPath(sid) }
