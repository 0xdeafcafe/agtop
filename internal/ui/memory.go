package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/convo"
	"github.com/0xdeafcafe/rush/internal/efficiency"
)

// --- memory view ---

// The memory view lists what the session's agent reads for its project,
// as its adapter says: for Claude Code, its auto memory, the CLAUDE.md
// files and rules, settings, and the agents, skills, commands and output
// styles on disk. ↑↓ pick a file, shown in the editor in the bottom half;
// space edits it there, ctrl+g in $EDITOR, x deletes it.

// memFile is one file the agent reads, or one it would read if it existed.
type memFile = agent.MemoryFile

// memoryOf is what the session's agent reads, read again at most every
// two seconds, or at once after an edit (c.mem set to nil); nothing if it
// doesn't say. It's read in the background: until a read is in, it's what
// was read last.
func (m *Model) memoryOf(c *hostConn) []memFile {
	if mem, ok := memReads.take(c); ok {
		if mem.Files == nil {
			mem.Files = []memFile{} // read, and empty: nil asks for a read
		}
		c.mem, c.memAt, c.memInfo = mem.Files, time.Now(), &mem
	}
	if c.mem != nil && time.Since(c.memAt) < 2*time.Second {
		return c.mem
	}
	k := sessionAgent(c)
	mr, ok := agent.As[agent.MemoryReader](k)
	if !ok || !agent.Supports(k, agent.FeatureMemory) {
		c.mem, c.memAt, c.memInfo = []memFile{}, time.Now(), &agent.Memory{}
		return c.mem
	}
	p, cwd, path := m.store.Config.ActiveAccount().Profile(), c.sess.Info.Cwd, c.path
	if a := m.agentByKey(c.key); a != nil {
		p, cwd = a.Acct, firstNonEmpty(cwd, a.Cwd)
	}
	memReads.start(c, func() agent.Memory { return mr.Memory(p, cwd, path) })
	if mem, ok := memReads.take(c); ok { // tests read at once
		if mem.Files == nil {
			mem.Files = []memFile{}
		}
		c.mem, c.memAt, c.memInfo = mem.Files, time.Now(), &mem
	}
	switch {
	case c.mem != nil:
		return c.mem
	case c.memInfo != nil:
		return c.memInfo.Files
	}
	return nil
}

// memReads are the memory views' reads out in the background, by pane.
var memReads = offReads[*hostConn, agent.Memory]{}

// exists is whether a file is there. It asks the disk: not for the UI
// goroutine.
func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// memFileAt is the file at path in what was last read, if it's there.
func memFileAt(files []memFile, path string) (memFile, bool) {
	for i := range files {
		if files[i].Path == path {
			return files[i], true
		}
	}
	return memFile{}, false
}

// memoryLines draws the memory view in h rows: the files on top, and the
// picked one below in the editor, which has the bottom half.
func (m *Model) memoryLines(c *hostConn, o convo.Options, h int) []convo.Line {
	w := o.Width
	files := m.memoryOf(c)
	ed := m.memDoc(c)
	edH := h / 2
	switch {
	case h < 14 && c.memEdit:
		edH = h // too short to share: the editor, alone
	case h < 14:
		edH = 0
	}
	var out []convo.Line
	line := func(text, ref string) { out = append(out, convo.Line{Text: fit(text, w), Ref: ref}) }
	if listH := h - edH; listH > 0 {
		var up int64
		for _, f := range files {
			up += f.Up
		}
		line(spread("  "+paint(cSub+bold, "Memory")+"   "+dim("what "+harnessName(string(sessionAgent(c)))+" reads for this project, and when"),
			paint(cSub, "≈"+efficiency.Tokens(up))+dim(" tokens every session · "+fmt.Sprintf("%d files", len(files)))+"  ", w), "")
		line("  "+faint(strings.Repeat("─", max(0, w-4))), "")
		head := len(out)
		if r := c.memInfo; r != nil && len(r.Problems) > 0 {
			key := "#efficiency findings"
			if strings.HasPrefix(c.sel, "mem:") {
				key = "f"
			}
			n := fmt.Sprintf("%d things to tidy", len(r.Problems))
			if len(r.Problems) == 1 {
				n = "1 thing to tidy"
			}
			left, right := "  "+paint(cYellow, "! "+n), faint(key+" shows them")+"  "
			if room := w - cellwidth(left) - cellwidth(right) - 5; room > 12 {
				left += dim(" · " + ansi.Truncate(r.Problems[0].Title, room, "…"))
			}
			line(spread(left, right, w), "")
		}
		var when map[string]string
		if c.memInfo != nil {
			when = c.memInfo.When
		}
		rows := memRows(files, when, c.sel, w, o, memNothing(c, files))
		sel := 0
		for i, r := range rows {
			if r.Ref == c.sel && c.sel != "" {
				sel = i
				break
			}
		}
		room := max(1, listH-head)
		switch {
		case sel < c.memTop:
			c.memTop = max(0, sel-1) // its group's heading too
		case sel >= c.memTop+room:
			c.memTop = sel - room + 1
		}
		c.memTop = max(0, min(c.memTop, len(rows)-room))
		for i := c.memTop; i < min(len(rows), c.memTop+room); i++ {
			out = append(out, rows[i])
		}
		for len(out) < listH {
			line("", "")
		}
	}
	if edH > 0 {
		for _, l := range m.docPane(c, ed, w, edH, o.Focused) {
			line(l, "")
		}
	}
	return out
}

// memRows is a row for each file, under its group's name and when its
// files reach the model. On the right, what a file costs: ≈ tokens every
// session in full colour, ≈ tokens when it's loaded later faint, nothing
// for what's never sent.
// With no files, it says nothing.
func memRows(files []memFile, memWhen map[string]string, picked string, w int, o convo.Options, nothing string) []convo.Line {
	var out []convo.Line
	group := ""
	for _, f := range files {
		if f.Group != group {
			group = f.Group
			head, when := "  "+paint(cText+bold, group), faint(memWhen[group])+"  "
			// The memory's folder gives way to when.
			if dir := tildify(filepath.Dir(f.Path)); group == "Auto memory" {
				if room := w - 1 - cellwidth(head) - cellwidth(when) - 6; room > 10 {
					head += "  " + faint(ansi.TruncateLeft(dir, max(0, cellwidth(dir)-room), "…"))
				}
			}
			out = append(out, convo.Line{Text: fit(spread(head, when, w-1), w)})
		}
		ref := "mem:" + f.Path
		var meta []string
		if f.Up > 0 {
			meta = append(meta, paint(cSub, "≈"+efficiency.Tokens(f.Up)))
		}
		if f.Later > 0 {
			meta = append(meta, faint("≈"+efficiency.Tokens(f.Later)+" "+f.When))
		}
		switch {
		case f.Missing && f.Warn != "":
			meta = append(meta, dim("missing"))
		case f.Missing:
			meta = append(meta, dim("not written yet"))
		case f.Up == 0 && f.Later == 0:
			meta = append(meta, dim(fileSize(f.Size)+" · "+dur(o.Now.Sub(f.Mod))+" ago"))
		default:
			meta = append(meta, dim(dur(o.Now.Sub(f.Mod))+" ago"))
		}
		right := strings.Join(meta, dim(" · "))
		mark := paint(cBlue, "◆ ")
		switch {
		case f.Missing && f.Warn != "":
			mark = paint(cYellow, "◇ ")
		case f.Missing:
			mark = faint("◇ ")
		}
		left := "    " + mark
		if f.Sub {
			left = "      " + faint("↳ ")
		}
		left += paint(cText, f.Name)
		if f.Kind != "" {
			left += "  " + paint(cSub, f.Kind)
		}
		note, paintNote := f.About, dim
		if f.Warn != "" {
			note, paintNote = f.Warn, func(s string) string { return paint(cYellow, s) }
		}
		if note != "" {
			room := w - cellwidth(left) - cellwidth(right) - 8
			if room > 12 {
				left += "  " + paintNote(ansi.Truncate(oneLine(note), room, "…"))
			}
		}
		r := spread(left, right+"  ", w-1)
		if ref == picked {
			r = picked1(r, w, o.Focused)
		}
		out = append(out, convo.Line{Text: fit(r, w), Ref: ref})
	}
	if len(files) == 0 {
		out = append(out, convo.Line{Text: fit("    "+dim(nothing), w)})
	}
	return out
}

func cellwidth(s string) int { return ansi.StringWidth(s) }

// memNothing is what the memory view says with no files to show.
func memNothing(c *hostConn, files []memFile) string {
	if files == nil && memReads.out(c) {
		return "Reading…"
	}
	return "Nothing found for this project."
}

// memDoc is the picked file, opened in the editor. The one being edited
// stays open whatever is picked.
func (m *Model) memDoc(c *hostConn) *docEditor {
	if c.memEdit && c.memEd != nil {
		c.memEd.refresh()
		return c.memEd
	}
	path, ok := strings.CutPrefix(c.sel, "mem:")
	if !ok {
		return nil
	}
	if c.memEd == nil || c.memEd.path != path {
		c.memEd = openDoc(path)
	}
	c.memEd.refresh()
	return c.memEd
}

// docPane draws the editor in h rows: a title, the file, and a line for
// its problems and where the cursor is.
func (m *Model) docPane(c *hostConn, e *docEditor, w, h int, paneFocused bool) []string {
	rule := func(left, right string) string {
		fill := w - cellwidth(left) - cellwidth(right) - 2
		return left + faint(" "+strings.Repeat("─", max(1, fill))+" ") + right
	}
	if e == nil {
		out := []string{rule(faint("──"), "")}
		for len(out) < h {
			out = append(out, "")
		}
		if h > 3 {
			out[h/2] = "    " + dim("Pick a file above to see it here · space edits it")
		}
		return out
	}
	focused := paneFocused && c.memEdit
	name := paint(cSub, filepath.Base(e.path))
	if focused {
		name = paint(cOrange, "✎ ") + paint(cText+bold, filepath.Base(e.path))
	}
	var chips []string
	switch {
	case e.loading:
		chips = append(chips, dim("reading…"))
	case e.readOnly != "":
		chips = append(chips, paint(cYellow, "read only"))
	case e.dirty():
		chips = append(chips, paint(cOrange, "● unsaved"))
	case e.missing:
		chips = append(chips, dim("new file"))
	}
	if e.stale {
		chips = append(chips, paint(cYellow, "changed on disk"))
	}
	diags := e.problems()
	errs, warns := 0, 0
	for _, d := range diags {
		if d.warn {
			warns++
		} else {
			errs++
		}
	}
	kind := map[docKind]string{docMarkdown: "markdown", docJSON: "json", docText: "text"}[e.kind]
	switch {
	case errs > 0:
		chips = append(chips, paint(cRed, fmt.Sprintf("✗ %d", errs)))
	case warns > 0:
		chips = append(chips, paint(cYellow, fmt.Sprintf("! %d", warns)))
	case e.kind == docJSON && strings.TrimSpace(string(e.buf)) != "":
		chips = append(chips, paint(cGreen, "✓ valid"))
	}
	chips = append(chips, dim(kind))
	right := strings.Join(chips, dim(" · "))
	left := faint("── ") + name
	// The folder gives way to the chips.
	if room := w - cellwidth(left) - cellwidth(right) - 8; room > 8 {
		left += "  " + faint(ansi.TruncateLeft(tildify(filepath.Dir(e.path)), max(0, len([]rune(tildify(filepath.Dir(e.path))))-room), "…"))
	}
	out := []string{rule(left, right)}

	body := e.view(w-1, max(1, h-2), focused)
	for _, l := range body {
		out = append(out, " "+l)
	}
	// The foot: the problem on the cursor's line, or the first one, and
	// where the cursor is.
	ln, col := e.cursorAt()
	where := dim(fmt.Sprintf("ln %d, col %d", ln, col))
	msg := ""
	if e.readOnly != "" && !e.loading {
		msg = paint(cYellow, e.readOnly)
	}
	var show *docDiag
	for i := range diags {
		if diags[i].line == ln-1 && (show == nil || show.warn && !diags[i].warn) {
			show = &diags[i]
		}
	}
	if show == nil && len(diags) > 0 {
		show = &diags[0]
	}
	if show != nil {
		mark, col := paint(cRed, "✗ "), cRed
		if show.warn {
			mark, col = paint(cYellow, "! "), cYellow
		}
		msg = mark + paint(col, fmt.Sprintf("line %d: ", show.line+1)) + paint(cSub, show.msg)
		if n := len(diags); n > 1 {
			msg += dim(fmt.Sprintf("  (%d problems)", n))
		}
	}
	out = append(out, spread("  "+msg, where+"  ", w))
	return out[:min(len(out), h)]
}

// editingDoc says whether a file in the memory view has the keys.
func (m *Model) editingDoc() bool {
	c := m.host
	return c != nil && c.memEdit && c.memEd != nil && m.paneFocus && m.mode == modeList && m.dialog == nil &&
		m.bar == nil && m.picker == nil && m.viewName(c) == "memory"
}

// docHint is the keys line while a file is being edited.
func (m *Model) docHint(e *docEditor, w int) string {
	pairs := []string{"ctrl+s", "save", "esc", "back to the list", "ctrl+z", "undo"}
	switch e.kind {
	case docJSON:
		pairs = append(pairs, "ctrl+t", "format")
	case docMarkdown:
		pairs = append(pairs, "tab", "indent", "ctrl+t", "tick a box", "ctrl+b", "bold")
	}
	pairs = append(pairs, "alt+↑↓", "move a line", "ctrl+g", "open in $EDITOR")
	return keysFit(w, pairs...)
}

// memoryKey handles the memory view's keys: ↑↓ pick a file, space edits it
// below, ctrl+g opens it in $EDITOR, x deletes it; while it's being edited,
// every key is the editor's. It reports whether it used the key.
func (m *Model) memoryKey(c *hostConn, k tea.KeyPressMsg, s string) (tea.Cmd, bool) {
	if m.viewName(c) != "memory" {
		c.memEdit = false
		return nil, false
	}
	if c.memEdit && c.memEd != nil {
		return m.docKey(c, k, s), true
	}
	if len(c.input) > 0 && !(s == "space" && strings.HasPrefix(c.sel, "mem:")) {
		return nil, false
	}
	files := m.memoryOf(c)
	cur := -1
	for i, f := range files {
		if "mem:"+f.Path == c.sel {
			cur = i
		}
	}
	step := map[string]int{"up": -1, "down": 1, "pgup": -10, "pgdown": 10}[s]
	if step != 0 && len(files) > 0 {
		n := 0
		if cur >= 0 && (s == "up" || s == "down") {
			n = roundMove(cur, step, len(files))
		} else if cur >= 0 {
			n = max(0, min(len(files)-1, cur+step))
		}
		c.sel = "mem:" + files[n].Path
		// The picked file is read in the background: draw it once it's in.
		if e := m.memDoc(c); e != nil {
			return e.reads.redraw(), true
		}
		return nil, true
	}
	path, ok := strings.CutPrefix(c.sel, "mem:")
	if !ok {
		return nil, false
	}
	switch s {
	case "f":
		// What's untidy, in Efficiency's findings.
		if r := c.memInfo; r == nil || len(r.Problems) == 0 {
			return nil, false
		}
		m.setView(placeEff)
		m.setEffPage(effFindings)
		if m.eff.view == nil {
			return m.effOpen(), true
		}
		return m.effLoad(false), true
	case "space", "right", "e":
		e := m.memDoc(c)
		if e == nil {
			return nil, true
		}
		if e.loading {
			m.flash("still reading "+filepath.Base(e.path)+"…", false)
			return e.reads.redraw(), true
		}
		if e.readOnly != "" {
			m.flash(e.readOnly, true)
			return nil, true
		}
		c.memEdit = true
		return nil, true
	case "ctrl+g":
		c.mem = nil // read it again when the editor closes
		return editorCmd(func() (string, error) {
			_ = os.MkdirAll(filepath.Dir(path), 0o755)
			return path, nil
		}, func(err error) tea.Msg { return dialogReload{err: err} }), true
	case "x", "delete", "ctrl+x":
		if f, ok := memFileAt(files, path); ok && !f.Missing {
			m.confirmForget(c, path)
		}
		return nil, true
	}
	return nil, false
}

// confirmForget asks before deleting the file at path, then deletes it in
// the background: a memory note also leaves MEMORY.md.
func (m *Model) confirmForget(c *hostConn, path string) {
	m.confirm = &confirmation{
		question: "Delete " + tildify(path) + "?",
		detail:   harnessName(string(sessionAgent(c))) + " stops reading it · a memory note also leaves MEMORY.md",
		onYes: func() tea.Cmd {
			forget := os.Remove
			if mr, ok := agent.As[agent.MemoryReader](sessionAgent(c)); ok {
				forget = mr.ForgetFile
			}
			return later(func() error { return forget(path) }, func(m *Model, err error) tea.Cmd {
				if err != nil {
					m.flash(err.Error(), true)
				}
				c.mem = nil
				if c.sel == "mem:"+path {
					c.sel = ""
				}
				if c.memEd != nil && c.memEd.path == path {
					c.memEd, c.memEdit = nil, false
				}
				return nil
			})
		},
	}
}

// docKey gives a key to the file being edited.
func (m *Model) docKey(c *hostConn, k tea.KeyPressMsg, s string) tea.Cmd {
	e := c.memEd
	switch s {
	case "esc":
		if _, _, sel := e.selection(); sel {
			e.anchor = -1
			return nil
		}
		m.leaveDoc(c)
		return nil
	case "ctrl+g":
		open := func(m *Model) tea.Cmd {
			c.memEdit, c.mem = false, nil
			return editFile(e.path)
		}
		if !e.dirty() {
			return open(m)
		}
		cmd, err := e.saveCmd(func(m *Model, err error) tea.Cmd {
			if err != nil {
				m.flash("couldn't save: "+err.Error(), true)
				return nil
			}
			return open(m)
		})
		if err != nil {
			m.flash("couldn't save: "+err.Error(), true)
		}
		return cmd
	}
	act, copied, _ := e.key(k, s)
	if copied != "" {
		m.copyText(copied)
	}
	switch act {
	case "save":
		return m.saveDoc(c, nil)
	case "format":
		text, err := formatJSON(string(e.buf), e.indent)
		switch {
		case err != nil:
			m.flash("it can't be formatted until it's valid JSON", true)
		case text != string(e.buf):
			ln, _ := e.cursorAt()
			e.change([]rune(text), 0, false)
			// Back to the same line, near enough.
			for i, n := 0, 1; i < len(e.buf) && n < ln; i++ {
				if e.buf[i] == '\n' {
					n++
					e.pos = i + 1
				}
			}
		}
	}
	return nil
}

// saveDoc saves the file being edited, asking first when it changed on
// disk meanwhile or isn't valid JSON, then runs then.
func (m *Model) saveDoc(c *hostConn, then func()) tea.Cmd {
	e := c.memEd
	do := func() tea.Cmd {
		cmd, err := e.saveCmd(func(m *Model, err error) tea.Cmd {
			if err != nil {
				m.flash("couldn't save: "+err.Error(), true)
				return nil
			}
			c.mem = nil
			m.flash("saved "+tildify(e.path), false)
			if then != nil {
				then()
			}
			return nil
		})
		if err != nil {
			m.flash("couldn't save: "+err.Error(), true)
		}
		return cmd
	}
	var bad *docDiag
	for i, d := range e.problems() {
		if !d.warn && e.kind == docJSON {
			bad = &e.problems()[i]
		}
	}
	switch {
	case e.stale:
		m.confirm = &confirmation{question: filepath.Base(e.path) + " changed on disk since you opened it.", detail: "y saves yours over it", onYes: do}
	case bad != nil:
		m.confirm = &confirmation{question: fmt.Sprintf("It isn't valid JSON (line %d: %s). Save anyway?", bad.line+1, bad.msg),
			detail: harnessName(string(sessionAgent(c))) + " can't read it until it's fixed", onYes: do}
	default:
		return do()
	}
	return nil
}

// leaveDoc hands the keys back to the list, asking about unsaved changes.
func (m *Model) leaveDoc(c *hostConn) {
	e := c.memEd
	leave := func() { c.memEdit = false }
	if !e.dirty() {
		leave()
		return
	}
	m.confirm = &confirmation{
		question: "Save your changes to " + filepath.Base(e.path) + "?",
		detail:   "n keeps editing",
		onYes: func() tea.Cmd {
			return m.saveDoc(c, leave)
		},
		bangText: "throw them away",
		onBang: func() tea.Cmd {
			e.buf, e.anchor = []rune(e.saved), -1
			e.pos = min(e.pos, len(e.buf))
			e.bump()
			leave()
			return nil
		},
	}
}

func fileSize(n int64) string {
	switch {
	case n < 1<<10:
		return fmt.Sprintf("%d B", n)
	case n < 1<<20:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
}
