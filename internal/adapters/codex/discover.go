package codex

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/0xdeafcafe/agtop/internal/agent"
)

// Live is nil. Codex keeps no record of which threads are running that
// is cheap to read and can be trusted: its thread-writer-locks files
// outlive the processes that made them, and a lock is only held while
// codex runs, so telling a live one from a stale one means taking the
// lock, which could get in a running codex's way. Matching codex
// processes to rollouts would mean lsof on every refresh. Threads agtop
// runs itself come from the host, not from here.
func (Adapter) Live(agent.Profile) []agent.Session { return nil }

// headLines is how many lines of a rollout Past reads at most looking for
// its first prompt.
const headLines = 400

// Past is every rollout under the profile's sessions folder, newest
// first. Threads another thread started (spawned agents and the guardian
// reviewer) are left out: they belong to the thread that started them.
// A thread's name is the one Codex keeps in session_index.jsonl when it
// has one (only renamed or titled threads are there), else its first
// prompt.
func (Adapter) Past(p agent.Profile) []agent.Session {
	names := threadNames(filepath.Join(p.Dir, "session_index.jsonl"))
	var out []agent.Session
	_ = filepath.WalkDir(filepath.Join(p.Dir, "sessions"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasPrefix(d.Name(), "rollout-") || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		s, ok := pastSession(path)
		if !ok {
			return nil
		}
		s.Kind, s.Profile, s.State = Kind, p, "done"
		if n := names[s.ID]; n != "" {
			s.Name = oneLine(n)
		}
		out = append(out, s)
		return nil
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}

// pastSession reads what a list needs from the head of a rollout. ok is
// false for a subagent's rollout or one that can't be read.
func pastSession(path string) (agent.Session, bool) {
	f, err := os.Open(path)
	if err != nil {
		return agent.Session{}, false
	}
	defer f.Close()
	s := agent.Session{ID: idFromName(filepath.Base(path)), Transcript: path}
	if fi, err := f.Stat(); err == nil {
		s.UpdatedAt = fi.ModTime()
	}
	meta, sub, n := false, false, 0
	_ = readLines(f, func(b []byte) bool {
		n++
		var l rolloutLine
		if json.Unmarshal(b, &l) != nil {
			return n < headLines
		}
		if s.CreatedAt.IsZero() {
			s.CreatedAt = parseTime(l.Timestamp)
		}
		switch l.Type {
		case "session_meta":
			var m sessionMeta
			if meta || json.Unmarshal(l.Payload, &m) != nil {
				break
			}
			meta = true
			if m.subagent() {
				sub = true
				return false
			}
			if m.ID != "" {
				s.ID = m.ID
			}
			s.Cwd = m.Cwd
			if t := parseTime(m.Timestamp); !t.IsZero() {
				s.CreatedAt = t
			}
		case "turn_context":
			var c turnContext
			if s.Model == "" && json.Unmarshal(l.Payload, &c) == nil {
				s.Model = c.Model
			}
		case "response_item":
			var ri responseItem
			if s.Name == "" && json.Unmarshal(l.Payload, &ri) == nil && ri.Type == "message" && ri.Role == "user" {
				if m, ok := userPrompt(ri); ok {
					for _, p := range m.Parts {
						if s.Name == "" && strings.TrimSpace(p.Text) != "" {
							s.Name = oneLine(p.Text)
						}
					}
				}
			}
		}
		return n < headLines && (s.Name == "" || s.Model == "")
	})
	if sub {
		return agent.Session{}, false
	}
	if s.UpdatedAt.IsZero() {
		s.UpdatedAt = s.CreatedAt
	}
	return s, true
}

// idFromName is the thread id at the end of a rollout's file name,
// rollout-2026-09-22T17-01-09-<uuid>.jsonl.
func idFromName(name string) string {
	name = strings.TrimSuffix(name, ".jsonl")
	if len(name) >= 36 {
		return name[len(name)-36:]
	}
	return name
}

// threadNames is session_index.jsonl: each named thread's latest name.
func threadNames(path string) map[string]string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	names := map[string]string{}
	_ = readLines(f, func(b []byte) bool {
		var e struct {
			ID         string `json:"id"`
			ThreadName string `json:"thread_name"`
		}
		if json.Unmarshal(b, &e) == nil && e.ID != "" && strings.TrimSpace(e.ThreadName) != "" {
			names[e.ID] = e.ThreadName
		}
		return true
	})
	return names
}

// nameLen is how many characters of a prompt name a thread.
const nameLen = 80

// oneLine is text on one line, cut to nameLen characters.
func oneLine(text string) string {
	s := strings.Join(strings.Fields(text), " ")
	if r := []rune(s); len(r) > nameLen {
		return strings.TrimSpace(string(r[:nameLen-1])) + "…"
	}
	return s
}

var (
	_ agent.Discoverer    = Adapter{}
	_ agent.HistoryReader = Adapter{}
)
