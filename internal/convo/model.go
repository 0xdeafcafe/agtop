// Package convo turns an agtop-mode session's event stream into turns and
// steps, and draws them in agtop's own style: folding turns headed by your
// words, one row per tool call with its outcome on the right, and output
// that opens by itself when something failed.
//
// It has no terminal or Bubble Tea dependency, so it can be tested as plain
// data in, lines out.
package convo

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/0xdeafcafe/agtop/internal/headless"
	"github.com/0xdeafcafe/agtop/internal/host"
)

// Status is where a step is.
type Status int

const (
	Running Status = iota
	OK
	Failed
	Waiting // an approval is pending
	Denied  // refused, by you or by a rule
	Lost    // its turn ended or Claude died before it reported back
)

// Step is one tool call.
type Step struct {
	ID       string
	Tool     string
	Input    json.RawMessage
	Status   Status
	Output   string          // the tool result as text
	Result   json.RawMessage // Claude Code's structured result (patches, stdout/stderr)
	Exit     int             // Bash exit code, -1 when unknown
	Start    time.Time
	End      time.Time
	Children []*Step // a subagent's own steps
	Approval *headless.PermissionRequest

	parent *Step
	turn   *Turn // the turn whose steps hold it
}

// Kind of an item in a turn.
type Kind int

const (
	KText Kind = iota
	KThinking
	KStep
	KInterject // you, sending mid-turn
	KCompact   // the conversation was compacted here; Text is the summary
)

// Item is one thing in a turn, in order.
type Item struct {
	Kind    Kind
	Text    string
	Compact *headless.Compact
	Step    *Step
	Answer  bool // the turn's final words, promoted when the turn ends
}

// Turn runs from your message to Claude's last word.
type Turn struct {
	N       int
	Prompt  string
	Items   []*Item
	Start   time.Time
	End     time.Time
	Live    bool
	Cost    float64
	Err     string // why it ended badly, if it did
	Stopped bool   // you stopped it
	Model   string // the main agent's model for this turn
	Effort  string
	Images  []string // names of images sent with the prompt
	// From is set when the turn wasn't started by you: a background task
	// reporting back, another session's message, a subagent's report.
	From string
	// Streamed counts what Claude has written this turn as it streams
	// (text, thinking and tool input), for a live token estimate; Thinking
	// is when the thinking now under way began.
	Streamed int
	Thinking time.Time

	steps map[string]*Step
	ver   int

	ref     string // "t13", made once
	waits   bool   // a step waits on you, as of waitVer-1
	waitVer int
}

// Outcome is the first line of the turn's answer.
func (t *Turn) Outcome() string {
	for i := len(t.Items) - 1; i >= 0; i-- {
		if it := t.Items[i]; it.Kind == KText && it.Answer {
			return firstPlain(it.Text)
		}
	}
	return ""
}

// Steps counts every tool call in the turn, subagents' included.
func (t *Turn) Steps() int { return len(t.steps) }

func (t *Turn) touch() { t.ver++ }

// Task is one entry in the agent's task list.
type Task struct {
	ID      string
	Subject string
	Active  string // present-tense form, shown while in progress
	Status  string // pending, in_progress, completed
}

// Request is one call to the model, as its usage reports it.
type Request struct {
	ID    string
	At    time.Time
	Model string
	Agent string // "" for the main agent, else the subagent's type
	Run   string // the tool call that started a subagent; "" for the main agent
	Usage headless.Usage
}

// ToolStat totals one tool's calls.
type ToolStat struct {
	Name   string
	Calls  int
	Failed int
	Time   time.Duration
}

// Session is everything known about one agtop-mode session.
type Session struct {
	Turns    []*Turn
	Info     host.Info
	Commands []headless.Command
	Tasks    []Task
	Model    string
	Cwd      string // where Claude Code says it's running
	Context  int    // tokens in the context window after the last request
	Limit    string
	Requests []Request
	Tools    map[string]*ToolStat

	streaming  *Item
	byID       map[string]*Step
	cache      map[*Turn]cached
	memo       map[memoKey][]Line
	memoOld    map[memoKey][]Line
	stepVer    int           // bumped whenever a step is added or changes
	changes    []*FileChange // Changes, as of changesVer
	searchHits []Hit         // the last search, for searchKey
	searchKey  string
	changesVer int
	editList   []edit
	editsVer   int
	rail       map[*Step]railBlock
	baseList   []string
	baseFor    string
	reqIdx     map[string]int

	// TaskStatus is what Claude Code last said about each background task
	// (completed, killed, …), keyed by task id: a subagent's agent id.
	TaskStatus map[string]string
	// First and Last are the times of the first and latest activity.
	First, Last time.Time
}

func New() *Session {
	return &Session{byID: map[string]*Step{}, cache: map[*Turn]cached{}, Tools: map[string]*ToolStat{},
		reqIdx: map[string]int{}, TaskStatus: map[string]string{}}
}

// Live is the turn in progress, if any.
func (s *Session) Live() *Turn {
	if n := len(s.Turns); n > 0 && s.Turns[n-1].Live {
		return s.Turns[n-1]
	}
	return nil
}

// Pending lists the approvals waiting, oldest first.
func (s *Session) Pending() []*Step {
	var out []*Step
	for _, t := range s.Turns {
		for _, st := range t.steps {
			if st.Approval != nil {
				out = append(out, st)
			}
		}
	}
	sortSteps(out)
	return out
}

func (s *Session) turnFor(now time.Time) *Turn {
	if t := s.Live(); t != nil {
		return t
	}
	// Output with no prompt to hold it: an agent waking for background
	// work, or a replay that starts mid-turn.
	t := &Turn{N: len(s.Turns) + 1, Live: true, Start: now, steps: map[string]*Step{}}
	s.Turns = append(s.Turns, t)
	return t
}

// Apply folds one decoded host line (host.Decode's result) into the session.
func (s *Session) Apply(ev any, now time.Time) {
	if !now.IsZero() {
		if s.First.IsZero() || now.Before(s.First) {
			s.First = now
		}
		if now.After(s.Last) {
			s.Last = now
		}
	}
	switch ev := ev.(type) {
	case host.Sent:
		if t := s.Live(); t != nil {
			txt := ev.Text
			for _, im := range ev.Images {
				txt += "  ▣ " + im
			}
			t.Items = append(t.Items, &Item{Kind: KInterject, Text: txt})
			t.touch()
			return
		}
		s.streaming = nil
		s.Turns = append(s.Turns, &Turn{N: len(s.Turns) + 1, Prompt: ev.Text, Start: now, Live: true, steps: map[string]*Step{}, Effort: s.Info.Effort, Images: ev.Images})
	case host.InfoEvent:
		s.Info = ev.Info
		// The host went idle with a turn still open: Claude died mid-turn.
		if t := s.Live(); t != nil && ev.Info.State == "idle" && ev.Info.ClaudePID == 0 {
			s.endTurn(t, now)
			t.Err = "claude exited mid-turn"
			if ev.Info.Error != "" {
				t.Err += ": " + firstLine(ev.Info.Error)
			}
		}
	case host.Commands:
		s.Commands = ev.Commands
	case headless.Init:
		s.Model, s.Cwd = ev.Model, ev.Cwd
	case headless.RateLimit:
		s.Limit = ev.Status
	case headless.BlockStart:
		t := s.turnFor(now)
		t.Thinking = time.Time{}
		if ev.Type == "thinking" || ev.Type == "redacted_thinking" {
			t.Thinking = now
			if n := len(t.Items); n == 0 || t.Items[n-1].Kind != KThinking {
				t.Items = append(t.Items, &Item{Kind: KThinking})
			}
		}
		if ev.Type != "text" {
			s.streaming = nil
		}
		t.touch()
	case headless.Compact:
		// A divider in the turn it happened in (or the last one), and the
		// context starts again from what the summary left.
		t := s.Live()
		if t == nil && len(s.Turns) > 0 {
			t = s.Turns[len(s.Turns)-1]
		}
		if t == nil {
			t = s.turnFor(now)
		}
		c := ev
		t.Items = append(t.Items, &Item{Kind: KCompact, Compact: &c})
		if ev.PostTokens > 0 {
			s.Context = ev.PostTokens
		}
		t.touch()
	case headless.Delta:
		t := s.turnFor(now)
		t.Streamed += len(ev.Text)
		if ev.Input {
			t.touch()
			return
		}
		if ev.Thinking {
			if n := len(t.Items); n == 0 || t.Items[n-1].Kind != KThinking {
				t.Items = append(t.Items, &Item{Kind: KThinking})
			}
			t.Items[len(t.Items)-1].Text += ev.Text
		} else {
			if s.streaming == nil {
				s.streaming = &Item{Kind: KText}
				t.Items = append(t.Items, s.streaming)
			}
			s.streaming.Text += ev.Text
		}
		t.touch()
	case headless.Message:
		s.message(ev, now)
	case headless.PermissionRequest:
		if st := s.byID[ev.ToolUseID]; st != nil {
			req := ev
			st.Approval, st.Status = &req, Waiting
			s.touchStep(st)
		}
	case host.Answered:
		s.settle(ev.ID)
	case headless.PermissionCancelled:
		s.settle(ev.ID)
	case headless.PermissionDenied:
		if st := s.byID[ev.ToolUseID]; st != nil {
			st.Status, st.Output, st.End = Denied, ev.Reason, now
			s.touchStep(st)
		}
	case headless.Result:
		if t := s.Live(); t != nil {
			s.endTurn(t, now)
			t.Cost = ev.CostUSD
			if ev.IsError || (ev.Subtype != "" && ev.Subtype != "success") {
				t.Err = strings.ReplaceAll(strings.TrimPrefix(ev.Subtype, "error_"), "_", " ")
				if t.Err == "" {
					t.Err = firstLine(ev.Text)
				}
			}
		}
	}
}

func (s *Session) endTurn(t *Turn, now time.Time) {
	s.stepVer++
	t.Live, t.End = false, now
	s.streaming = nil
	for _, st := range t.steps {
		if st.Status == Running || st.Status == Waiting {
			st.Status, st.Approval = Lost, nil
		}
	}
	// The last words become the answer.
	for i := len(t.Items) - 1; i >= 0; i-- {
		it := t.Items[i]
		if it.Kind == KText && strings.TrimSpace(it.Text) != "" {
			it.Answer = true
			break
		}
		if it.Kind == KStep {
			break
		}
	}
	t.touch()
}

func (s *Session) settle(requestID string) {
	for _, st := range s.byID {
		if st.Approval != nil && st.Approval.ID == requestID {
			st.Approval = nil
			if st.Status == Waiting {
				st.Status = Running
			}
			s.touchStep(st)
		}
	}
}

func (s *Session) touchStep(st *Step) {
	s.stepVer++
	if st.turn != nil && st.turn.steps[st.ID] == st {
		st.turn.touch()
	}
}

func (s *Session) message(m headless.Message, now time.Time) {
	if m.Role != "assistant" {
		// Tool results belong to the turn their call is in, however late
		// they arrive; they never open a turn of their own.
		s.results(m, now)
		return
	}
	t := s.turnFor(now)
	defer t.touch()
	{
		parent := s.byID[m.ParentToolUseID]
		// A subagent's message whose run we never saw start still isn't
		// the main agent's: it mustn't land in the turn or its numbers.
		sub := m.ParentToolUseID != ""
		if m.Usage != nil {
			s.request(m, parent, now)
			if !sub {
				u := m.Usage
				s.Context = u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens + u.OutputTokens
				if m.Model != "" {
					t.Model = m.Model
				}
			}
		}
		for _, b := range m.Blocks {
			switch b.Type {
			case "text":
				if sub || strings.TrimSpace(b.Text) == "" {
					continue // a subagent's words stay inside it
				}
				if s.streaming != nil {
					s.streaming.Text = b.Text
					s.streaming = nil
				} else {
					t.Items = append(t.Items, &Item{Kind: KText, Text: b.Text})
				}
			case "thinking":
				if !sub && (len(t.Items) == 0 || t.Items[len(t.Items)-1].Kind != KThinking) {
					t.Items = append(t.Items, &Item{Kind: KThinking, Text: b.Text})
				}
			case "tool_use":
				s.streaming = nil
				st := &Step{ID: b.ID, Tool: b.Name, Input: b.Input, Start: now, Exit: -1, parent: parent, turn: t}
				s.byID[b.ID] = st
				t.steps[b.ID] = st
				s.stepVer++
				s.tool(b.Name).Calls++
				switch {
				case parent != nil:
					parent.Children = append(parent.Children, st)
				case !sub:
					t.Items = append(t.Items, &Item{Kind: KStep, Step: st})
				}
				s.tasksFromInput(st)
			}
		}
	}
}

// interrupted ends the running turn (or marks the last one) as stopped by
// you.
func (s *Session) interrupted(now time.Time) {
	t := s.Live()
	if t != nil {
		s.endTurn(t, now)
	} else if n := len(s.Turns); n > 0 {
		t = s.Turns[n-1]
	}
	if t != nil {
		t.Stopped, t.Err = true, ""
		t.touch()
	}
}

func (s *Session) results(m headless.Message, now time.Time) {
	for _, b := range m.Blocks {
		if b.Type == "text" && strings.HasPrefix(strings.TrimSpace(b.Text), "[Request interrupted by user") {
			s.interrupted(now)
			return
		}
	}
	// Text Claude Code injects as a user message (a background task
	// finishing, another session's message) starts a turn of its own.
	for _, b := range m.Blocks {
		if b.Type == "text" && strings.HasPrefix(strings.TrimSpace(b.Text), "<") {
			if from, text, ok := Injected(b.Text); ok && s.Live() == nil {
				raw, _ := json.Marshal(b.Text)
				s.noteTask(raw)
				s.Apply(host.Sent{Text: text}, now)
				s.Turns[len(s.Turns)-1].From = from
			}
		}
	}
	for _, b := range m.Blocks {
		if b.Type != "tool_result" {
			continue
		}
		st := s.byID[b.ToolUseID]
		if st == nil {
			continue
		}
		st.Output, st.Result, st.End = b.Text, m.ToolResult, now
		st.Approval = nil
		switch {
		case st.Status == Denied:
		case b.IsError:
			st.Status = Failed
			if isRejection(b.Text) {
				st.Status = Denied
			}
		default:
			st.Status = OK
		}
		if st.Tool == "Bash" {
			st.Exit = exitCode(st)
		}
		s.touchStep(st)
		ts := s.tool(st.Tool)
		if st.Status == Failed {
			ts.Failed++
		}
		if !st.Start.IsZero() {
			ts.Time += st.End.Sub(st.Start)
		}
		s.tasksFromResult(st)
	}
}

var exitRe = regexp.MustCompile(`(?m)^(?:Error: )?Exit code (\d+)`)

func exitCode(st *Step) int {
	if st.Status == OK {
		return 0 // "Exit code N" in a success's output is just output
	}
	if m := exitRe.FindStringSubmatch(st.Output); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	if st.Status == OK {
		return 0
	}
	return -1
}

// isRejection is a tool call you (or a rule) refused, as against one that
// failed on its own, like a file it had no permission to read.
func isRejection(text string) bool {
	t := strings.ToLower(text)
	for _, k := range []string{"user declined", "user rejected", "doesn't want to proceed", "tool use was rejected",
		"requires approval", "permission to use", "haven't granted", "has been denied"} {
		if strings.Contains(t, k) {
			return true
		}
	}
	return false
}

// request records a model call. Claude Code sends one message per content
// block, all with the same id and usage, so a repeat replaces the last.
func (s *Session) request(m headless.Message, parent *Step, now time.Time) {
	agent := ""
	if parent != nil {
		agent = readInput(parent.Input).str("subagent_type")
		if agent == "" {
			agent = "subagent"
		}
	}
	if m.Model == "<synthetic>" {
		return // Claude Code's own placeholder, not a model call
	}
	r := Request{ID: m.ID, At: now, Model: m.Model, Agent: agent, Run: m.ParentToolUseID, Usage: *m.Usage}
	// One call arrives as several messages with the same id and usage; the
	// output count can grow between them, so keep the largest.
	if i, ok := s.reqIdx[m.ID]; ok && m.ID != "" {
		prev := s.Requests[i]
		r.At = prev.At
		r.Usage.OutputTokens = max(r.Usage.OutputTokens, prev.Usage.OutputTokens)
		s.Requests[i] = r
		return
	}
	if m.ID != "" {
		s.reqIdx[m.ID] = len(s.Requests)
	}
	s.Requests = append(s.Requests, r)
}

func (s *Session) tool(name string) *ToolStat {
	ts := s.Tools[name]
	if ts == nil {
		ts = &ToolStat{Name: name}
		s.Tools[name] = ts
	}
	return ts
}

// Tasks come from TodoWrite (the whole list each time) and from TaskCreate
// and TaskUpdate (one at a time; TaskCreate's id is only in its result).
func (s *Session) tasksFromInput(st *Step) {
	switch st.Tool {
	case "TodoWrite":
		var in struct {
			Todos []struct {
				Content    string `json:"content"`
				ActiveForm string `json:"activeForm"`
				Status     string `json:"status"`
			} `json:"todos"`
		}
		if json.Unmarshal(st.Input, &in) != nil {
			return
		}
		s.Tasks = s.Tasks[:0]
		for i, td := range in.Todos {
			s.Tasks = append(s.Tasks, Task{ID: strconv.Itoa(i + 1), Subject: td.Content, Active: td.ActiveForm, Status: td.Status})
		}
	case "TaskUpdate":
		var in struct {
			ID         string `json:"taskId"`
			Status     string `json:"status"`
			Subject    string `json:"subject"`
			ActiveForm string `json:"activeForm"`
		}
		if json.Unmarshal(st.Input, &in) != nil {
			return
		}
		for i := range s.Tasks {
			if s.Tasks[i].ID != in.ID {
				continue
			}
			if in.Status == "deleted" {
				s.Tasks = append(s.Tasks[:i], s.Tasks[i+1:]...)
				return
			}
			if in.Status != "" {
				s.Tasks[i].Status = in.Status
			}
			if in.Subject != "" {
				s.Tasks[i].Subject = in.Subject
			}
			if in.ActiveForm != "" {
				s.Tasks[i].Active = in.ActiveForm
			}
			return
		}
	}
}

var taskIDRe = regexp.MustCompile(`Task #(\w+) created`)

func (s *Session) tasksFromResult(st *Step) {
	if st.Tool != "TaskCreate" || st.Status != OK {
		return
	}
	var in struct {
		Subject    string `json:"subject"`
		ActiveForm string `json:"activeForm"`
	}
	_ = json.Unmarshal(st.Input, &in)
	id := strconv.Itoa(len(s.Tasks) + 1)
	if m := taskIDRe.FindStringSubmatch(st.Output); m != nil {
		id = m[1]
	}
	s.Tasks = append(s.Tasks, Task{ID: id, Subject: in.Subject, Active: in.ActiveForm, Status: "pending"})
}

// Current is the task in progress and how far through the list the agent is.
func (s *Session) Current() (now *Task, done, total int) {
	for i := range s.Tasks {
		t := &s.Tasks[i]
		switch t.Status {
		case "completed":
			done++
		case "in_progress":
			if now == nil {
				now = t
			}
		}
	}
	return now, done, len(s.Tasks)
}

// bases are the folders paths are shown relative to, symlinks resolved.
func (s *Session) bases() []string {
	if s.baseList != nil && s.baseFor == s.Info.Cwd+"|"+s.Cwd {
		return s.baseList
	}
	var out []string
	for _, b := range []string{s.Info.Cwd, s.Cwd} {
		if b == "" {
			continue
		}
		out = append(out, b)
		if r, err := filepath.EvalSymlinks(b); err == nil && r != b {
			out = append(out, r)
		}
	}
	s.baseList, s.baseFor = out, s.Info.Cwd+"|"+s.Cwd
	return out
}

func sortSteps(xs []*Step) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j].Start.Before(xs[j-1].Start); j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

var mdMarks = strings.NewReplacer("**", "", "__", "", "`", "")

func stripMarkdown(s string) string {
	s = mdMarks.Replace(s)
	return strings.TrimLeft(s, "#> -*")
}

// firstPlain is firstLine(stripMarkdown(s)), reading only as far as the
// line it returns: the marks never span lines, so each line strips alone.
func firstPlain(s string) string {
	for first := true; s != ""; first = false {
		line, rest, _ := strings.Cut(s, "\n")
		l := mdMarks.Replace(line)
		if first {
			l = strings.TrimLeft(l, "#> -*")
		}
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
		s = rest
	}
	return ""
}
