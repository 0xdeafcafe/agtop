package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Session is a live Claude Code process as it registers itself under
// sessions/<pid>.json. Interactive terminals only appear here.
type Session struct {
	PID       int    `json:"pid"`
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd"`
	Kind      string `json:"kind"` // bg or interactive
	Name      string `json:"name"`
	Status    string `json:"status"` // busy, idle, shell
	JobID     string `json:"jobId"`
	StartedMs int64  `json:"startedAt"`
	UpdatedMs int64  `json:"updatedAt"`
	StatusMs  int64  `json:"statusUpdatedAt"`
}

func (s Session) StatusAt() time.Time { return time.UnixMilli(s.StatusMs) }

func (s Session) StartedAt() time.Time { return time.UnixMilli(s.StartedMs) }
func (s Session) UpdatedAt() time.Time {
	if s.UpdatedMs == 0 {
		return s.StartedAt()
	}
	return time.UnixMilli(s.UpdatedMs)
}

// ReadSessions lists sessions whose process is still alive.
func ReadSessions(a Account) []Session {
	dir := filepath.Join(a.ConfigDir, "sessions")
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Session
	for _, e := range ents {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var s Session
		if json.Unmarshal(b, &s) != nil || s.PID <= 0 {
			continue
		}
		if syscall.Kill(s.PID, 0) != nil {
			continue
		}
		out = append(out, s)
	}
	return out
}
