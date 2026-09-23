package convo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
func ListSubagents(transcript string) []Subagent {
	dir := filepath.Join(strings.TrimSuffix(transcript, ".jsonl"), "subagents")
	metas, _ := filepath.Glob(filepath.Join(dir, "agent-*.meta.json"))
	var out []Subagent
	for _, meta := range metas {
		b, err := os.ReadFile(meta)
		if err != nil {
			continue
		}
		var sa Subagent
		if json.Unmarshal(b, &sa) != nil {
			continue
		}
		sa.ID = strings.TrimSuffix(strings.TrimPrefix(filepath.Base(meta), "agent-"), ".meta.json")
		sa.Path = filepath.Join(dir, "agent-"+sa.ID+".jsonl")
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
