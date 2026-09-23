package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Job is one background session as Claude Code records it under jobs/<short>.
type Job struct {
	ID             string
	Account        string
	Name           string
	NameSource     string
	State          string // working, blocked, done, stopped
	Detail         string
	Tempo          string
	Needs          string
	Intent         string
	Cwd            string
	SessionID      string
	TranscriptPath string
	CLIVersion     string
	RespawnFlags   []string
	WorktreePath   string
	WorktreeBranch string
	Children       int
	CreatedAt      time.Time
	UpdatedAt      time.Time
	ModTime        time.Time
}

func (j Job) Live() bool { return j.State == "working" || j.State == "blocked" }

type jobFile struct {
	State          string          `json:"state"`
	Detail         string          `json:"detail"`
	Tempo          string          `json:"tempo"`
	Needs          json.RawMessage `json:"needs"`
	Intent         string          `json:"intent"`
	DisplayIntent  string          `json:"displayIntent"`
	Name           string          `json:"name"`
	NameSource     string          `json:"nameSource"`
	Cwd            string          `json:"cwd"`
	SessionID      string          `json:"sessionId"`
	LinkScanPath   string          `json:"linkScanPath"`
	CLIVersion     string          `json:"cliVersion"`
	RespawnFlags   []string        `json:"respawnFlags"`
	WorktreePath   string          `json:"worktreePath"`
	WorktreeBranch string          `json:"worktreeBranch"`
	Children       json.RawMessage `json:"children"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

func LoadJob(a Account, id string) (Job, error) {
	p := filepath.Join(a.JobsDir(), id, "state.json")
	st, err := os.Stat(p)
	if err != nil {
		return Job{}, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return Job{}, err
	}
	var f jobFile
	if err := json.Unmarshal(b, &f); err != nil {
		return Job{}, err
	}
	j := Job{
		ID: id, Account: a.Name, Name: f.Name, NameSource: f.NameSource,
		State: f.State, Detail: f.Detail, Tempo: f.Tempo, Intent: f.Intent,
		Cwd: f.Cwd, SessionID: f.SessionID, TranscriptPath: f.LinkScanPath,
		CLIVersion: f.CLIVersion, RespawnFlags: f.RespawnFlags,
		WorktreePath: f.WorktreePath, WorktreeBranch: f.WorktreeBranch,
		CreatedAt: f.CreatedAt, UpdatedAt: f.UpdatedAt, ModTime: st.ModTime(),
	}
	if f.DisplayIntent != "" {
		j.Intent = f.DisplayIntent
	}
	if len(f.Needs) > 0 && f.Needs[0] == '"' {
		_ = json.Unmarshal(f.Needs, &j.Needs)
	}
	if len(f.Children) > 0 && f.Children[0] == '[' {
		var c []json.RawMessage
		if json.Unmarshal(f.Children, &c) == nil {
			j.Children = len(c)
		}
	}
	if j.Name == "" {
		j.Name = id
	}
	if j.TranscriptPath == "" && j.SessionID != "" {
		j.TranscriptPath = filepath.Join(a.ProjectsDir(), ProjectSlug(j.Cwd), j.SessionID+".jsonl")
	}
	return j, nil
}

func ListJobIDs(a Account) []string {
	ents, err := os.ReadDir(a.JobsDir())
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(ents))
	for _, e := range ents {
		if e.IsDir() && len(e.Name()) == 8 {
			ids = append(ids, e.Name())
		}
	}
	sort.Strings(ids)
	return ids
}

// ProjectSlug is how Claude Code names a folder's project directory.
func ProjectSlug(dir string) string {
	b := []byte(dir)
	for i, c := range b {
		if c == '/' || c == '.' {
			b[i] = '-'
		}
	}
	return string(b)
}

type TimelineEntry struct {
	At     time.Time `json:"at"`
	State  string    `json:"state"`
	Detail string    `json:"detail"`
}

func ReadTimeline(a Account, id string) []TimelineEntry {
	b, err := os.ReadFile(filepath.Join(a.JobsDir(), id, "timeline.jsonl"))
	if err != nil {
		return nil
	}
	var out []TimelineEntry
	for len(b) > 0 {
		i := indexByte(b, '\n')
		line := b
		if i >= 0 {
			line, b = b[:i], b[i+1:]
		} else {
			b = nil
		}
		var e TimelineEntry
		if json.Unmarshal(line, &e) == nil {
			out = append(out, e)
		}
	}
	return out
}

func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

func pinsPath(a Account) string { return filepath.Join(a.JobsDir(), "pins.json") }

// ReadPins shares the native view's pin list so a pin shows in both views.
func ReadPins(a Account) []string {
	b, err := os.ReadFile(pinsPath(a))
	if err != nil {
		return nil
	}
	var ids []string
	_ = json.Unmarshal(b, &ids)
	return ids
}

func WritePins(a Account, ids []string) error {
	b, err := json.MarshalIndent(ids, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(pinsPath(a), b)
}

func writeAtomic(path string, b []byte) error {
	tmp := path + ".agtop.tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
