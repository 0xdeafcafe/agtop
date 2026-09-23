package claude

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Totals is what one transcript file has cost so far. It is small and
// serialisable so the scan resumes from Offset instead of re-reading.
type Totals struct {
	Offset    int64                  `json:"o"`
	Size      int64                  `json:"s"`
	ByModel   map[string]*TokenUsage `json:"m,omitempty"`
	Cost      float64                `json:"c"`
	First     time.Time              `json:"f"`
	Last      time.Time              `json:"l"`
	PRs       []string               `json:"p,omitempty"`
	LastModel string                 `json:"lm,omitempty"`
	Days      map[string]float64     `json:"d,omitempty"`
	// pending is the newest assistant message; its usage can still change
	// while more content blocks of the same message are appended.
	PendingID    string     `json:"pi,omitempty"`
	PendingModel string     `json:"pm,omitempty"`
	PendingUse   TokenUsage `json:"pu"`
	PendingFast  bool       `json:"pf,omitempty"`
	PendingAt    time.Time  `json:"pa"`
}

func Day(t time.Time) string { return t.Local().Format("2006-01-02") }

func (t *Totals) commitPending() {
	if t.PendingID == "" {
		return
	}
	if t.ByModel == nil {
		t.ByModel = map[string]*TokenUsage{}
	}
	u := t.ByModel[t.PendingModel]
	if u == nil {
		u = &TokenUsage{}
		t.ByModel[t.PendingModel] = u
	}
	u.Add(t.PendingUse)
	c := Cost(t.PendingModel, t.PendingUse, t.PendingFast)
	t.Cost += c
	if t.Days == nil {
		t.Days = map[string]float64{}
	}
	t.Days[Day(t.PendingAt)] += c
	t.PendingID, t.PendingModel, t.PendingUse, t.PendingFast = "", "", TokenUsage{}, false
}

// Spend includes the pending message so a running agent's figure moves live.
func (t *Totals) Spend() (float64, TokenUsage) {
	var u TokenUsage
	for _, m := range t.ByModel {
		u.Add(*m)
	}
	u.Add(t.PendingUse)
	return t.Cost + Cost(t.PendingModel, t.PendingUse, t.PendingFast), u
}

// DaySpend is what was spent on one local calendar day.
func (t *Totals) DaySpend(day string) float64 {
	c := t.Days[day]
	if t.PendingID != "" && Day(t.PendingAt) == day {
		c += Cost(t.PendingModel, t.PendingUse, t.PendingFast)
	}
	return c
}

// Preview is the tail of a transcript: what the agent is doing right now.
type Preview struct {
	Text     string
	Tool     string
	ToolArg  string
	At       time.Time
	Model    string
	LastUser string
	Context  int64 // tokens the last request sent: how full the context window is
	Recent   []Event
}

// Event is one step of the conversation tail: a prompt, a reply or a tool call.
type Event struct {
	Role string // user, assistant, tool
	Text string
	At   time.Time
}

// ContextWindow is the model's window; everything current but Haiku has 1M.
func ContextWindow(model string) int64 {
	if strings.Contains(model, "haiku") {
		return 200_000
	}
	return 1_000_000
}

type line struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Message   struct {
		ID      string          `json:"id"`
		Model   string          `json:"model"`
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
		Usage   *struct {
			Input        int64  `json:"input_tokens"`
			Output       int64  `json:"output_tokens"`
			CacheRead    int64  `json:"cache_read_input_tokens"`
			CacheCreate  int64  `json:"cache_creation_input_tokens"`
			Speed        string `json:"speed"`
			CacheBreakup *struct {
				M5 int64 `json:"ephemeral_5m_input_tokens"`
				H1 int64 `json:"ephemeral_1h_input_tokens"`
			} `json:"cache_creation"`
		} `json:"usage"`
	} `json:"message"`
}

var (
	assistantMarker = []byte(`"type":"assistant"`)
	prURL           = regexp.MustCompile(`https://github\.com/[\w.-]+/[\w.-]+/pull/\d+`)
)

// Scan advances t over whatever was appended to path since t.Offset. Only
// complete lines are consumed, so a half-written line is read next time.
func Scan(path string, t *Totals, buf []byte) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return buf, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return buf, err
	}
	if st.Size() < t.Offset {
		*t = Totals{}
	}
	t.Size = st.Size()
	if st.Size() == t.Offset {
		return buf, nil
	}
	if _, err := f.Seek(t.Offset, io.SeekStart); err != nil {
		return buf, err
	}
	r := bufio.NewReaderSize(f, 256<<10)
	for {
		buf = buf[:0]
		complete := false
		for {
			chunk, err := r.ReadSlice('\n')
			buf = append(buf, chunk...)
			if err == nil {
				complete = true
				break
			}
			if err != bufio.ErrBufferFull {
				break
			}
		}
		if !complete {
			return buf, nil
		}
		t.Offset += int64(len(buf))
		consume(t, buf)
	}
}

func consume(t *Totals, b []byte) {
	if !bytes.Contains(b, assistantMarker) {
		return
	}
	var l line
	if json.Unmarshal(b, &l) != nil || l.Type != "assistant" {
		return
	}
	if !l.Timestamp.IsZero() {
		if t.First.IsZero() {
			t.First = l.Timestamp
		}
		t.Last = l.Timestamp
	}
	if bytes.Contains(b, []byte("/pull/")) {
		for _, m := range prURL.FindAll(b, -1) {
			addUnique(&t.PRs, string(m))
		}
	}
	m := l.Message
	if m.Usage == nil || m.Model == "" || m.Model == "<synthetic>" {
		return
	}
	if m.ID != t.PendingID {
		t.commitPending()
	}
	u := TokenUsage{Input: m.Usage.Input, Output: m.Usage.Output, CacheRead: m.Usage.CacheRead}
	if cb := m.Usage.CacheBreakup; cb != nil && cb.M5+cb.H1 > 0 {
		u.CacheWrite5m, u.CacheWrite1h = cb.M5, cb.H1
	} else {
		u.CacheWrite5m = m.Usage.CacheCreate
	}
	t.PendingID, t.PendingModel, t.PendingUse = m.ID, m.Model, u
	t.PendingFast = m.Usage.Speed == "fast"
	t.PendingAt = l.Timestamp
	t.LastModel = m.Model
}

func addUnique(s *[]string, v string) {
	for _, x := range *s {
		if x == v {
			return
		}
	}
	*s = append(*s, v)
}

// SubagentTranscripts lists the transcripts of subagents a session spawned.
func SubagentTranscripts(mainPath string) []string {
	dir := strings.TrimSuffix(mainPath, ".jsonl")
	m, _ := filepath.Glob(filepath.Join(dir, "subagents", "*.jsonl"))
	return m
}

// ReadPreview reads only the last window of a transcript.
func ReadPreview(path string, window int64) Preview {
	var p Preview
	f, err := os.Open(path)
	if err != nil {
		return p
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return p
	}
	off := st.Size() - window
	if off < 0 {
		off = 0
	}
	b := make([]byte, st.Size()-off)
	if _, err := f.ReadAt(b, off); err != nil && err != io.EOF {
		return p
	}
	lines := bytes.Split(b, []byte{'\n'})
	p.Recent = recentEvents(lines, 14)
	for i := len(lines) - 1; i >= 0 && (p.Text == "" || p.Tool == "" || p.LastUser == "" || p.Context == 0); i-- {
		var l line
		if json.Unmarshal(lines[i], &l) != nil {
			continue
		}
		if p.At.IsZero() && !l.Timestamp.IsZero() {
			p.At = l.Timestamp
		}
		var blocks []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		}
		if l.Type == "user" && p.LastUser == "" {
			var s string
			if json.Unmarshal(l.Message.Content, &s) == nil && s != "" && !strings.HasPrefix(s, "<") {
				p.LastUser = s
			}
			continue
		}
		if l.Type != "assistant" || json.Unmarshal(l.Message.Content, &blocks) != nil {
			continue
		}
		if p.Model == "" {
			p.Model = l.Message.Model
		}
		if u := l.Message.Usage; u != nil && p.Context == 0 && l.Message.Model != "<synthetic>" {
			p.Context = u.Input + u.CacheRead + u.CacheCreate
		}
		for j := len(blocks) - 1; j >= 0; j-- {
			bl := blocks[j]
			switch {
			case bl.Type == "text" && p.Text == "" && strings.TrimSpace(bl.Text) != "":
				p.Text = strings.TrimSpace(bl.Text)
			case bl.Type == "tool_use" && p.Tool == "":
				p.Tool, p.ToolArg = bl.Name, toolArg(bl.Input)
			}
		}
	}
	return p
}

func toolArg(in json.RawMessage) string {
	var m map[string]any
	if json.Unmarshal(in, &m) != nil {
		return ""
	}
	for _, k := range []string{"description", "command", "file_path", "pattern", "prompt", "url", "query"} {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func recentEvents(lines [][]byte, n int) []Event {
	var out []Event
	for _, raw := range lines {
		var l line
		if json.Unmarshal(raw, &l) != nil || (l.Type != "user" && l.Type != "assistant") {
			continue
		}
		var str string
		if l.Type == "user" && json.Unmarshal(l.Message.Content, &str) == nil {
			if str != "" && !strings.HasPrefix(str, "<") {
				out = append(out, Event{Role: "user", Text: str, At: l.Timestamp})
			}
			continue
		}
		var blocks []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		}
		if json.Unmarshal(l.Message.Content, &blocks) != nil {
			continue
		}
		for _, bl := range blocks {
			switch {
			case bl.Type == "text" && strings.TrimSpace(bl.Text) != "":
				if l.Type == "user" {
					continue // text blocks from the user side are harness notes, not prompts
				} else {
					out = append(out, Event{Role: "assistant", Text: strings.TrimSpace(bl.Text), At: l.Timestamp})
				}
			case bl.Type == "tool_use":
				out = append(out, Event{Role: "tool", Text: bl.Name + "\x00" + toolArg(bl.Input), At: l.Timestamp})
			}
		}
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

// SubagentStats counts a session's subagents from their metadata files:
// how many it ever spawned, and how many are running (written in the last
// 90s) directly and at any depth.
type SubagentStats struct {
	Spawned, Direct, Nested int
}

func ReadSubagentStats(mainPath string, now time.Time) SubagentStats {
	var st SubagentStats
	dir := filepath.Join(strings.TrimSuffix(mainPath, ".jsonl"), "subagents")
	metas, _ := filepath.Glob(filepath.Join(dir, "*.meta.json"))
	for _, meta := range metas {
		st.Spawned++
		fi, err := os.Stat(strings.TrimSuffix(meta, ".meta.json") + ".jsonl")
		if err != nil || now.Sub(fi.ModTime()) > 90*time.Second {
			continue
		}
		var m struct {
			Depth int `json:"spawnDepth"`
		}
		if b, err := os.ReadFile(meta); err == nil {
			_ = json.Unmarshal(b, &m)
		}
		if m.Depth <= 1 {
			st.Direct++
		} else {
			st.Nested++
		}
	}
	return st
}
