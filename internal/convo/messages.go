package convo

import (
	"bytes"
	"github.com/0xdeafcafe/agtop/internal/agent/tool"
	"github.com/0xdeafcafe/agtop/internal/jsonx"
	"github.com/charmbracelet/x/ansi"
	"maps"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// A message is something Claude said rather than something it ran: to a
// subagent or another session, a subagent's report back to its caller, a
// notification to you. The words are all there is to it, so it draws as
// them, in a card whose top edge is the step's row: nothing to open, and it
// never folds away into a run.
func messageTool(tool string) bool {
	switch tool {
	case "SendMessage", "SubagentHandback", "PushNotification":
		return true
	}
	return false
}

// messageText is what a message said and its gist, when it gave one.
func messageText(in input) (text, gist string) {
	return firstNonEmpty(in.str("message"), in.str("text"), in.str("content")), oneLine(in.str("summary"))
}

// recipient is who a message went to: the subagent's description when it's
// one this session started, else the name or id it was sent to.
func (d *drawer) recipient(st *Step) string {
	in := readInput(st.Input)
	to := firstNonEmpty(in.str("to"), in.str("recipient"))
	if to == "" {
		return "an agent"
	}
	toB := []byte(to)
	for _, a := range d.s.byID {
		// Only a call whose JSON names it can match: most are passed over
		// without decoding their prompt and report.
		if a.kind() != tool.Subagent || !bytes.Contains(a.Result, toB) && !bytes.Contains(a.Input, toB) {
			continue
		}
		var r struct {
			AgentID     string `json:"agentId"`
			Description string `json:"description"`
		}
		_ = jsonx.Unmarshal(a.Result, &r)
		ain := readInput(a.Input)
		if r.AgentID == to || ain.str("name") == to {
			return firstNonEmpty(oneLine(a.in().Description), oneLine(r.Description), to)
		}
	}
	if hexID.MatchString(to) {
		return "agent " + to[:7]
	}
	return to
}

var hexID = regexp.MustCompile(`^a?[0-9a-f]{12,}$`)

// peers is each session's name as agtop lists it, by the codename other
// sessions know it by (agtop-8a, as ListAgents gives it).
var peers atomic.Pointer[map[string]string]

// SetPeers is who each codename is, as the list names them now. What was
// drawn before they changed is drawn again.
func SetPeers(names map[string]string) {
	if old := peers.Load(); old != nil && maps.Equal(*old, names) {
		return
	}
	peers.Store(&names)
	lookupsGen.Add(1)
}

// sentTo is who a message went to, a session named as the list names it
// with its codename after.
// ponytail: a one-line label is memoized by step alone, so a session
// renamed after it was drawn keeps its old name in that row; stepKey would
// need a peers generation to follow it.
func (d *drawer) sentTo(st *Step) string {
	to := d.stepMemo(st, 'r', d.recipient)
	if p := peers.Load(); p != nil {
		if n := oneLine((*p)[to]); n != "" && n != to {
			return ansi.Truncate(n, 32, "…") + " (" + to + ")"
		}
	}
	return to
}

// delivery is how a message went, for the card's bottom edge.
func delivery(st *Step) string {
	switch st.Status {
	case Running:
		return paint(cOrange, "sending…")
	case Waiting:
		return paint(cYellow, "waiting on you")
	case Denied:
		return dim("not sent")
	case Failed:
		msg := oneLine(toolErrTag.Replace(st.Output))
		msg = strings.TrimPrefix(msg, "Error: ")
		return paint(cRed, firstNonEmpty(msg, "didn't send"))
	}
	type sent struct {
		Message   string `json:"message"`
		Resumed   string `json:"resumedAgentId"`
		PushSent  bool   `json:"pushSent"`
		LocalSent bool   `json:"localSent"`
	}
	// The result, or its text when the structured one didn't come.
	var r sent
	if jsonx.Unmarshal(st.Result, &r) != nil || r == (sent{}) {
		_ = jsonx.Unmarshal([]byte(st.Output), &r)
	}
	switch st.Tool {
	case "PushNotification":
		switch {
		case r.PushSent:
			return dim("sent to your phone")
		case r.LocalSent:
			return dim("shown in the terminal")
		}
		return ""
	case "SubagentHandback":
		return dim("delivered")
	}
	switch {
	case r.Resumed != "":
		return dim("woke it up")
	case strings.Contains(r.Message, "next tool round"):
		return dim("read at its next step")
	}
	return dim("sent")
}

// message draws a message step as its card; ref is the step's, on the top
// edge. Opened, it shows the whole message rather than its start.
func (d *drawer) message(st *Step, ref string, indent int) {
	in := readInput(st.Input)
	body, gist := messageText(in)
	room := min(d.cw, capRow) - indent - 4
	if room < 24 {
		d.add(ref, "", d.spine()+blanks(indent-1)+d.statusMark(st)+" "+d.stepMemo(st, 'l', d.label), "")
		return
	}
	room = min(room, 72)
	frame, mark := cFaint, cBlue
	switch st.Status {
	case Failed:
		frame, mark = cLostQ, cLost
	case Waiting:
		frame, mark = cWarnQ, cYellow
	case Denied:
		mark = cDim
	}
	var headL string
	// A message that went is the usual case: only one that's still going,
	// waiting or failed says how it stands.
	if st.Status != OK {
		headL = d.statusMark(st) + " "
	}
	switch st.Tool {
	case "SendMessage":
		headL += paint(mark, "→") + " " + dim("to ") + text(d.sentTo(st))
	case "SubagentHandback":
		headL += paint(mark, "↩") + " " + dim("reported back")
	case "PushNotification":
		headL += paint(mark, "◉") + " " + dim("notified you")
	}
	var rows []string
	open := d.o.Verbose || d.o.Open[ref]
	switch {
	case open:
		if gist != "" {
			rows = wrap(paint(cWhite, gist), room)
		}
		for _, l := range strings.Split(strings.TrimSpace(body), "\n") {
			if strings.TrimSpace(l) == "" {
				rows = append(rows, "")
				continue
			}
			rows = append(rows, wrap(sub(expandTabs(l)), room)...)
		}
	case gist != "":
		rows = cardText(gist, strings.Split(body, "\n"), room)
	default:
		// No gist: the message's first line stands in for one.
		first, rest, _ := strings.Cut(strings.TrimSpace(body), "\n")
		rows = cardText(first, strings.Split(rest, "\n"), room)
	}
	d.box(ref, indent, headL, "", rows, delivery(st), room, frame)
}

// --- labels for Claude Code's own tools ---

var (
	peerCount = regexp.MustCompile(`\((\d+)\):`)
	tookFor   = regexp.MustCompile(`task: \S+ \((.*)\)$`)
)

// toolLabel is the row for one of Claude Code's own tools that has no row
// of its own yet, and whether it had one.
func (d *drawer) toolLabel(st *Step, lbl func(string) string) (string, bool) {
	in := readInput(st.Input)
	g := func(s string) string { return glyphColor(s) + " " }
	switch st.Tool {
	case "SendMessage":
		body, gist := messageText(in)
		return g("→") + lbl("to "+d.sentTo(st)) + "  " + faint(firstNonEmpty(gist, oneLine(body))), true
	case "SubagentHandback":
		body, _ := messageText(in)
		return g("↩") + lbl("reported back") + "  " + faint(oneLine(body)), true
	case "PushNotification":
		return g("◉") + lbl("notified you") + "  " + faint(oneLine(in.str("message"))), true
	case "ListAgents":
		return g("⇉") + lbl("listed agents"), true
	case "TaskStop", "KillShell":
		var r struct {
			Command string `json:"command"`
		}
		_ = jsonx.Unmarshal(st.Result, &r)
		what := oneLine(r.Command)
		if what == "" {
			if m := tookFor.FindStringSubmatch(oneLine(st.Output)); m != nil {
				what = m[1]
			}
		}
		if what == "" {
			what = firstNonEmpty(in.str("task_id"), in.str("shell_id"), "a background task")
		}
		return g("⏹") + lbl("stopped ") + faint(what), true
	case "TaskOutput", "BashOutput":
		return g("◔") + lbl("checked ") + faint(firstNonEmpty(in.str("task_id"), in.str("bash_id"), "a background task")), true
	case "Monitor":
		what := oneLine(in.str("description"))
		if what == "" {
			what = program(firstNonEmpty(in.str("command"), in.str("until")))
		}
		return g("◔") + lbl("watching ") + faint(what), true
	case "ScheduleWakeup":
		if b, _ := in["stop"].(bool); b {
			return g("◔") + lbl("ended the loop"), true
		}
		l := lbl("wake up")
		if s, ok := in["delaySeconds"].(float64); ok && s > 0 {
			l = lbl("wake in " + dur(time.Duration(s)*time.Second))
		}
		return g("◔") + l + "  " + faint(oneLine(in.str("reason"))), true
	case "ToolSearch":
		q := in.str("query")
		if names, ok := strings.CutPrefix(q, "select:"); ok {
			return g("⌕") + lbl("loaded ") + faint(strings.ReplaceAll(names, ",", ", ")), true
		}
		return g("⌕") + lbl("looked up tools ") + faint(oneLine(q)), true
	case "EnterWorktree":
		p := firstNonEmpty(in.str("path"), in.str("name"))
		return g("⎇") + lbl("into worktree ") + faint(p[strings.LastIndexByte(p, '/')+1:]), true
	case "ExitWorktree":
		return g("⎇") + lbl("left the worktree") + "  " + faint(in.str("action")), true
	case "EnterPlanMode":
		return g("◇") + lbl("planning"), true
	case "ExitPlanMode":
		return g("◇") + lbl("has a plan for you"), true
	}
	return "", false
}

// toolSummary is how one of Claude Code's own tools came out, for its row.
func toolSummary(st *Step) string {
	if st.Status != OK {
		return ""
	}
	var r map[string]any
	_ = jsonx.Unmarshal(st.Result, &r)
	switch st.Tool {
	case "ListAgents":
		n := 0
		for _, m := range peerCount.FindAllStringSubmatch(st.Output, -1) {
			k, _ := strconv.Atoi(m[1])
			n += k
		}
		if !peerCount.MatchString(st.Output) {
			return ""
		}
		return faint(plural(n, "agent"))
	case "ToolSearch":
		if ms, ok := r["matches"].([]any); ok {
			if len(ms) == 0 {
				return faint("none found")
			}
			if strings.HasPrefix(readInput(st.Input).str("query"), "select:") {
				return ""
			}
			return faint(plural(len(ms), "tool"))
		}
	case "ScheduleWakeup":
		if ms, ok := r["scheduledFor"].(float64); ok && ms > 0 {
			return faint("at " + time.UnixMilli(int64(ms)).Local().Format("15:04"))
		}
	case "Monitor":
		if ms, ok := r["timeoutMs"].(float64); ok && ms > 0 {
			if p, _ := r["persistent"].(bool); p {
				return faint("until stopped")
			}
			return faint("up to " + dur(time.Duration(ms)*time.Millisecond))
		}
	case "EnterWorktree":
		if b, _ := r["worktreeBranch"].(string); b != "" {
			return paint(cBlue, b)
		}
	}
	return ""
}
