package ui

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/agtop/internal/convo"
)

// --- memory view ---

// The memory view lists what Claude reads for the session's project: its
// auto memory, the CLAUDE.md files and rules, settings, and the agents,
// skills, commands and output styles on disk (plugins' are left out; an
// update would overwrite them). ↑↓ pick a file, shown in the editor in the
// bottom half; enter edits it there, ctrl+g in $EDITOR, x deletes it.

// memFile is one file Claude reads, or one it would read if it existed.
type memFile struct {
	Group   string
	Path    string
	Name    string
	About   string
	Kind    string // a memory note's type: user, feedback, project, reference
	Missing bool
	Size    int64
	Mod     time.Time
}

var memGroups = []string{"Auto memory", "Instructions", "Settings", "Agents", "Skills", "Commands", "Output styles"}

// memoryOf is memoryFiles for the session, read again at most every two
// seconds, or at once after an edit.
func (m *Model) memoryOf(c *hostConn) []memFile {
	if c.mem != nil && time.Since(c.memAt) < 2*time.Second {
		return c.mem
	}
	cfg, cwd := m.store.Config.ActiveAccount().ConfigDir, c.sess.Info.Cwd
	if a := m.agentByKey(c.key); a != nil {
		cfg, cwd = a.Acct.ConfigDir, firstNonEmpty(cwd, a.Cwd)
	}
	proj := ""
	if c.path != "" {
		proj = filepath.Dir(c.path)
	} else if cwd != "" {
		proj = filepath.Join(cfg, "projects", projectSlug(cwd))
	}
	c.mem, c.memAt = memoryFiles(cfg, cwd, proj), time.Now()
	return c.mem
}

var slugUnsafe = regexp.MustCompile(`[^A-Za-z0-9]`)

// projectSlug is the folder Claude Code keeps a project's transcripts and
// memory in, under projects/.
func projectSlug(cwd string) string { return slugUnsafe.ReplaceAllString(cwd, "-") }

// memoryFiles lists the files, grouped in memGroups' order. The user's
// CLAUDE.md and settings, and the project's CLAUDE.md, are listed even when
// they don't exist yet, so they can be written.
func memoryFiles(cfg, cwd, proj string) []memFile {
	var out []memFile
	seen := map[string]bool{}
	add := func(group, path, name, about string, always bool) {
		if path == "" || seen[path] {
			return
		}
		st, err := os.Stat(path)
		if (err != nil || st.IsDir()) && !always {
			return
		}
		seen[path] = true
		f := memFile{Group: group, Path: path, Name: name, About: about, Missing: err != nil}
		if err == nil {
			f.Size, f.Mod = st.Size(), st.ModTime()
		}
		out = append(out, f)
	}
	// The folders a session in cwd reads from, nearest first, up to but not
	// including home, whose .claude is the user's own.
	home, _ := os.UserHomeDir()
	var dirs []string
	for d := cwd; d != "" && d != "/" && d != "." && d != home; d = filepath.Dir(d) {
		dirs = append(dirs, d)
	}
	name := func(p string) string {
		if cwd != "" {
			if rel, err := filepath.Rel(cwd, p); err == nil && !strings.HasPrefix(rel, "..") {
				return rel
			}
		}
		return tildify(p)
	}

	if proj != "" {
		dir := filepath.Join(proj, "memory")
		add("Auto memory", filepath.Join(dir, "MEMORY.md"), "MEMORY.md", "the index · loaded into every session", false)
		notes, _ := filepath.Glob(filepath.Join(dir, "*.md"))
		sort.Strings(notes)
		for _, p := range notes {
			fm := noteMeta(p)
			add("Auto memory", p, firstNonEmpty(fm["name"], strings.TrimSuffix(filepath.Base(p), ".md")), fm["description"], false)
			if n := len(out) - 1; n >= 0 && out[n].Path == p {
				out[n].Kind = fm["type"]
			}
		}
	}

	add("Instructions", filepath.Join(cfg, "CLAUDE.md"), tildify(filepath.Join(cfg, "CLAUDE.md")), "yours · every project", true)
	if cwd != "" {
		if !exists(filepath.Join(cwd, "CLAUDE.md")) && !exists(filepath.Join(cwd, ".claude", "CLAUDE.md")) {
			add("Instructions", filepath.Join(cwd, "CLAUDE.md"), "CLAUDE.md", "this project's · shared through git", true)
		}
	}
	for _, d := range dirs {
		add("Instructions", filepath.Join(d, "CLAUDE.md"), name(filepath.Join(d, "CLAUDE.md")), "project · shared through git", false)
		add("Instructions", filepath.Join(d, ".claude", "CLAUDE.md"), name(filepath.Join(d, ".claude", "CLAUDE.md")), "project · shared through git", false)
		add("Instructions", filepath.Join(d, "CLAUDE.local.md"), name(filepath.Join(d, "CLAUDE.local.md")), "project · just yours", false)
	}
	for _, root := range append([]string{cfg}, dotClaude(dirs)...) {
		for _, p := range mdTree(filepath.Join(root, "rules")) {
			fm := noteMeta(p)
			add("Instructions", p, name(p), firstNonEmpty(fm["description"], "rule"), false)
		}
	}

	add("Settings", filepath.Join(cfg, "settings.json"), tildify(filepath.Join(cfg, "settings.json")), "yours · every project", true)
	add("Settings", filepath.Join(cfg, "settings.local.json"), tildify(filepath.Join(cfg, "settings.local.json")), "yours · this machine", false)
	if cwd != "" {
		add("Settings", filepath.Join(cwd, ".claude", "settings.json"), filepath.Join(".claude", "settings.json"), "project · shared through git", false)
		add("Settings", filepath.Join(cwd, ".claude", "settings.local.json"), filepath.Join(".claude", "settings.local.json"), "project · just yours", false)
		add("Settings", filepath.Join(cwd, ".mcp.json"), ".mcp.json", "project MCP servers", false)
	}
	add("Settings", claudeJSON(cfg), tildify(claudeJSON(cfg)), "Claude Code's own state · MCP servers, trust, per-project history", false)

	for _, root := range append(dotClaude(dirs), cfg) {
		for _, p := range mdTree(filepath.Join(root, "agents")) {
			fm := noteMeta(p)
			add("Agents", p, firstNonEmpty(fm["name"], strings.TrimSuffix(filepath.Base(p), ".md")), fm["description"], false)
		}
		skills, _ := filepath.Glob(filepath.Join(root, "skills", "*", "SKILL.md"))
		for _, p := range skills {
			fm := noteMeta(p)
			add("Skills", p, firstNonEmpty(fm["name"], filepath.Base(filepath.Dir(p))), fm["description"], false)
		}
		for _, p := range mdTree(filepath.Join(root, "commands")) {
			rel, _ := filepath.Rel(filepath.Join(root, "commands"), strings.TrimSuffix(p, ".md"))
			fm := noteMeta(p)
			add("Commands", p, "/"+strings.ReplaceAll(rel, string(filepath.Separator), ":"), fm["description"], false)
		}
		for _, p := range mdTree(filepath.Join(root, "output-styles")) {
			fm := noteMeta(p)
			add("Output styles", p, firstNonEmpty(fm["name"], strings.TrimSuffix(filepath.Base(p), ".md")), fm["description"], false)
		}
	}
	// Grouped, keeping each group's own order.
	rank := map[string]int{}
	for i, g := range memGroups {
		rank[g] = i
	}
	sort.SliceStable(out, func(i, j int) bool { return rank[out[i].Group] < rank[out[j].Group] })
	return out
}

// claudeJSON is Claude Code's state file: ~/.claude.json for the default
// config folder, or .claude.json inside any other.
func claudeJSON(cfg string) string {
	if home, _ := os.UserHomeDir(); home != "" && cfg == filepath.Join(home, ".claude") {
		return filepath.Join(home, ".claude.json")
	}
	return filepath.Join(cfg, ".claude.json")
}

func dotClaude(dirs []string) []string {
	out := make([]string, len(dirs))
	for i, d := range dirs {
		out[i] = filepath.Join(d, ".claude")
	}
	return out
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// mdTree is every .md file under dir, in order.
func mdTree(dir string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".md") {
			out = append(out, p)
		}
		return nil
	})
	return out
}

// noteMeta reads name, description and type from a markdown file's
// frontmatter, at any depth (a memory note keeps its type under metadata).
func noteMeta(path string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for n := 0; sc.Scan() && n < 40; n++ {
		line := sc.Text()
		if strings.TrimSpace(line) == "---" {
			if n == 0 {
				continue
			}
			break
		}
		if n == 0 {
			return out // no frontmatter
		}
		k, v, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		switch k = strings.TrimSpace(k); k {
		case "name", "description", "type":
			if out[k] == "" {
				out[k] = strings.Trim(strings.TrimSpace(v), `"'`)
			}
		}
	}
	return out
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
		line(spread("  "+paint(cSub+bold, "Memory")+"   "+dim("what Claude reads for this project"), dim(fmt.Sprintf("%d files", len(files)))+"  ", w), "")
		line("  "+faint(strings.Repeat("─", max(0, w-4))), "")
		rows := memRows(files, c.sel, w, o)
		sel := 0
		for i, r := range rows {
			if r.Ref == c.sel && c.sel != "" {
				sel = i
				break
			}
		}
		room := max(1, listH-2)
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

// memRows is a row for each file, under its group's name.
func memRows(files []memFile, picked string, w int, o convo.Options) []convo.Line {
	var out []convo.Line
	group := ""
	for _, f := range files {
		if f.Group != group {
			group = f.Group
			head := "  " + paint(cText+bold, group)
			if group == "Auto memory" {
				head += "  " + faint(tildify(filepath.Dir(f.Path)))
			}
			out = append(out, convo.Line{Text: fit(head, w)})
		}
		ref := "mem:" + f.Path
		meta := "not written yet"
		if !f.Missing {
			meta = fileSize(f.Size) + " · " + dur(o.Now.Sub(f.Mod)) + " ago"
		}
		mark := paint(cBlue, "◆ ")
		if f.Missing {
			mark = faint("◇ ")
		}
		left := "    " + mark + paint(cText, f.Name)
		if f.Kind != "" {
			left += "  " + paint(cSub, f.Kind)
		}
		if f.About != "" {
			room := w - cellwidth(left) - cellwidth(meta) - 8
			if room > 12 {
				left += "  " + dim(ansi.Truncate(oneLine(f.About), room, "…"))
			}
		}
		r := spread(left, dim(meta)+"  ", w-1)
		if ref == picked {
			r = picked1(r, w, o.Focused)
		}
		out = append(out, convo.Line{Text: fit(r, w), Ref: ref})
	}
	if len(files) == 0 {
		out = append(out, convo.Line{Text: fit("    "+dim("Nothing found for this project."), w)})
	}
	return out
}

func cellwidth(s string) int { return ansi.StringWidth(s) }

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
			out[h/2] = "    " + dim("Pick a file above to see it here · enter edits it")
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
	if e.readOnly != "" {
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

// memoryKey handles the memory view's keys: ↑↓ pick a file, enter edits it
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
	if len(c.input) > 0 {
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
		return nil, true
	}
	path, ok := strings.CutPrefix(c.sel, "mem:")
	if !ok {
		return nil, false
	}
	switch s {
	case "enter", "right", "e":
		e := m.memDoc(c)
		if e == nil {
			return nil, true
		}
		if e.readOnly != "" {
			m.flash(e.readOnly, true)
			return nil, true
		}
		c.memEdit = true
		return nil, true
	case "ctrl+g":
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		c.mem = nil // read it again when the editor closes
		return editFile(path), true
	case "x", "delete", "ctrl+x":
		if !exists(path) {
			return nil, true
		}
		m.confirm = &confirmation{
			question: "Delete " + tildify(path) + "?",
			detail:   "Claude stops reading it · a memory note also leaves MEMORY.md",
			onYes: func() tea.Cmd {
				if err := forgetFile(path); err != nil {
					m.flash(err.Error(), true)
				}
				c.mem, c.sel, c.memEd = nil, "", nil
				return nil
			},
		}
		return nil, true
	}
	return nil, false
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
		if e.dirty() {
			if err := e.save(); err != nil {
				m.flash("couldn't save: "+err.Error(), true)
				return nil
			}
		}
		c.memEdit, c.mem = false, nil
		return editFile(e.path)
	}
	act, copied, _ := e.key(k, s)
	if copied != "" {
		m.copyText(copied)
	}
	switch act {
	case "save":
		m.saveDoc(c, nil)
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
func (m *Model) saveDoc(c *hostConn, then func()) {
	e := c.memEd
	do := func() tea.Cmd {
		if err := e.save(); err != nil {
			m.flash("couldn't save: "+err.Error(), true)
			return nil
		}
		c.mem = nil
		m.flash("saved "+tildify(e.path), false)
		if then != nil {
			then()
		}
		return nil
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
			detail: "Claude Code can't read it until it's fixed", onYes: do}
	default:
		do()
	}
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
			m.saveDoc(c, leave)
			return nil
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

// forgetFile deletes a file, and a memory note's line in its MEMORY.md.
func forgetFile(path string) error {
	if err := os.Remove(path); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	index := filepath.Join(dir, "MEMORY.md")
	if filepath.Base(dir) != "memory" || path == index {
		return nil
	}
	b, err := os.ReadFile(index)
	if err != nil {
		return nil
	}
	link := "(" + filepath.Base(path) + ")"
	var keep []string
	for _, l := range strings.Split(string(b), "\n") {
		if !strings.Contains(l, link) {
			keep = append(keep, l)
		}
	}
	return os.WriteFile(index, []byte(strings.Join(keep, "\n")), 0o644)
}
