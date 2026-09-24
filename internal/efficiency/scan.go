// Package efficiency is what the Efficiency place knows: where tokens go,
// hour by hour, what each session carried in its context, the token savers
// that are installed and when they were used, and what that changed.
package efficiency

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"time"

	"github.com/0xdeafcafe/agtop/internal/claude"
)

// Tool classes: what tool results are counted by. Bash and Read are most of
// what's carried in context; the rest are grouped.
const (
	ToolBash = iota
	ToolRead
	ToolSearch // Grep, Glob
	ToolEdit   // Edit, Write, NotebookEdit
	ToolMCP
	ToolOther
	NTools
)

var ToolNames = [NTools]string{"Bash", "Read", "Grep/Glob", "Edit/Write", "MCP", "other"}

func toolClass(name string) uint8 {
	switch {
	case name == "Bash":
		return ToolBash
	case name == "Read":
		return ToolRead
	case name == "Grep" || name == "Glob":
		return ToolSearch
	case name == "Edit" || name == "Write" || name == "MultiEdit" || name == "NotebookEdit":
		return ToolEdit
	case strings.HasPrefix(name, "mcp__"):
		return ToolMCP
	}
	return ToolOther
}

// Bucket is one hour of use: requests, tokens by kind, dollars by kind and
// the tool output that came back.
type Bucket struct {
	Req   int64 `json:"r,omitempty"`
	In    int64 `json:"i,omitempty"`
	Out   int64 `json:"o,omitempty"`
	CR    int64 `json:"cr,omitempty"`
	CW    int64 `json:"cw,omitempty"`
	Think int64 `json:"t,omitempty"`
	// Cost by what it paid for: input, output, cache read, cache write.
	CIn  float64 `json:"ci,omitempty"`
	COut float64 `json:"co,omitempty"`
	CCR  float64 `json:"ccr,omitempty"`
	CCW  float64 `json:"ccw,omitempty"`

	Calls [NTools]int32 `json:"k"`
	Bytes [NTools]int64 `json:"b"`
}

func (b *Bucket) Cost() float64 { return b.CIn + b.COut + b.CCR + b.CCW }

// Context is the input side of the requests: what was sent each time.
func (b *Bucket) Context() int64 { return b.In + b.CR + b.CW }

func (b *Bucket) Add(o *Bucket) {
	b.Req += o.Req
	b.In += o.In
	b.Out += o.Out
	b.CR += o.CR
	b.CW += o.CW
	b.Think += o.Think
	b.CIn += o.CIn
	b.COut += o.COut
	b.CCR += o.CCR
	b.CCW += o.CCW
	for i := range b.Calls {
		b.Calls[i] += o.Calls[i]
		b.Bytes[i] += o.Bytes[i]
	}
}

// Use is how often something was seen in a transcript, and when first and
// last: a hook's command, a skill, a slash command, an MCP server, the
// first word of a shell command.
type Use struct {
	N     int       `json:"n"`
	First time.Time `json:"f"`
	Last  time.Time `json:"l"`
}

func (u *Use) see(at time.Time) {
	u.N++
	if u.First.IsZero() || at.Before(u.First) {
		u.First = at
	}
	if at.After(u.Last) {
		u.Last = at
	}
}

// Compact is one compaction of the conversation.
type Compact struct {
	At   time.Time `json:"a"`
	Auto bool      `json:"u,omitempty"`
	Pre  int64     `json:"p"`
	Post int64     `json:"q"`
}

// File is one transcript as far as it has been read. It is serialisable so
// the next run carries on from Offset.
type File struct {
	Offset  int64  `json:"off"`
	Size    int64  `json:"size"`
	Account string `json:"acct"`
	Session string `json:"sid,omitempty"`
	Project string `json:"cwd,omitempty"` // the folder it first worked in
	Sub     bool   `json:"sub,omitempty"` // a subagent's transcript
	Model   string `json:"model,omitempty"`

	First time.Time `json:"first"`
	Last  time.Time `json:"last"`

	Hours map[int64]*Bucket `json:"h,omitempty"` // unix hour → use

	StartCtx int64     `json:"sc,omitempty"` // the first request's context: what a session starts with
	PeakCtx  int64     `json:"pc,omitempty"`
	Compacts []Compact `json:"cp,omitempty"`
	Big      int       `json:"big,omitempty"` // tool results over BigResult bytes

	Uses  map[string]*Use `json:"u,omitempty"`
	Reads map[string]int  `json:"rd,omitempty"` // file → times read

	// The newest assistant message; its usage can still change while more
	// of its blocks are appended, so it's added when the next one starts.
	PendID    string            `json:"pi,omitempty"`
	PendModel string            `json:"pm,omitempty"`
	PendUse   claude.TokenUsage `json:"pu"`
	PendThink int64             `json:"pt,omitempty"`
	PendFast  bool              `json:"pf,omitempty"`
	PendAt    time.Time         `json:"pa"`
	// Tool calls whose results haven't come back yet: id → class.
	Open map[string]uint8 `json:"op,omitempty"`
}

// BigResult is a tool result big enough to be worth a finding: about 10k
// tokens.
const BigResult = 40_000

const (
	maxUses  = 96
	maxReads = 300
	maxOpen  = 256
)

// Totals is the file's use summed over its hours.
func (f *File) Totals() Bucket {
	var t Bucket
	f.EachHour(func(_ int64, b *Bucket) { t.Add(b) })
	return t
}

func (f *File) clone() *File {
	c := *f
	c.Hours = make(map[int64]*Bucket, len(f.Hours))
	for k, v := range f.Hours {
		b := *v
		c.Hours[k] = &b
	}
	c.Compacts = append([]Compact(nil), f.Compacts...)
	c.Uses = make(map[string]*Use, len(f.Uses))
	for k, v := range f.Uses {
		u := *v
		c.Uses[k] = &u
	}
	c.Reads = make(map[string]int, len(f.Reads))
	for k, v := range f.Reads {
		c.Reads[k] = v
	}
	c.Open = make(map[string]uint8, len(f.Open))
	for k, v := range f.Open {
		c.Open[k] = v
	}
	return &c
}

func hourOf(t time.Time) int64 { return t.Unix() / 3600 }

func (f *File) hour(at time.Time) *Bucket {
	if f.Hours == nil {
		f.Hours = map[int64]*Bucket{}
	}
	h := hourOf(at)
	b := f.Hours[h]
	if b == nil {
		b = &Bucket{}
		f.Hours[h] = b
	}
	return b
}

func (f *File) use(key string, at time.Time) {
	if f.Uses == nil {
		f.Uses = map[string]*Use{}
	}
	u := f.Uses[key]
	if u == nil {
		if len(f.Uses) >= maxUses {
			return
		}
		u = &Use{}
		f.Uses[key] = u
	}
	u.see(at)
}

// pendBucket is the pending message as one request's use.
func (f *File) pendBucket() Bucket {
	u, model, fast := f.PendUse, f.PendModel, f.PendFast
	return Bucket{
		Req:   1,
		In:    u.Input,
		Out:   u.Output,
		CR:    u.CacheRead,
		CW:    u.CacheWrite5m + u.CacheWrite1h,
		Think: f.PendThink,
		CIn:   claude.Cost(model, claude.TokenUsage{Input: u.Input}, fast),
		COut:  claude.Cost(model, claude.TokenUsage{Output: u.Output}, fast),
		CCR:   claude.Cost(model, claude.TokenUsage{CacheRead: u.CacheRead}, fast),
		CCW:   claude.Cost(model, claude.TokenUsage{CacheWrite5m: u.CacheWrite5m, CacheWrite1h: u.CacheWrite1h}, fast),
	}
}

// commit adds the pending message: a request, its tokens and what it cost.
func (f *File) commit() {
	if f.PendID == "" {
		return
	}
	pb := f.pendBucket()
	f.hour(f.PendAt).Add(&pb)
	ctx := pb.Context()
	if f.StartCtx == 0 {
		f.StartCtx = ctx
	}
	f.PeakCtx = max(f.PeakCtx, ctx)
	f.Model = f.PendModel
	f.PendID, f.PendModel, f.PendUse, f.PendThink, f.PendFast = "", "", claude.TokenUsage{}, 0, false
}

// Start is the context the session started with, and Peak the most it
// carried, the message still being written included.
func (f *File) Start() int64 {
	if f.StartCtx == 0 && f.PendID != "" {
		return f.PendUse.Input + f.PendUse.CacheRead + f.PendUse.CacheWrite5m + f.PendUse.CacheWrite1h
	}
	return f.StartCtx
}

func (f *File) Peak() int64 {
	p := f.PeakCtx
	if f.PendID != "" {
		p = max(p, f.PendUse.Input+f.PendUse.CacheRead+f.PendUse.CacheWrite5m+f.PendUse.CacheWrite1h)
	}
	return p
}

// EachHour calls fn with every hour's use, the message still being written
// included, so a running session's figures move.
func (f *File) EachHour(fn func(hour int64, b *Bucket)) {
	var pend int64 = -1
	var pb Bucket
	if f.PendID != "" {
		pend, pb = hourOf(f.PendAt), f.pendBucket()
	}
	for h, b := range f.Hours {
		if h == pend {
			sum := *b
			sum.Add(&pb)
			fn(h, &sum)
			pend = -1
			continue
		}
		fn(h, b)
	}
	if pend >= 0 {
		fn(pend, &pb)
	}
}

type rawLine struct {
	Type      string    `json:"type"`
	Subtype   string    `json:"subtype"`
	Timestamp time.Time `json:"timestamp"`
	Cwd       string    `json:"cwd"`
	SessionID string    `json:"sessionId"`
	Message   struct {
		ID      string          `json:"id"`
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"`
		Usage   *struct {
			Input       int64  `json:"input_tokens"`
			Output      int64  `json:"output_tokens"`
			CacheRead   int64  `json:"cache_read_input_tokens"`
			CacheCreate int64  `json:"cache_creation_input_tokens"`
			Speed       string `json:"speed"`
			Details     *struct {
				Thinking int64 `json:"thinking_tokens"`
			} `json:"output_tokens_details"`
			CacheBreakup *struct {
				M5 int64 `json:"ephemeral_5m_input_tokens"`
				H1 int64 `json:"ephemeral_1h_input_tokens"`
			} `json:"cache_creation"`
		} `json:"usage"`
	} `json:"message"`
	Attachment *struct {
		Type      string `json:"type"`
		HookEvent string `json:"hookEvent"`
		Command   string `json:"command"`
		Skills    []struct {
			Name string `json:"name"`
		} `json:"skills"`
	} `json:"attachment"`
	Compact *struct {
		Trigger string `json:"trigger"`
		Pre     int64  `json:"preTokens"`
		Post    int64  `json:"postTokens"`
	} `json:"compactMetadata"`
	Content json.RawMessage `json:"content"` // a system line's
}

type block struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
}

// The lines worth decoding carry one of these; the rest (most of the bytes:
// snapshots, progress, queue operations) are skipped unread.
var markers = [][]byte{
	[]byte(`"type":"assistant"`),
	[]byte(`"tool_result"`),
	[]byte(`"hook_success"`),
	[]byte(`"compact_boundary"`),
	[]byte(`"invoked_skills"`),
	[]byte(`<command-name>`),
}

func interesting(b []byte) bool {
	for _, m := range markers {
		if bytes.Contains(b, m) {
			return true
		}
	}
	return false
}

// Scan reads what was appended to path since f.Offset. Only whole lines
// are read, so a half-written one is read next time.
func Scan(path string, f *File, buf []byte) ([]byte, error) {
	fh, err := os.Open(path)
	if err != nil {
		return buf, err
	}
	defer fh.Close()
	st, err := fh.Stat()
	if err != nil {
		return buf, err
	}
	if st.Size() < f.Offset {
		*f = File{Account: f.Account, Sub: f.Sub} // rewritten: read it again
	}
	f.Size = st.Size()
	if st.Size() == f.Offset {
		return buf, nil
	}
	if _, err := fh.Seek(f.Offset, io.SeekStart); err != nil {
		return buf, err
	}
	r := bufio.NewReaderSize(fh, 256<<10)
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
		f.Offset += int64(len(buf))
		if interesting(buf) {
			f.consume(buf)
		}
	}
}

func (f *File) consume(b []byte) {
	var l rawLine
	if json.Unmarshal(b, &l) != nil {
		return
	}
	at := l.Timestamp
	if at.IsZero() {
		return
	}
	if f.First.IsZero() || at.Before(f.First) {
		f.First = at
	}
	if at.After(f.Last) {
		f.Last = at
	}
	if f.Session == "" {
		f.Session = l.SessionID
	}
	if f.Project == "" {
		f.Project = l.Cwd
	}
	switch l.Type {
	case "assistant":
		f.assistant(&l, at)
	case "user":
		f.user(&l, at)
	case "attachment":
		a := l.Attachment
		if a == nil {
			return
		}
		switch a.Type {
		case "hook_success":
			if a.Command != "" {
				f.use("hook:"+hookKey(a.Command), at)
			}
		case "invoked_skills":
			for _, s := range a.Skills {
				f.use("skill:"+s.Name, at)
			}
		}
	case "system":
		if l.Subtype == "compact_boundary" && l.Compact != nil {
			f.Compacts = append(f.Compacts, Compact{At: at, Auto: l.Compact.Trigger == "auto", Pre: l.Compact.Pre, Post: l.Compact.Post})
		}
		var s string
		if json.Unmarshal(l.Content, &s) == nil {
			f.slash(s, at)
		}
	}
}

// hookKey is a hook's command, short enough to keep: agents' hooks are
// told apart by their program, not their arguments' details.
func hookKey(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if len(cmd) > 120 {
		cmd = cmd[:120]
	}
	return cmd
}

func (f *File) slash(s string, at time.Time) {
	_, rest, ok := strings.Cut(s, "<command-name>")
	if !ok {
		return
	}
	name, _, ok := strings.Cut(rest, "</command-name>")
	if ok && name != "" && len(name) < 64 {
		f.use("cmd:"+name, at)
	}
}

func (f *File) assistant(l *rawLine, at time.Time) {
	m := &l.Message
	if u := m.Usage; u != nil && m.Model != "" && m.Model != "<synthetic>" {
		if m.ID != f.PendID {
			f.commit()
		}
		tu := claude.TokenUsage{Input: u.Input, Output: u.Output, CacheRead: u.CacheRead}
		if cb := u.CacheBreakup; cb != nil && cb.M5+cb.H1 > 0 {
			tu.CacheWrite5m, tu.CacheWrite1h = cb.M5, cb.H1
		} else {
			tu.CacheWrite5m = u.CacheCreate
		}
		f.PendID, f.PendModel, f.PendUse, f.PendAt = m.ID, m.Model, tu, at
		f.PendFast = u.Speed == "fast"
		f.PendThink = 0
		if u.Details != nil {
			f.PendThink = u.Details.Thinking
		}
	}
	if !bytes.Contains(m.Content, []byte(`"tool_use"`)) {
		return
	}
	var blocks []block
	if json.Unmarshal(m.Content, &blocks) != nil {
		return
	}
	for _, bl := range blocks {
		if bl.Type != "tool_use" {
			continue
		}
		c := toolClass(bl.Name)
		if f.Open == nil {
			f.Open = map[string]uint8{}
		}
		if len(f.Open) < maxOpen {
			f.Open[bl.ID] = c
		}
		switch {
		case bl.Name == "Bash":
			var in struct {
				Command string `json:"command"`
			}
			if json.Unmarshal(bl.Input, &in) == nil {
				if w, admin := firstWord(in.Command); w != "" && admin {
					f.use("bash:"+w+"/admin", at)
				} else if w != "" {
					f.use("bash:"+w, at)
				}
			}
		case bl.Name == "Read":
			var in struct {
				Path string `json:"file_path"`
			}
			if json.Unmarshal(bl.Input, &in) == nil && in.Path != "" {
				if f.Reads == nil {
					f.Reads = map[string]int{}
				}
				if _, ok := f.Reads[in.Path]; ok || len(f.Reads) < maxReads {
					f.Reads[in.Path]++
				}
			}
		case bl.Name == "Skill":
			var in struct {
				Skill string `json:"skill"`
			}
			if json.Unmarshal(bl.Input, &in) == nil && in.Skill != "" {
				f.use("skill:"+in.Skill, at)
			}
		case strings.HasPrefix(bl.Name, "mcp__"):
			server, _, _ := strings.Cut(strings.TrimPrefix(bl.Name, "mcp__"), "__")
			f.use("mcp:"+server, at)
		}
	}
}

// firstWord is the program a shell command runs, past any VAR=x, and
// whether it's being looked after rather than used: installed, set up,
// asked its version or its figures.
func firstWord(cmd string) (string, bool) {
	fields := strings.Fields(cmd)
	for i, w := range fields {
		if strings.Contains(w, "=") && !strings.HasPrefix(w, "=") {
			continue
		}
		w = strings.Trim(w, "'\"(")
		if j := strings.LastIndexByte(w, '/'); j >= 0 {
			w = w[j+1:]
		}
		if len(w) == 0 || len(w) > 32 {
			return "", false
		}
		admin := false
		if i+1 < len(fields) {
			admin = adminWords[fields[i+1]]
		}
		return w, admin
	}
	return "", false
}

var adminWords = map[string]bool{
	"init": true, "gain": true, "install": true, "uninstall": true, "setup": true, "help": true,
	"--help": true, "-h": true, "--version": true, "-V": true, "version": true, "discover": true,
	"session": true, "verify": true, "telemetry": true, "doctor": true, "config": true,
}

func (f *File) user(l *rawLine, at time.Time) {
	c := l.Message.Content
	if len(c) > 0 && c[0] == '"' {
		var s string
		if json.Unmarshal(c, &s) == nil {
			f.slash(s, at)
		}
		return
	}
	if !bytes.Contains(c, []byte(`"tool_result"`)) {
		return
	}
	var blocks []block
	if json.Unmarshal(c, &blocks) != nil {
		return
	}
	for _, bl := range blocks {
		if bl.Type != "tool_result" {
			continue
		}
		cl, ok := f.Open[bl.ToolUseID]
		if !ok {
			cl = ToolOther
		}
		delete(f.Open, bl.ToolUseID)
		n := resultSize(bl.Content)
		b := f.hour(at)
		b.Calls[cl]++
		b.Bytes[cl] += n
		if n > BigResult {
			f.Big++
		}
	}
}

// resultSize is how much text a tool result gave back; images count as
// nothing, being tokens of another kind.
func resultSize(raw json.RawMessage) int64 {
	if len(raw) == 0 {
		return 0
	}
	if raw[0] == '"' {
		return int64(len(raw) - 2)
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return int64(len(raw))
	}
	var n int64
	for _, p := range parts {
		n += int64(len(p.Text))
	}
	return n
}
