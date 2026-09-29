package ui

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/rush/internal/cellw"
	"github.com/0xdeafcafe/rush/internal/convo"
	"github.com/0xdeafcafe/rush/internal/host"
)

// What Claude Code runs besides talking: shells, monitors and workflows,
// in the background or with the turn waiting on them. The dock shows the
// ones running, the background view all of them; either lets you stop
// one, or send one the turn is waiting on into the background.

// jobWait is how long a command the turn waits on runs before the dock
// offers to background or stop it: quick ones come and go unannounced.
const jobWait = 3 * time.Second

// dockJobsShown is how many running tasks the dock shows at once.
const dockJobsShown = 4

// dockJobs are the tasks the dock shows: everything running but subagents
// (they have their own block), a foreground one once it's run a while.
func (c *hostConn) dockJobs() []*convo.Job {
	var out []*convo.Job
	for _, j := range c.sess.RunningJobs() {
		if c.sess.JobKind(j) == "subagent" || !j.Background && time.Since(j.Start) < jobWait {
			continue
		}
		out = append(out, j)
	}
	return out
}

// jobIcon is a task's mark and colour, by what it is.
func jobIcon(kind string) (string, string) {
	switch kind {
	case "monitor":
		return "◎", cBlue
	case "workflow":
		return "⧉", cQueue
	case "subagent":
		return "⇉", cBlue
	}
	return "▸", cSub
}

// jobLabel is what a task is doing in a line: the command for a shell,
// its description otherwise.
func jobLabel(c *hostConn, j *convo.Job) string {
	l := j.Label
	if cmd := c.sess.JobCommand(j); cmd != "" && c.sess.JobKind(j) == "shell" {
		l = cmd
	}
	if j.Agent != "" {
		l = j.Agent + "  " + l
	}
	return oneLine(tildify(l))
}

// jobState is how a task stands, in a word or two and its colour.
func jobState(j *convo.Job, now time.Time) string {
	took := ""
	end := j.End
	if j.Running() {
		end = now
	}
	if !j.Start.IsZero() {
		took = " · " + dur(end.Sub(j.Start).Round(time.Second))
	}
	switch {
	case j.Running() && j.Background:
		return dim("in background" + took)
	case j.Running():
		return paint(cOrange, "waiting on it") + dim(took)
	case j.Status == "completed":
		return paint(cGreen, "✓ done") + dim(took)
	case j.Status == "failed":
		return paint(cRed, "✗ failed") + dim(took)
	}
	return dim("⏹ "+j.Status) + dim(took)
}

// jobsPreview is the dock's block of running tasks: a heading, then a row
// each with its latest line of output under it.
func (m *Model) jobsPreview(c *hostConn, jobs []*convo.Job, w int) []string {
	picked := -1
	for i, j := range jobs {
		if c.sel == "job:"+j.ID {
			picked = i
		}
	}
	fg := 0
	for _, j := range jobs {
		if !j.Background {
			fg++
		}
	}
	head := fmt.Sprintf("%d running", len(jobs))
	if bg := len(jobs) - fg; bg > 0 && fg > 0 {
		head += fmt.Sprintf(" · %d in the background", bg)
	} else if fg == 0 {
		head += " in the background"
	}
	hint := ""
	switch {
	case picked >= 0 && !jobs[picked].Background:
		if rp, ok := c.sess.RunningPart(jobs[picked].ToolUseID); ok {
			hint = keys("k", "kill "+firstWord(rp.Command), "b", "background", "x", "stop")
		} else {
			hint = keys("b", "background", "x", "stop", "enter", "output")
		}
	case picked >= 0:
		hint = keys("x", "stop", "enter", "output")
	case m.paneFocus && fg > 0:
		hint = keys("ctrl+b", "background", "↑", "pick")
	case m.paneFocus && len(c.input) == 0 && len(m.queueOf(c).items) == 0:
		hint = keys("↑", "pick one to stop")
	}
	out := []string{spread(" "+paint(cSub, "▸ ")+paint(cText+bold, head), hint+"  ", w)}
	start := 0
	if picked >= dockJobsShown {
		start = picked - dockJobsShown + 1
	}
	if start > 0 {
		out = append(out, dim(fmt.Sprintf("  … %d before", start)))
	}
	now := time.Now()
	for i := start; i < min(len(jobs), start+dockJobsShown); i++ {
		j := jobs[i]
		kind := c.sess.JobKind(j)
		icon, col := jobIcon(kind)
		mark := paint(col, icon)
		if !j.Background {
			mark = paint(cOrange, spinner[(m.tick+i)%len(spinner)])
		}
		right := jobState(j, now) + "  "
		left := "  " + mark + " " + paint(cText+bold, fmt.Sprintf("%-7s", kind)) + " " +
			paint(cSub, ansi.Truncate(jobLabel(c, j), max(12, w-cellw.String(ansi.Strip(right))-14), "…"))
		rows := []string{spread(left, right, w)}
		if last := m.jobTail(c, j, 1); len(last) > 0 {
			rows = append(rows, ansi.Truncate("  "+paint(cFaint, "╰")+" "+paint(cOrange, "›")+" "+faint(last[0]), w-2, "…"))
		}
		if i == picked {
			for k := range rows {
				rows[k] = picked1(rows[k], w, m.paneFocus)
			}
		}
		out = append(out, rows...)
	}
	if rest := len(jobs) - start - dockJobsShown; rest > 0 {
		out = append(out, dim(fmt.Sprintf("  + %d more in the background view", rest)))
	}
	return out
}

// jobLines is the background view: every task this session has run but
// its subagents (the subagents view has those), what's running first, each
// with its output under it when opened.
func (m *Model) jobLines(c *hostConn, o convo.Options) []convo.Line {
	w := o.Width
	var run, done []*convo.Job
	for _, j := range c.sess.WorkJobs() {
		if j.Running() {
			run = append(run, j)
		} else {
			done = append(done, j)
		}
	}
	slices.Reverse(done) // the latest finished first
	head := dim(fmt.Sprintf("%d tasks", len(run)+len(done)))
	if len(run) > 0 {
		head = paint(cOrange, fmt.Sprintf("%d running", len(run))) + dim(" · ") + head
	}
	how := "enter shows output · x stops · b backgrounds"
	lines := []convo.Line{{Text: fit("  "+head+dim(" · "+how), w)}, {Text: ""}}
	if len(run)+len(done) == 0 {
		return append(lines, convo.Line{Text: dim("  nothing running · shells, monitors and workflows Claude starts show here")})
	}
	now := time.Now()
	emit := func(ref string, rows []string, pick bool) {
		for _, r := range rows {
			if pick && ref == o.Selected {
				r = picked1(r, w, o.Focused)
			} else {
				r = fit(r, w)
			}
			lines = append(lines, convo.Line{Text: r, Ref: ref})
		}
	}
	section := func(title string, jobs []*convo.Job) {
		if len(jobs) == 0 {
			return
		}
		lines = append(lines, convo.Line{Text: fit("  "+paint(cSub+bold, title), w)})
		for i, j := range jobs {
			ref := "job:" + j.ID
			kind := c.sess.JobKind(j)
			icon, col := jobIcon(kind)
			mark := paint(col, icon)
			if j.Running() && !j.Background {
				mark = paint(cOrange, spinner[(m.tick+i)%len(spinner)])
			}
			n := 1
			if j.Running() {
				n = 3
			}
			if c.open[ref] {
				n = jobTailMost
			}
			tail, from := m.jobTailFrom(c, j, n)
			// A finished task's rows stay as they were drawn until what
			// they're drawn from changes: a long session has hundreds.
			var key jobRowsKey
			if memo := !j.Running() && !c.open[ref]; memo {
				key = jobRowsKey{w: w, pal: cText + cSub, status: j.Status, end: j.End, from: from, tail: len(tail),
					facts: fmt.Sprint(j.ToolUses, j.Tokens, len(j.Error), len(j.Summary))}
				if len(tail) > 0 {
					key.last = tail[len(tail)-1]
				}
				if e, ok := c.jobRows[j.ID]; ok && e.key == key {
					emit(ref, e.rows, true)
					continue
				}
			}
			right := jobState(j, now) + "  "
			// A shell says what it's for; its command comes under it.
			label, cmd := jobLabel(c, j), ""
			if full := c.sess.JobCommand(j); kind == "shell" && full != "" && j.Label != "" && j.Label != full {
				label, cmd = oneLine(j.Label), jobLabel(c, j)
			}
			left := "  " + mark + " " + paint(cText+bold, fmt.Sprintf("%-8s", kind)) + " " +
				paint(cSub, ansi.Truncate(label, max(12, w-cellw.String(ansi.Strip(right))-16), "…"))
			rows := []string{spread(left, right, w)}
			var facts []string
			if rp, ok := c.sess.RunningPart(j.ToolUseID); ok {
				facts = append(facts, paint(cOrange, fmt.Sprintf("▸ %d/%d", rp.At, rp.Of))+" "+paint(cText, oneLine(rp.Command)))
			}
			if j.ToolUses > 0 {
				facts = append(facts, fmt.Sprintf("%d steps", j.ToolUses))
			}
			if j.Tokens > 0 {
				facts = append(facts, convo.Tokens(j.Tokens)+" tokens")
			}
			if s := firstNonEmpty(j.Error, j.Summary); s != "" && s != j.Label {
				facts = append(facts, oneLine(s))
			}
			if len(facts) > 0 {
				rows = append(rows, ansi.Truncate("      "+faint(strings.Join(facts, faint(" · "))), w-2, "…"))
			}
			// Opened: the whole command, a command a line with how long
			// each ran; closed, the command on a line.
			var body []convo.Line
			if c.open[ref] {
				body = c.sess.JobLines(j, o, 6)
			}
			if len(body) == 0 && cmd != "" {
				rows = append(rows, ansi.Truncate("      "+faint("$ "+cmd), w-2, "…"))
			}
			top := len(rows)
			if len(tail) > 0 && from != c.jobOutput(j) {
				rows = append(rows, "      "+faint("from "+tildify(from)))
			}
			if len(tail) == 0 && c.open[ref] && !j.Background {
				// Claude Code keeps no file for a call the turn waited on:
				// what it got back is its output.
				if st := c.sess.Step(j.ToolUseID); st != nil {
					tail = lastLines(st.Output, n)
				}
			}
			for _, l := range tail {
				rows = append(rows, ansi.Truncate("      "+paint(cFaint, "│ ")+dim(l), w-2, "…"))
			}
			if len(tail) == 0 && c.open[ref] {
				none := "no output"
				if j.Running() {
					none = "no output yet"
				}
				rows = append(rows, "      "+faint(none))
			}
			if key.w != 0 {
				if c.jobRows == nil {
					c.jobRows = map[string]jobRowsMemo{}
				}
				c.jobRows[j.ID] = jobRowsMemo{key: key, rows: rows}
			}
			emit(ref, rows[:top], true)
			for _, l := range body {
				lines = append(lines, convo.Line{Text: l.Text, Ref: ref})
			}
			// Under the command's well the output reads as the call's, as in
			// the conversation, not as the pick again.
			emit(ref, rows[top:], len(body) == 0)
		}
		lines = append(lines, convo.Line{Text: ""})
	}
	section("Running", run)
	section("Finished", done)
	return lines
}

// jobKey acts on a picked task, and ctrl+b on whatever the turn waits on.
func (m *Model) jobKey(c *hostConn, s string, empty bool) (tea.Cmd, bool) {
	if s == "ctrl+b" && c.client != nil {
		// Only while the dock offers it; otherwise ctrl+b is /btw's.
		if !slices.ContainsFunc(c.dockJobs(), func(j *convo.Job) bool { return !j.Background }) {
			return nil, false
		}
		if j := m.pickedJob(c); j != nil && !j.Background {
			return m.backgroundJob(c, j), true
		}
		return m.backgroundJob(c, nil), true
	}
	if cmd, used := m.shellKey(c, s, empty); used {
		return cmd, true
	}
	j := m.pickedJob(c)
	if j == nil {
		return nil, false
	}
	if strings.HasPrefix(c.sel, "run:") && s != "b" {
		return nil, false // the subagent's own keys
	}
	switch {
	case s == "ctrl+x" || s == "x" && empty:
		return m.stopJob(c, j), true
	case s == "b" && empty:
		if j.Background {
			m.flash("that's already in the background", false)
			return nil, true
		}
		return m.backgroundJob(c, j), true
	case s == "enter" && empty:
		ref := "job:" + j.ID
		if m.viewName(c) != "background" {
			// From the dock: the background view, on it, opened.
			for i, v := range m.views(c) {
				if v == "background" {
					c.view = i
				}
			}
			c.open[ref], c.sel, c.selMoved = true, ref, true
			return nil, true
		}
		c.open[ref] = !c.open[ref]
		return nil, true
	}
	return nil, false
}

// pickedJob is the task picked, in the dock or the background view; a
// subagent picked in the dock counts as its task.
func (m *Model) pickedJob(c *hostConn) *convo.Job {
	if id, ok := strings.CutPrefix(c.sel, "job:"); ok {
		return c.sess.Job(id)
	}
	if id, ok := strings.CutPrefix(c.sel, "run:"); ok {
		for _, sa := range c.subs {
			if sa.ID != id {
				continue
			}
			for _, j := range c.sess.RunningJobs() {
				if j.ToolUseID != "" && j.ToolUseID == sa.ToolUseID || j.ID == sa.ID {
					return j
				}
			}
		}
	}
	return nil
}

func (m *Model) stopJob(c *hostConn, j *convo.Job) tea.Cmd {
	switch {
	case !j.Running():
		m.flash("that has already finished", false)
		return nil
	case c.client == nil:
		m.flash("rush can stop a task only in a session it runs; this one is Claude Code's", true)
		return nil
	}
	what := c.sess.JobKind(j)
	if j.Background {
		m.flash("stopping the "+what+" · the rest carries on", false)
	} else {
		m.flash("stopping the "+what+" · Claude hears it was stopped and carries on", false)
	}
	cl, id := c.client, j.ID
	return hostCmd(func() error { return cl.StopTask(id) })
}

// backgroundJob sends a task the turn waits on into the background (nil:
// every one), so the turn carries on and Claude hears when it's done.
func (m *Model) backgroundJob(c *hostConn, j *convo.Job) tea.Cmd {
	if c.client == nil {
		return nil
	}
	if c.sess.Info.Proto < 3 {
		m.flash("this session's host is older than this rush · /restart it to move tasks to the background", true)
		return nil
	}
	id := ""
	if j != nil {
		id = j.ToolUseID
		m.flash("moved the "+c.sess.JobKind(j)+" to the background · the turn carries on", false)
	} else {
		m.flash("moved what the turn was waiting on to the background", false)
	}
	cl := c.client
	return hostCmd(func() error { return cl.Background(id) })
}

// jobTail is a task's last n lines of output (at most jobTailMost), if
// it writes any where rush can find it. The background view draws every
// task's tail each frame, so each file is read once and then only looked
// at again, at most every tailEvery, while its task runs: a stat, and a
// read only when it has grown. Both happen in the background; a frame
// draws what was last read.
func (m *Model) jobTail(c *hostConn, j *convo.Job, n int) []string {
	lines, _ := m.jobTailFrom(c, j, n)
	return lines
}

// jobTailFrom is jobTail and the file it's from: Claude Code's own, or,
// when that has nothing, a file the command sends its output to.
func (m *Model) jobTailFrom(c *hostConn, j *convo.Job, n int) ([]string, string) {
	p := c.jobOutput(j)
	if lines := c.tailOf(p, j.Running(), n); len(lines) > 0 || c.sess.JobKind(j) != "shell" {
		return lines, p
	}
	for _, w := range c.jobWrites(j) {
		if lines := c.tailOf(w, j.Running(), n); len(lines) > 0 {
			return lines, w
		}
	}
	return nil, ""
}

// jobWrites is the files a task's command writes to, worked out once: a
// frame asks for every task's.
func (c *hostConn) jobWrites(j *convo.Job) []string {
	if w, ok := c.writes[j.ToolUseID]; ok {
		return w
	}
	w := c.sess.JobWrites(j)
	if c.sess.JobCommand(j) != "" { // its call may not be read yet
		if c.writes == nil {
			c.writes = map[string][]string{}
		}
		c.writes[j.ToolUseID] = w
	}
	return w
}

// jobRowsKey is what a finished task's rows are drawn from.
type jobRowsKey struct {
	w                              int
	pal, status, from, last, facts string
	end                            time.Time
	tail                           int
}

// jobRowsMemo is a finished task's rows as last drawn, unpicked.
type jobRowsMemo struct {
	key  jobRowsKey
	rows []string
}

// tailOf is the last n lines of the file at p, read as jobTail says.
func (c *hostConn) tailOf(p string, running bool, n int) []string {
	if p == "" {
		return nil
	}
	if c.tails == nil {
		c.tails = map[string]*jobTailed{}
	}
	e := c.tails[p]
	if e == nil {
		e = &jobTailed{size: -2}
		c.tails[p] = e
	}
	e.poll()
	if now := time.Now(); !e.final && now.Sub(e.at) >= tailEvery {
		e.at = now
		size, mod := e.size, e.mod
		if e.read.start(func() tailRead { return readTail(p, size, mod, running) }) {
			e.poll() // tests read at once
		}
	}
	return e.lines[max(0, len(e.lines)-n):]
}

// tailRead is a task's output as a read in the background found it.
type tailRead struct {
	size  int64 // -1 when there was nothing there
	mod   time.Time
	lines []string
	same  bool // unchanged: nothing was read
	final bool // read after its task ended: it won't change
}

// readTail looks at a task's output, reading it when it isn't the size
// and time it was.
func readTail(p string, size int64, mod time.Time, running bool) tailRead {
	st, err := os.Stat(p)
	switch {
	case err != nil:
		return tailRead{size: -1}
	case st.Size() == size && st.ModTime().Equal(mod):
		return tailRead{same: true, size: size, final: !running}
	}
	// Read once its task had ended, it won't change again.
	return tailRead{size: st.Size(), mod: st.ModTime(), lines: tailLines(p, jobTailMost), final: !running}
}

// poll takes in a read that's done.
func (e *jobTailed) poll() {
	r, ok := e.read.take()
	if !ok {
		return
	}
	if !r.same {
		e.size, e.mod, e.lines = r.size, r.mod, r.lines
	}
	e.final = r.final && e.size >= 0
}

// jobTailMost is the most of a task's output shown: an opened one's.
const jobTailMost = 30

// tailEvery is how often a running task's output is looked at again.
const tailEvery = 500 * time.Millisecond

// jobTailed is a task's output as last read.
type jobTailed struct {
	at    time.Time // when it was last looked at
	size  int64     // -1 when there was nothing there
	mod   time.Time
	lines []string
	final bool // read after its task ended: it won't change
	read  offRead[tailRead]
}

// jobOutput is the file a task writes its output to: Claude Code says
// where once it's done; while it runs, it's in the session's tasks folder
// under its own temp folder, found once, by the task's id.
func (c *hostConn) jobOutput(j *convo.Job) string {
	if j.OutputFile != "" {
		return j.OutputFile
	}
	if c.client == nil || c.id == "" || c.sess.Info.SessionID == "" {
		return ""
	}
	if c.taskDirFor != c.sess.Info.SessionID {
		c.taskDir, c.taskDirAt, c.taskDirFor = "", time.Time{}, c.sess.Info.SessionID
	}
	if d, ok := taskDirs.take(c); ok && d.sid == c.taskDirFor {
		c.taskDir = d.dir
	}
	if c.taskDir == "" && time.Since(c.taskDirAt) >= 2*time.Second {
		c.taskDirAt = time.Now()
		sid := c.sess.Info.SessionID
		pat := filepath.Join(host.TempDir(c.id), "claude-*", "*", sid, "tasks")
		taskDirs.start(c, func() taskDir {
			d := taskDir{sid: sid}
			if found, _ := filepath.Glob(pat); len(found) > 0 {
				d.dir = found[0]
			}
			return d
		})
		if d, ok := taskDirs.take(c); ok && d.sid == c.taskDirFor { // tests look at once
			c.taskDir = d.dir
		}
	}
	if c.taskDir == "" {
		return ""
	}
	return filepath.Join(c.taskDir, j.ID+".output")
}

// taskDir is a session's tasks folder, looked for in the background.
type taskDir struct{ sid, dir string }

var taskDirs = offReads[*hostConn, taskDir]{}

// tailWidest is the most of a line of output kept, in bytes.
const tailWidest = 1024

// tailLines is the last n non-blank lines of a file, from its last 16KB.
func tailLines(path string, n int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() == 0 {
		return nil
	}
	const most = 16 << 10
	off := max(0, st.Size()-most)
	buf := make([]byte, st.Size()-off)
	if _, err := f.ReadAt(buf, off); err != nil && err != io.EOF {
		return nil
	}
	var out []string
	for _, l := range strings.Split(string(buf), "\n") {
		// A progress bar redraws itself with \r: its latest state counts.
		if i := strings.LastIndexByte(strings.TrimRight(l, "\r"), '\r'); i >= 0 {
			l = l[i+1:]
		}
		if l = strings.TrimRight(ansi.Strip(l), " \t\r"); l != "" {
			// No row is wider than this: the rest would only be cut off
			// again, every frame.
			if len(l) > tailWidest {
				l = strings.ToValidUTF8(l[:tailWidest], "")
			}
			out = append(out, strings.ReplaceAll(l, "\t", "  "))
		}
	}
	if off > 0 && len(out) > 0 {
		out = out[1:] // cut mid-line
	}
	return out[max(0, len(out)-n):]
}

// lastLines is the last n non-blank lines of s.
func lastLines(s string, n int) []string {
	var out []string
	for l := range strings.SplitSeq(ansi.Strip(s), "\n") {
		if l = strings.TrimRight(l, " \t\r"); l != "" {
			out = append(out, strings.ReplaceAll(l, "\t", "  "))
		}
	}
	return out[max(0, len(out)-n):]
}

// firstWord is a command's program.
func firstWord(cmd string) string {
	for _, f := range strings.Fields(cmd) {
		if !strings.Contains(f, "=") {
			return f
		}
	}
	return cmd
}
