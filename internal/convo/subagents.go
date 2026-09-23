package convo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Subagent is one run of a subagent, as Claude Code records it beside the
// session's transcript: <session>/subagents/agent-<id>.jsonl and its meta.
type Subagent struct {
	ID          string
	Type        string `json:"agentType"`
	Description string `json:"description"`
	Model       string `json:"model"`
	ToolUseID   string `json:"toolUseId"`
	Path        string // its own transcript
	Mod         int64  // when it last wrote, for ordering
}

// ListSubagents finds the subagents of the session whose transcript is at
// path, oldest first.
func ListSubagents(transcript string) []Subagent { return new(Subagents).List(transcript) }

// Subagents lists a session's subagents again and again, reading each
// run's meta file only when it's new or has changed.
type Subagents struct {
	metas map[string]subMeta
}

type subMeta struct {
	mod  time.Time
	size int64
	sa   Subagent
	ok   bool
}

// List is ListSubagents.
func (l *Subagents) List(transcript string) []Subagent {
	dir := filepath.Join(strings.TrimSuffix(transcript, ".jsonl"), "subagents")
	metas, _ := filepath.Glob(filepath.Join(dir, "agent-*.meta.json"))
	if l.metas == nil {
		l.metas = map[string]subMeta{}
	}
	var out []Subagent
	for _, meta := range metas {
		st, err := os.Stat(meta)
		if err != nil {
			continue
		}
		m, seen := l.metas[meta]
		if !seen || !m.mod.Equal(st.ModTime()) || m.size != st.Size() {
			m = subMeta{mod: st.ModTime(), size: st.Size()}
			if b, err := os.ReadFile(meta); err == nil && json.Unmarshal(b, &m.sa) == nil {
				m.ok = true
				m.sa.ID = strings.TrimSuffix(strings.TrimPrefix(filepath.Base(meta), "agent-"), ".meta.json")
				m.sa.Path = filepath.Join(dir, "agent-"+m.sa.ID+".jsonl")
			}
			l.metas[meta] = m // a half-written one changes size, and is read again
		}
		if !m.ok {
			continue
		}
		sa := m.sa
		if st, err := os.Stat(sa.Path); err == nil {
			sa.Mod = st.ModTime().UnixNano()
		}
		out = append(out, sa)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Mod < out[j].Mod })
	return out
}

// Step finds a tool call by its id.
func (s *Session) Step(id string) *Step { return s.byID[id] }

// SubagentTail reads a subagent's transcript. Its lines are all marked as a
// sidechain, which a session's own Tail skips, so this one keeps them.
func SubagentTail(path string) *Tail { return &Tail{Path: path, Sess: New(), sidechain: true} }
