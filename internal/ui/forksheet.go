package ui

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/actions"
	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/convo"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/host"
)

// --- /fork ---

// forkSheet sets up a fork before it starts: its name, how much of the
// conversation it keeps, model, effort, permissions, folder (this one or a
// new worktree) and a first message.
type forkSheet struct {
	conn, agent string
	acct        claude.Account
	sid, cwd    string
	path        string // the conversation's transcript
	turns       []forkTurn
	repo        string // the git checkout, when there is one

	row          int
	name, first  []rune
	namePos      int
	firstPos     int
	upTo         int // index into turns; 0 keeps them all
	model        int
	effort, perm int
	worktree     bool
	now          host.Info
	was          host.Config
	hosted       bool
}

type forkTurn struct {
	n      int
	prompt string
	end    time.Time
}

const (
	fkName = iota
	fkFrom
	fkModel
	fkEffort
	fkPerm
	fkFolder
	fkFirst
	fkGo
	fkRows
)

var (
	forkModels  = []string{"", "opus", "opus[1m]", "sonnet", "haiku", "fable"}
	forkEfforts = []string{"", "low", "medium", "high", "xhigh", "max"}
	forkPerms   = []string{"", "default", "acceptEdits", "plan", "auto", "bypassPermissions"}
)

// openFork opens the /fork sheet for the agent, name prefilled from arg.
func (m *Model) openFork(c *hostConn, a *fleet.Agent, name string) {
	sid := firstNonEmpty(c.sess.Info.SessionID, a.SessionID)
	if sid == "" || len(c.sess.Turns) == 0 {
		m.flash("nothing to fork yet: "+a.DisplayName+" hasn't had a turn", true)
		return
	}
	cwd := firstNonEmpty(c.sess.Info.Cwd, a.Cwd)
	f := &forkSheet{
		conn: c.key, agent: a.DisplayName, acct: a.Acct, sid: sid, cwd: cwd,
		path: firstNonEmpty(c.path, a.TranscriptPath, a.Acct.TranscriptPath(cwd, sid)),
		name: []rune(firstNonEmpty(name, a.DisplayName+" (fork)")),
		now:  c.sess.Info, hosted: a.Agtop, repo: actions.RepoRoot(cwd),
	}
	f.namePos = len(f.name)
	if a.Agtop {
		f.was, _ = host.ReadConfig(a.ID)
	}
	// Newest first: "all of it", then up to each earlier turn.
	last := c.sess.Turns[len(c.sess.Turns)-1]
	f.turns = append(f.turns, forkTurn{n: last.N, prompt: last.Prompt, end: last.End})
	for i := len(c.sess.Turns) - 2; i >= 0; i-- {
		t := c.sess.Turns[i]
		f.turns = append(f.turns, forkTurn{n: t.N, prompt: t.Prompt, end: t.End})
	}
	m.sheet = f
}

// openForkAt is alt+f on a turn in the history: /fork, remembering up to
// and including it.
func (m *Model) openForkAt(c *hostConn, a *fleet.Agent, t *convo.Turn) {
	m.openFork(c, a, "")
	if f, ok := m.sheet.(*forkSheet); ok {
		for i, ft := range f.turns {
			if ft.n == t.N {
				f.upTo, f.row = i, fkFrom
			}
		}
	}
}

func (f *forkSheet) cycle(d int) {
	step := func(i, n int) int { return (i + d + n) % n }
	switch f.row {
	case fkFrom:
		f.upTo = step(f.upTo, len(f.turns))
	case fkModel:
		f.model = step(f.model, len(forkModels))
	case fkEffort:
		f.effort = step(f.effort, len(forkEfforts))
	case fkPerm:
		f.perm = step(f.perm, len(forkPerms))
	case fkFolder:
		if f.repo != "" {
			f.worktree = !f.worktree
		}
	}
}

func (f *forkSheet) key(m *Model, k tea.KeyPressMsg, s string) tea.Cmd {
	switch s {
	case "esc", "ctrl+c":
		m.sheet = nil
		return nil
	case "up", "shift+tab":
		f.row = (f.row + fkRows - 1) % fkRows
		return nil
	case "down", "tab":
		f.row = (f.row + 1) % fkRows
		return nil
	case "enter", "ctrl+s":
		return f.start(m)
	}
	switch f.row {
	case fkName:
		f.name, f.namePos, _ = edit(f.name, f.namePos, k, s)
		return nil
	case fkFirst:
		f.first, f.firstPos, _ = edit(f.first, f.firstPos, k, s)
		return nil
	}
	switch s {
	case "left", "h":
		f.cycle(-1)
	case "right", "l", "space":
		f.cycle(1)
	}
	return nil
}

// current is what the session runs with now, for "as now".
func (f *forkSheet) current(what string) string {
	switch what {
	case "model":
		return firstNonEmpty(f.now.Model, f.was.Model)
	case "effort":
		return firstNonEmpty(f.now.Effort, f.was.Effort)
	}
	return firstNonEmpty(f.now.PermissionMode, f.was.PermissionMode)
}

func (f *forkSheet) body(m *Model, w, h int) []string {
	out := []string{
		sheetTitle("Fork "+oneLine(f.agent), "a copy of the conversation carries on as a new agent; this one stays as it is", w),
		"",
	}
	labelW := 16
	valW := w - labelW - 6
	choice := func(v, what string) string {
		if v == "" {
			if now := f.current(what); now != "" {
				if what == "model" {
					now = claude.ModelName(now)
				}
				return "as now · " + now
			}
			return "as now"
		}
		return v
	}
	for r := 0; r < fkRows; r++ {
		on := r == f.row
		var label, val string
		switch r {
		case fkName:
			label, val = "Name", textField(f.name, f.namePos, on, "what to call it", valW)
		case fkFrom:
			label = "Remembers"
			t := f.turns[f.upTo]
			if f.upTo == 0 {
				val = fmt.Sprintf("the whole conversation · %d turns", t.n)
			} else {
				val = fmt.Sprintf("up to turn %d · ", t.n) + faint(oneLine(t.prompt))
			}
		case fkModel:
			label, val = "Model", choice(forkModels[f.model], "model")
		case fkEffort:
			label, val = "Effort", choice(forkEfforts[f.effort], "effort")
		case fkPerm:
			label, val = "Permissions", choice(forkPerms[f.perm], "perm")
		case fkFolder:
			label = "Folder"
			switch {
			case f.worktree:
				val = "a new worktree · " + tildify(filepath.Join(f.repo, ".claude", "worktrees", worktreeName(string(f.name))))
			case f.repo == "":
				val = "same folder · " + tildify(f.cwd) + faint(" (not a git repo, so no worktree)")
			default:
				val = "same folder · " + tildify(f.cwd)
			}
		case fkFirst:
			label, val = "First message", textField(f.first, f.firstPos, on, "optional: what to tell it first", valW)
		case fkGo:
			btn := " Fork "
			if on {
				out = append(out, "", "  "+onBg(selBG, paint(cOrange+bold, btn), len(btn))+dim("  enter"))
			} else {
				out = append(out, "", "  "+paint(cSub, "[Fork]")+dim("  enter"))
			}
			continue
		}
		line := paint(cSub, fit(label, labelW))
		if r == fkName || r == fkFirst {
			line += "  " + val
		} else {
			line += faint("‹ ") + paint(cText, ansi.Truncate(val, valW, "…")) + faint(" ›")
		}
		out = append(out, sheetRow(line, on, w))
		if r == fkFirst {
			// Where that message lands: after the cache, or breaking it.
			out = append(out, strings.Repeat(" ", labelW+4)+ansi.Truncate(f.firstNote(), max(10, w-labelW-5), "…"))
		}
	}
	// What the fork will and won't remember.
	out = append(out, "")
	if f.upTo > 0 {
		t := f.turns[f.upTo]
		out = append(out, dim(fmt.Sprintf("  It remembers turns 1–%d and forgets %d–%d. This one keeps them all.", t.n, t.n+1, f.turns[0].n)))
	} else {
		out = append(out, dim("  It remembers everything so far, then goes its own way."))
	}
	if f.worktree {
		out = append(out, dim("  A new branch, worktree-"+worktreeName(string(f.name))+", so its edits don't touch this checkout."))
	}
	return append(out, "", keysFit(w, "↑↓", "move", "←→", "change", "enter", "fork", "esc", "cancel"))
}

// firstNote says whether the fork's first message is added after the
// prompt cache it shares with this conversation, so what it remembers is
// read at about a tenth of the price, or breaks it. Measured: another
// model or effort re-reads it all; permissions (plan too) don't.
func (f *forkSheet) firstNote() string {
	if md := forkModels[f.model]; md != "" && !strings.Contains(strings.ToLower(f.current("model")), strings.TrimSuffix(md, "[1m]")) {
		return paint(cYellow, "↳ breaks the cache: another model reads all it remembers at full price")
	}
	if e := forkEfforts[f.effort]; e != "" && !strings.EqualFold(e, f.current("effort")) {
		return paint(cYellow, "↳ breaks the cache: another effort reads all it remembers at full price")
	}
	warm := f.now.CacheWarm
	if f.upTo > 0 {
		// A point further back was cached when it happened.
		if end := f.turns[f.upTo].end; !end.IsZero() {
			warm = end.Add(time.Hour)
		}
	}
	switch {
	case time.Now().Before(warm):
		return paint(cGreen, "↳ added after the cache") + dim(" (warm till "+warm.Local().Format("15:04")+"): what it remembers costs about a tenth")
	case f.now.CacheWarm.IsZero() && f.upTo == 0:
		return dim("↳ added after what it remembers: read from the cache if used in the last hour")
	}
	return dim("↳ added after what it remembers, but that point's cache is cold: full price once")
}

var worktreeUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

// worktreeName is a folder and branch name made from the fork's name.
func worktreeName(name string) string {
	s := strings.Trim(worktreeUnsafe.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(s) > 40 {
		s = strings.TrimRight(s[:40], "-")
	}
	return firstNonEmpty(s, "fork")
}

// start makes the fork. The whole conversation in the same folder is
// Claude Code's own --fork-session; up to an earlier turn, or into a new
// worktree, agtop copies the transcript (cut there) and resumes the copy.
func (f *forkSheet) start(m *Model) tea.Cmd {
	d := m.store.Config.Dispatch
	name := strings.TrimSpace(string(f.name))
	if name == "" {
		name = f.agent + " (fork)"
	}
	cfg := host.Config{
		Account: f.acct, Cwd: f.cwd, Name: name, Resume: true,
		Model:          firstNonEmpty(forkModels[f.model], f.current("model"), d.Model),
		Effort:         firstNonEmpty(forkEfforts[f.effort], f.current("effort"), d.Effort),
		PermissionMode: firstNonEmpty(forkPerms[f.perm], f.current("perm"), d.Permission),
		LimitMode:      firstNonEmpty(f.was.LimitMode, d.OnLimit), Flags: f.was.Flags,
		Lean: d.Lean, IdleStop: host.Duration(d.Rest()),
		Prompt: strings.TrimSpace(string(f.first)),
	}
	var cut *forkTurn
	next := ""
	if f.upTo > 0 {
		cut, next = &f.turns[f.upTo], f.turns[f.upTo-1].prompt
	}
	worktree, src, sid := f.worktree, f.path, f.sid
	wtName := worktreeName(name)
	m.sheet = nil
	m.flash("forking "+f.agent+"…", false)
	return func() tea.Msg {
		if cut == nil && !worktree {
			cfg.SessionID, cfg.Fork, cfg.From = sid, true, sid
		} else {
			if worktree {
				dir, err := actions.NewWorktree(cfg.Cwd, wtName)
				if err != nil {
					return doneMsg{err: err}
				}
				cfg.Cwd = dir
			}
			upTo := int64(-1)
			if cut != nil {
				var err error
				if upTo, err = cutAfter(src, cut.n, next); err != nil {
					return doneMsg{err: err}
				}
			}
			newID, _ := host.NewSessionID()
			if err := claude.CopyTranscript(src, cfg.Account.TranscriptPath(cfg.Cwd, newID), sid, newID, upTo); err != nil {
				return doneMsg{err: fmt.Errorf("couldn't copy the conversation: %w", err)}
			}
			// Its file checkpoints too, so it can rewind; without them it
			// still runs.
			_ = cfg.Account.CopyCheckpoints(sid, newID)
			cfg.SessionID = newID
		}
		started, err := host.Spawn(cfg)
		if err != nil {
			return doneMsg{err: err}
		}
		return hostStartedMsg{id: started.ID, name: cfg.Name, acct: cfg.Account.Name}
	}
}

// cutAfter is the byte offset in the transcript where turn n ends: where
// the turn after it, whose prompt is next, starts. The prompt decides when
// the numbering differs (a hosted session numbers what its host saw).
func cutAfter(path string, n int, next string) (int64, error) {
	starts, err := convo.TurnStarts(path)
	if err != nil {
		return 0, fmt.Errorf("couldn't read the conversation: %w", err)
	}
	best, found := int64(0), false
	gap := -1
	for _, s := range starts {
		if next == "" || s.Prompt != next {
			continue
		}
		if g := abs(s.N - (n + 1)); gap < 0 || g < gap {
			best, gap, found = s.Offset, g, true
		}
	}
	if found {
		return best, nil
	}
	for _, s := range starts {
		if s.N == n+1 {
			return s.Offset, nil
		}
	}
	return 0, fmt.Errorf("couldn't find where turn %d ends in the conversation", n)
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
