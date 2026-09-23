package convo

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/0xdeafcafe/agtop/internal/headless"
	"github.com/0xdeafcafe/agtop/internal/host"
)

// Tail follows a Claude Code transcript file, so a session agtop doesn't
// run itself draws exactly like one it does: the transcript holds the same
// messages the stream carries. Each Read takes in only what's new.
type Tail struct {
	Path string
	Sess *Session

	off       int64
	partial   []byte
	sidechain bool // a subagent's own transcript: its lines are the story
}

func NewTail(path string) *Tail { return &Tail{Path: path, Sess: New()} }

type tline struct {
	Type          string          `json:"type"`
	Subtype       string          `json:"subtype"`
	IsMeta        bool            `json:"isMeta"`
	IsSidechain   bool            `json:"isSidechain"`
	Timestamp     time.Time       `json:"timestamp"`
	Message       json.RawMessage `json:"message"`
	ToolUseResult json.RawMessage `json:"toolUseResult"`
	Cwd           string          `json:"cwd"`
	Effort        string          `json:"effort"`
}

// Read applies whatever has been appended since the last call and reports
// whether anything changed.
func (t *Tail) Read() (bool, error) {
	f, err := os.Open(t.Path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return false, err
	}
	if st.Size() < t.off { // rewritten from scratch: start over
		t.off, t.partial, t.Sess = 0, nil, New()
	}
	if st.Size() == t.off {
		return false, nil
	}
	if _, err := f.Seek(t.off, io.SeekStart); err != nil {
		return false, err
	}
	r := bufio.NewReaderSize(f, 1<<20)
	changed := false
	for {
		chunk, err := r.ReadBytes('\n')
		t.off += int64(len(chunk))
		if err != nil {
			// An unfinished last line waits for the rest of it.
			t.partial = append(t.partial, chunk...)
			break
		}
		line := chunk
		if len(t.partial) > 0 {
			line = append(t.partial, chunk...)
			t.partial = nil
		}
		if t.apply(bytes.TrimSpace(line)) {
			changed = true
		}
	}
	return changed, nil
}

func (t *Tail) apply(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	var l tline
	if json.Unmarshal(b, &l) != nil || l.IsSidechain != t.sidechain || l.IsMeta {
		return false
	}
	s := t.Sess
	if l.Cwd != "" && s.Cwd == "" {
		s.Cwd = l.Cwd
	}
	at := l.Timestamp
	switch l.Type {
	case "system":
		if l.Subtype == "turn_duration" {
			s.Apply(headless.Result{Subtype: "success"}, at)
			return true
		}
		return false
	case "user":
		var m struct {
			Content json.RawMessage `json:"content"`
		}
		_ = json.Unmarshal(l.Message, &m)
		if text, images, ok := prompt(m.Content); ok {
			// A shell command you ran with ! is its own small turn, and
			// its output lands on it rather than starting another.
			if out, ok := shellOutput(text); ok {
				if live := s.Live(); live != nil && strings.HasPrefix(live.Prompt, "! ") {
					s.shellResult(live, out, at)
				}
				return true
			}
			// A new prompt closes a turn the transcript never marked done.
			if live := s.Live(); live != nil && live.Prompt != "" {
				s.Apply(headless.Result{Subtype: "success"}, at)
			}
			from, text2, injected := Injected(text)
			if injected {
				text = text2
				s.noteTask(m.Content)
			}
			s.Apply(host.Sent{Text: text, Images: images}, at)
			if injected {
				s.Turns[len(s.Turns)-1].From = from
			}
			if cmd, ok := strings.CutPrefix(text, "! "); ok {
				s.shellStart(cmd, at)
			}
			if n := len(s.Turns); n > 0 && l.Effort != "" {
				s.Turns[n-1].Effort = l.Effort
			}
			return true
		}
		fallthrough
	case "assistant":
		stream, _ := json.Marshal(map[string]any{"type": l.Type, "message": l.Message, "tool_use_result": l.ToolUseResult})
		ev, err := headless.Decode(stream)
		if err != nil {
			return false
		}
		if msg, ok := ev.(headless.Message); ok && l.Type == "assistant" {
			if live := s.Live(); live != nil && l.Effort != "" && live.Effort == "" {
				live.Effort = l.Effort
			}
			_ = msg
		}
		s.Apply(ev, at)
		return true
	}
	return false
}

// prompt reads a user line as something you typed: plain text or text and
// image blocks, not a tool result. Command wrappers are unwrapped.
func prompt(raw json.RawMessage) (string, []string, bool) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return cleanPrompt(s), nil, strings.TrimSpace(s) != ""
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil || len(blocks) == 0 {
		return "", nil, false
	}
	var texts, images []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			texts = append(texts, b.Text)
		case "image":
			images = append(images, "image")
		default:
			return "", nil, false // a tool result
		}
	}
	return cleanPrompt(strings.Join(texts, "\n")), images, true
}

// cleanPrompt drops the markup Claude Code wraps around slash commands and
// system notes, keeping what you actually said.
func cleanPrompt(s string) string {
	if cmd := between(s, "<bash-input>", "</bash-input>"); cmd != "" {
		return "! " + cmd
	}
	if strings.Contains(s, "<bash-stdout>") || strings.Contains(s, "<bash-stderr>") {
		return s // kept whole for shellOutput
	}
	if name := between(s, "<command-name>", "</command-name>"); name != "" {
		args := between(s, "<command-args>", "</command-args>")
		return strings.TrimSpace(name + " " + args)
	}
	for _, tag := range []string{"system-reminder", "local-command-stdout", "local-command-caveat"} {
		for {
			i := strings.Index(s, "<"+tag+">")
			j := strings.Index(s, "</"+tag+">")
			if i < 0 || j < i {
				break
			}
			s = s[:i] + s[j+len(tag)+3:]
		}
	}
	return strings.TrimSpace(s)
}

func between(s, a, b string) string {
	i := strings.Index(s, a)
	if i < 0 {
		return ""
	}
	rest := s[i+len(a):]
	j := strings.Index(rest, b)
	if j < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:j])
}

// shellOutput reads the line Claude Code writes after a ! command.
func shellOutput(s string) (shellOut, bool) {
	if !strings.Contains(s, "<bash-stdout>") && !strings.Contains(s, "<bash-stderr>") {
		return shellOut{}, false
	}
	return shellOut{stdout: between(s, "<bash-stdout>", "</bash-stdout>"), stderr: between(s, "<bash-stderr>", "</bash-stderr>")}, true
}

type shellOut struct{ stdout, stderr string }

func (s *Session) shellStart(cmd string, at time.Time) {
	t := s.Live()
	if t == nil {
		return
	}
	id := fmt.Sprintf("you-%d", t.N)
	in, _ := json.Marshal(map[string]string{"command": cmd, "description": "you ran"})
	st := &Step{ID: id, Tool: "Bash", Input: in, Start: at, Exit: -1}
	s.byID[id] = st
	t.steps[id] = st
	t.Items = append(t.Items, &Item{Kind: KStep, Step: st})
	t.touch()
}

func (s *Session) shellResult(t *Turn, out shellOut, at time.Time) {
	st := s.byID[fmt.Sprintf("you-%d", t.N)]
	if st == nil {
		return
	}
	stdout := out.stdout
	if stdout == "(Bash completed with no output)" {
		stdout = ""
	}
	st.Output, st.End, st.Status = strings.TrimSpace(stdout+"\n"+out.stderr), at, OK
	st.Result, _ = json.Marshal(map[string]string{"stdout": stdout, "stderr": out.stderr})
	if strings.TrimSpace(out.stderr) != "" {
		st.Status = Failed
	}
	s.Apply(headless.Result{Subtype: "success"}, at)
}

// Injected recognises text Claude Code puts in a user message that you
// didn't type, and says who it's from and what it says in a line.
func Injected(s string) (from, text string, ok bool) {
	t := strings.TrimSpace(s)
	switch {
	case strings.HasPrefix(t, "<task-notification>"):
		status := between(t, "<status>", "</status>")
		sum := between(t, "<summary>", "</summary>")
		from = "background task"
		if status != "" {
			from += " · " + status
		}
		return from, firstLine(firstNonEmpty(sum, "finished")), true
	case strings.HasPrefix(t, "<cross-session-message"):
		name := attr(t, "from-name")
		body := t[strings.Index(t, ">")+1:]
		body = strings.TrimSuffix(strings.TrimSpace(body), "</cross-session-message>")
		return "message from " + firstNonEmpty(name, "another session"), firstLine(body), true
	case strings.HasPrefix(t, "<agent-message"):
		body := t[strings.Index(t, ">")+1:]
		body = strings.TrimSuffix(strings.TrimSpace(body), "</agent-message>")
		return "a subagent reported back", firstLine(stripTags(body)), true
	case strings.HasPrefix(t, "<") && strings.Contains(t, "</"):
		// Some other wrapper: drop the tags, keep the words.
		return "Claude Code", firstLine(stripTags(t)), true
	}
	return "", s, false
}

func attr(s, name string) string {
	i := strings.Index(s, name+`="`)
	if i < 0 {
		return ""
	}
	rest := s[i+len(name)+2:]
	if j := strings.IndexByte(rest, '"'); j >= 0 {
		return rest[:j]
	}
	return ""
}

var tagRe = regexp.MustCompile(`<[^>]{1,80}>`)

func stripTags(s string) string { return strings.TrimSpace(tagRe.ReplaceAllString(s, " ")) }

// noteTask records a background task's reported status, from the
// notification Claude Code injects when one finishes.
func (s *Session) noteTask(raw json.RawMessage) {
	var t string
	if json.Unmarshal(raw, &t) != nil {
		var blocks []struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(raw, &blocks)
		for _, b := range blocks {
			t += b.Text
		}
	}
	if id := between(t, "<task-id>", "</task-id>"); id != "" {
		s.TaskStatus[id] = firstNonEmpty(between(t, "<status>", "</status>"), "completed")
	}
}
