// Package efficiency is what the Efficiency place knows: where tokens go,
// hour by hour, what each session carried in its context, the token savers
// that are installed and when they were used, and what that changed.
package efficiency

import (
	"bufio"
	"io"
	"os"
	"strings"
	"time"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/agent/tool"
	"github.com/0xdeafcafe/agtop/internal/agent/usage"
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

// toolClass is the class a call of this kind is counted in; name is the
// agent's own name for the tool, for an MCP tool's.
func toolClass(k tool.Kind, name string) uint8 {
	switch k {
	case tool.Shell:
		return ToolBash
	case tool.Read:
		return ToolRead
	case tool.Search, tool.Glob:
		return ToolSearch
	case tool.Edit, tool.Write, tool.Notebook:
		return ToolEdit
	case tool.MCP:
		return ToolMCP
	}
	if strings.HasPrefix(name, "mcp__") {
		return ToolMCP
	}
	return ToolOther
}

// Bucket is one hour of use: requests, tokens by kind, dollars by kind and
// the tool output that came back.
type Bucket struct {
	Req   int64 `json:"r,omitzero"`
	In    int64 `json:"i,omitzero"`
	Out   int64 `json:"o,omitzero"`
	CR    int64 `json:"cr,omitzero"`
	CW    int64 `json:"cw,omitzero"`
	Think int64 `json:"t,omitzero"`
	// Cost by what it paid for: input, output, cache read, cache write.
	CIn  float64 `json:"ci,omitzero"`
	COut float64 `json:"co,omitzero"`
	CCR  float64 `json:"ccr,omitzero"`
	CCW  float64 `json:"ccw,omitzero"`

	Calls [NTools]int32 `json:"k"`
	Bytes [NTools]int64 `json:"b"`
	// Look is the part of Bash's output that came from searching and
	// reading code (grep, rg, find, cat, sed -n…): what code-graph and
	// retrieval savers stand in for.
	LookCalls int32 `json:"lk,omitzero"`
	Look      int64 `json:"lb,omitzero"`
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
	b.LookCalls += o.LookCalls
	b.Look += o.Look
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
	Auto bool      `json:"u,omitzero"`
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
	Sub     bool   `json:"sub,omitzero"`  // a subagent's transcript
	Model   string `json:"model,omitempty"`

	First time.Time `json:"first"`
	Last  time.Time `json:"last"`

	Hours map[int64]*Bucket `json:"h,omitempty"` // unix hour → use

	StartCtx int64     `json:"sc,omitzero"` // the first request's context: what a session starts with
	PeakCtx  int64     `json:"pc,omitzero"`
	Compacts []Compact `json:"cp,omitempty"`
	Big      int       `json:"big,omitzero"` // tool results over BigResult bytes

	Uses  map[string]*Use `json:"u,omitempty"`
	Reads map[string]int  `json:"rd,omitempty"` // file → times read

	// The newest assistant message; its usage can still change while more
	// of its blocks are appended, so it's added when the next one starts.
	PendID    string           `json:"pi,omitempty"`
	PendModel string           `json:"pm,omitempty"`
	PendUse   usage.TokenUsage `json:"pu"`
	PendThink int64            `json:"pt,omitzero"`
	PendFast  bool             `json:"pf,omitzero"`
	PendAt    time.Time        `json:"pa"`
	// Tool calls whose results haven't come back yet: id → class, with
	// lookFlag for a shell command that searches or reads code.
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
		CIn:   cost(model, usage.TokenUsage{Input: u.Input}, fast),
		COut:  cost(model, usage.TokenUsage{Output: u.Output}, fast),
		CCR:   cost(model, usage.TokenUsage{CacheRead: u.CacheRead}, fast),
		CCW:   cost(model, usage.TokenUsage{CacheWrite5m: u.CacheWrite5m, CacheWrite1h: u.CacheWrite1h}, fast),
	}
}

// cost is what Agent's tokens u cost on model, in its fast mode or not; 0
// for a model it doesn't price.
func cost(model string, u usage.TokenUsage, fast bool) float64 {
	if fast {
		if fp, ok := agent.As[agent.FastPricer](Agent); ok {
			c, _ := fp.CostFast(model, u)
			return c
		}
	}
	if pr, ok := agent.As[agent.Pricer](Agent); ok {
		c, _ := pr.Cost(model, u)
		return c
	}
	return 0
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
	f.PendID, f.PendModel, f.PendUse, f.PendThink, f.PendFast = "", "", usage.TokenUsage{}, 0, false
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

// Scan reads what was appended to path since f.Offset, each whole line
// through src. A half-written line is read next time.
func Scan(src Source, path string, f *File, buf []byte) ([]byte, error) {
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
		src.ReadLine(f, buf)
	}
}

// What a Source records, line by line.

// Saw is a line written at, by session, in cwd: it keeps the file's span,
// and its session and folder from the first line that says them.
func (f *File) Saw(at time.Time, session, cwd string) {
	if f.First.IsZero() || at.Before(f.First) {
		f.First = at
	}
	if at.After(f.Last) {
		f.Last = at
	}
	if f.Session == "" {
		f.Session = session
	}
	if f.Project == "" {
		f.Project = cwd
	}
}

// Request is one model request's use, by its id: a message's usage can
// still change while more of it is written, so it's added once the next
// one starts.
func (f *File) Request(id, model string, u usage.TokenUsage, think int64, fast bool, at time.Time) {
	if id != f.PendID {
		f.commit()
	}
	f.PendID, f.PendModel, f.PendUse, f.PendAt = id, model, u, at
	f.PendFast, f.PendThink = fast, think
}

// Call is a tool call of kind k, name being the agent's own name for the
// tool; its result is counted when Result comes with the same id.
func (f *File) Call(id string, k tool.Kind, name string, in *tool.Input, at time.Time) {
	c := toolClass(k, name)
	if f.Open == nil {
		f.Open = map[string]uint8{}
	}
	switch k {
	case tool.Shell:
		if Looks(in.Command) {
			c |= lookFlag
		}
		if w, admin := firstWord(in.Command); w != "" && admin {
			f.use("bash:"+w+"/admin", at)
		} else if w != "" {
			f.use("bash:"+w, at)
		}
	case tool.Read:
		if in.Path != "" {
			if f.Reads == nil {
				f.Reads = map[string]int{}
			}
			if _, ok := f.Reads[in.Path]; ok || len(f.Reads) < maxReads {
				f.Reads[in.Path]++
			}
		}
	}
	if len(f.Open) < maxOpen {
		f.Open[id] = c
	}
}

// Result is what the call id gave back: n bytes of text.
func (f *File) Result(id string, n int64, at time.Time) {
	cl, ok := f.Open[id]
	if !ok {
		cl = ToolOther
	}
	delete(f.Open, id)
	b := f.hour(at)
	if cl&lookFlag != 0 {
		cl &^= lookFlag
		b.LookCalls++
		b.Look += n
	}
	b.Calls[cl]++
	b.Bytes[cl] += n
	if n > BigResult {
		f.Big++
	}
}

// Skill, MCP, Command and Hook are uses of a skill, an MCP server, a slash
// command and a hook.
func (f *File) Skill(name string, at time.Time)   { f.use("skill:"+name, at) }
func (f *File) MCP(server string, at time.Time)   { f.use("mcp:"+server, at) }
func (f *File) Command(name string, at time.Time) { f.use("cmd:"+name, at) }
func (f *File) Hook(cmd string, at time.Time)     { f.use("hook:"+hookKey(cmd), at) }

// Compacted is one compaction of the conversation.
func (f *File) Compacted(c Compact) { f.Compacts = append(f.Compacts, c) }

// hookKey is a hook's command, short enough to keep: agents' hooks are
// told apart by their program, not their arguments' details.
func hookKey(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if len(cmd) > 120 {
		cmd = cmd[:120]
	}
	return cmd
}

const lookFlag = 0x80

// lookPrograms search or read code: what a code graph answers instead.
var lookPrograms = map[string]bool{
	"grep": true, "rg": true, "ag": true, "ack": true, "egrep": true, "fgrep": true,
	"find": true, "fd": true, "ls": true, "tree": true, "eza": true,
	"cat": true, "bat": true, "head": true, "tail": true, "less": true, "nl": true, "wc": true,
	"sg": true, "ast-grep": true,
}

// Looks is whether a shell command searches or reads code, going by the
// program of its last step that isn't a cd, echo or true, past rtk and
// VAR=x, and not redirected to a file. sed and awk count with -n, git with grep, ls-files or show.
func Looks(cmd string) bool {
	steps := strings.FieldsFunc(strings.NewReplacer("&&", ";", "||", ";", "\n", ";").Replace(cmd), func(r rune) bool { return r == ';' })
	for i := len(steps) - 1; i >= 0; i-- {
		step, _, _ := strings.Cut(steps[i], "|")
		fields := strings.Fields(step)
		for len(fields) > 0 && (strings.Contains(fields[0], "=") || fields[0] == "rtk" || fields[0] == "command" || fields[0] == "env") {
			fields = fields[1:]
		}
		if len(fields) == 0 {
			continue
		}
		w := strings.Trim(fields[0], "'\"(")
		if j := strings.LastIndexByte(w, '/'); j >= 0 {
			w = w[j+1:]
		}
		arg := ""
		if len(fields) > 1 {
			arg = fields[1]
		}
		for _, f := range fields[1:] {
			if strings.HasPrefix(f, ">") || strings.HasPrefix(f, "1>") {
				w = "" // written to a file, not read back
			}
		}
		switch w {
		case "cd", "echo", "true", ":", "printf", "pushd", "popd", "set", "export":
			continue
		case "sed", "awk":
			return arg == "-n"
		case "git":
			return arg == "grep" || arg == "ls-files" || arg == "show"
		}
		return lookPrograms[w]
	}
	return false
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
