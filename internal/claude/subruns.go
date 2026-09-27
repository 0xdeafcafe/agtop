package claude

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// A subagent run's transcript says little about whether it is still going:
// one busy with a long command, or a long think, writes nothing for
// minutes. Its parent's transcript says more. The Agent (or Task) call
// that started it has no result while a run the turn waits on works; one
// launched in the background has a result at once, and a task
// notification when it finishes. SubagentRuns follows those.

// RunStale is how long a run the transcripts still call running may go
// without writing before it's taken to have died with its process.
const RunStale = 15 * time.Minute

// runQuiet is how recently a run the transcripts say nothing about must
// have written to count as running.
const runQuiet = 90 * time.Second

// RunState is how a session's transcripts say a subagent run stands.
type RunState int

const (
	RunUnknown RunState = iota // no word of its call: judge by its writing
	RunRunning
	RunDone
)

// SubagentRuns follows a session's transcript, and its runs' own when one
// run started another, reading only what each has gained since.
type SubagentRuns struct {
	// Gone says the session's own process is known to have exited: a run
	// the transcripts left unfinished ended with it, at once, rather than
	// after RunStale. Left false when that can't be told.
	Gone bool

	path   string
	files  map[string]int64      // how far each transcript has been read
	calls  map[string]*agentCall // Agent calls, by tool_use id
	ends   map[string]runEnd     // finished runs, by agent id and by tool_use id
	woken  map[string]int        // runs a message was sent to, by agent id: when
	seq    int                   // lines read, which orders what they say
	metas  map[string]runMeta    // by meta file
	dirMod time.Time
	names  []string // the meta files as of dirMod
	listed time.Time
	buf    []byte
}

type agentCall struct {
	seq    int    // when it was made
	resSeq int    // when its result came
	result bool   // it has its result
	async  bool   // the result was the launch of a background run
	status string // how a run the turn waited on ended
	at     time.Time
}

type runEnd struct {
	status string
	at     time.Time
	seq    int
}

type runMeta struct {
	mod     time.Time
	size    int64
	id      string
	toolUse string
	depth   int
}

// SubagentRun is one run as SubagentRuns knows it.
type SubagentRun struct {
	ID, ToolUseID string
	Depth         int       // 1 for one the session started, more for one a run started
	Mod           time.Time // when its transcript was last written
}

func (r *SubagentRuns) reset(path string) {
	*r = SubagentRuns{Gone: r.Gone, path: path, files: map[string]int64{}, calls: map[string]*agentCall{},
		ends: map[string]runEnd{}, woken: map[string]int{}, metas: map[string]runMeta{}}
}

// Update reads what the session's transcript at path, and its runs' when
// needed, have gained, and returns its runs.
func (r *SubagentRuns) Update(path string) []SubagentRun {
	if r.files == nil || r.path != path {
		r.reset(path)
	}
	runs := r.list()
	if len(runs) == 0 {
		return nil
	}
	r.read(path)
	// A run a run started has its call, and word of its end, in that run's
	// transcript: those are read only while such a run hasn't ended.
	depths := map[int]bool{}
	for _, x := range runs {
		if x.Depth > 1 && !depths[x.Depth-1] {
			if st, _, _ := r.State(x.ID, x.ToolUseID); st != RunDone {
				depths[x.Depth-1] = true
			}
		}
	}
	dir := r.dir()
	for _, x := range runs {
		if depths[x.Depth] {
			r.read(filepath.Join(dir, "agent-"+x.ID+".jsonl"))
		}
	}
	return runs
}

func (r *SubagentRuns) dir() string {
	return filepath.Join(strings.TrimSuffix(r.path, ".jsonl"), "subagents")
}

// list finds the runs from their meta files, reading each only when it's
// new or changed, and the folder only when something was added to it (or
// every 10s).
func (r *SubagentRuns) list() []SubagentRun {
	dir := r.dir()
	st, err := os.Stat(dir)
	if err != nil {
		r.names = nil
		return nil
	}
	if !st.ModTime().Equal(r.dirMod) || time.Since(r.listed) > 10*time.Second {
		r.names, _ = filepath.Glob(filepath.Join(dir, "agent-*.meta.json"))
		r.dirMod, r.listed = st.ModTime(), time.Now()
	}
	out := make([]SubagentRun, 0, len(r.names))
	for _, p := range r.names {
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		m, ok := r.metas[p]
		if !ok || !m.mod.Equal(fi.ModTime()) || m.size != fi.Size() {
			m = runMeta{mod: fi.ModTime(), size: fi.Size(), depth: 1,
				id: strings.TrimSuffix(strings.TrimPrefix(filepath.Base(p), "agent-"), ".meta.json")}
			var v struct {
				ToolUse string `json:"toolUseId"`
				Depth   int    `json:"spawnDepth"`
			}
			if b, err := os.ReadFile(p); err == nil && json.Unmarshal(b, &v) == nil {
				m.toolUse = v.ToolUse
				m.depth = max(1, v.Depth)
			}
			r.metas[p] = m
		}
		run := SubagentRun{ID: m.id, ToolUseID: m.toolUse, Depth: m.depth}
		if fi, err := os.Stat(filepath.Join(dir, "agent-"+m.id+".jsonl")); err == nil {
			run.Mod = fi.ModTime()
		}
		out = append(out, run)
	}
	return out
}

// read takes in the complete lines a transcript has gained.
func (r *SubagentRuns) read(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return
	}
	off := r.files[path]
	if st.Size() < off {
		off = 0 // rewritten
	}
	if st.Size() == off {
		return
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return
	}
	br := bufio.NewReaderSize(f, 64<<10)
	for {
		r.buf = r.buf[:0]
		complete := false
		for {
			chunk, err := br.ReadSlice('\n')
			r.buf = append(r.buf, chunk...)
			if err == nil {
				complete = true
				break
			}
			if err != bufio.ErrBufferFull {
				break
			}
		}
		if !complete {
			break // a line still being written is read whole next time
		}
		off += int64(len(r.buf))
		r.line(r.buf)
	}
	r.files[path] = off
	if cap(r.buf) > 1<<20 {
		r.buf = nil
	}
}

var (
	toolUseMark    = []byte(`"tool_use"`)
	agentNameMarks = [][]byte{[]byte(`"name":"Agent"`), []byte(`"name":"Task"`), []byte(`"name":"SendMessage"`)}
	toolResultMark = []byte(`"tool_result"`)
	notifyOpen     = []byte("<task-notification>")
	notifyClose    = []byte("</task-notification>")
	asyncMarks     = [][]byte{[]byte(`"async_launched"`), []byte("Async agent launched")}
	stampMark      = []byte(`"timestamp":"`)
)

func (r *SubagentRuns) line(b []byte) {
	r.seq++
	if bytes.Contains(b, toolUseMark) && (bytes.Contains(b, agentNameMarks[0]) || bytes.Contains(b, agentNameMarks[1]) || bytes.Contains(b, agentNameMarks[2])) {
		for _, bl := range lineBlocks(b) {
			switch {
			case bl.Type != "tool_use":
			case (bl.Name == "Agent" || bl.Name == "Task") && r.calls[bl.ID] == nil:
				r.calls[bl.ID] = &agentCall{seq: r.seq}
			case bl.Name == "SendMessage":
				// A message to a finished run wakes it, until it next ends.
				var in struct {
					To string `json:"to"`
				}
				if json.Unmarshal(bl.Input, &in) == nil && in.To != "" {
					r.woken[in.To] = r.seq
				}
			}
		}
	}
	if len(r.calls) > 0 && bytes.Contains(b, toolResultMark) {
		for _, bl := range lineBlocks(b) {
			c := r.calls[bl.ToolUseID]
			if bl.Type != "tool_result" || c == nil || c.result {
				continue
			}
			c.result, c.at, c.resSeq = true, lineTime(b), r.seq
			c.async = bytes.Contains(b, asyncMarks[0]) || bytes.Contains(b, asyncMarks[1])
			c.status = "completed"
			switch {
			case bytes.Contains(bl.Content, []byte("[Request interrupted")):
				c.status = "stopped"
			case bl.IsError:
				c.status = "failed"
			}
		}
	}
	for rest := b; ; {
		i := bytes.Index(rest, notifyOpen)
		if i < 0 {
			break
		}
		rest = rest[i+len(notifyOpen):]
		seg := rest
		if j := bytes.Index(rest, notifyClose); j >= 0 {
			seg = rest[:j]
		}
		status := tagTexts(seg, "status")
		if len(status) == 0 {
			continue
		}
		end := runEnd{status: status[0], at: lineTime(b), seq: r.seq}
		if end.status == "killed" {
			end.status = "stopped"
		}
		// One notice can tell of several runs ending (a session that
		// ended with them running); the same one comes again as it's
		// queued and delivered, which changes nothing.
		for _, id := range append(tagTexts(seg, "task-id"), tagTexts(seg, "tool-use-id")...) {
			if e, seen := r.ends[id]; !seen || e.status != end.status || r.woken[id] > e.seq {
				r.ends[id] = end
			}
		}
	}
}

type lineBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
	Input     json.RawMessage `json:"input"`
	Content   json.RawMessage `json:"content"`
}

// lineBlocks are a transcript line's message's content blocks.
func lineBlocks(b []byte) []lineBlock {
	var l struct {
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(b, &l) != nil {
		return nil
	}
	var bl []lineBlock
	_ = json.Unmarshal(l.Message.Content, &bl) // a plain-text message has none
	return bl
}

// lineTime is a transcript line's time, or now.
func lineTime(b []byte) time.Time {
	if i := bytes.Index(b, stampMark); i >= 0 {
		s := b[i+len(stampMark):]
		if j := bytes.IndexByte(s, '"'); j > 0 {
			if t, err := time.Parse(time.RFC3339Nano, string(s[:j])); err == nil {
				return t
			}
		}
	}
	return time.Now()
}

// tagTexts is what's inside each <tag>…</tag> in s.
func tagTexts(s []byte, tag string) []string {
	open, end := []byte("<"+tag+">"), []byte("</"+tag+">")
	var out []string
	for {
		i := bytes.Index(s, open)
		if i < 0 {
			return out
		}
		s = s[i+len(open):]
		j := bytes.Index(s, end)
		if j < 0 {
			return out
		}
		if t := strings.TrimSpace(string(s[:j])); t != "" {
			out = append(out, t)
		}
		s = s[j+len(end):]
	}
}

// State is how the run with this agent id, started by this call, stands:
// and when it has ended, how and when.
// Whatever the transcripts said last counts: a run launched, or woken by a
// message, runs until a notice says it ended.
func (r *SubagentRuns) State(id, toolUseID string) (RunState, string, time.Time) {
	run, done := -1, -1
	var end runEnd
	if c := r.calls[toolUseID]; c != nil && toolUseID != "" {
		switch {
		case !c.result:
			run = c.seq
		case c.async:
			run = c.resSeq
		default:
			done, end = c.resSeq, runEnd{status: c.status, at: c.at}
		}
	}
	for _, k := range []string{id, toolUseID} {
		if e, ok := r.ends[k]; ok && k != "" && e.seq > done {
			done, end = e.seq, e
		}
	}
	if w, ok := r.woken[id]; ok && id != "" && w > run {
		run = w
	}
	switch {
	case run < 0 && done < 0:
		return RunUnknown, "", time.Time{}
	case run > done:
		return RunRunning, "", time.Time{}
	}
	return RunDone, end.status, end.at
}

// Going is whether a run is still working, last written at mod, and how it
// ended if it has. The transcripts' word counts: a run they call running
// is, unless its session's process is Gone ("ended"), or, when that can't
// be told, it's been silent past RunStale (it died with its process);
// one they call done is, unless it's written since (a message sent to it
// woke it). With no word, it's running while it writes.
func (r *SubagentRuns) Going(id, toolUseID string, mod, now time.Time) (bool, string) {
	st, status, at := r.State(id, toolUseID)
	recent := !mod.IsZero() && now.Sub(mod) < runQuiet
	switch st {
	case RunRunning:
		if r.Gone {
			return false, "ended"
		}
		return mod.IsZero() || now.Sub(mod) < RunStale, ""
	case RunDone:
		if recent && mod.After(at.Add(5*time.Second)) {
			return true, ""
		}
		return false, status
	}
	return recent, ""
}

// Stats counts the session's runs, and those still working, directly and
// at any depth.
func (r *SubagentRuns) Stats(path string, now time.Time) SubagentStats {
	var st SubagentStats
	for _, x := range r.Update(path) {
		st.Spawned++
		if going, _ := r.Going(x.ID, x.ToolUseID, x.Mod, now); !going {
			continue
		}
		if x.Depth <= 1 {
			st.Direct++
		} else {
			st.Nested++
		}
	}
	return st
}
