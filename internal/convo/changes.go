package convo

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/0xdeafcafe/agtop/internal/headless"
)

// FileChange is everything this session did to one file.
type FileChange struct {
	Path    string
	Add     int
	Del     int
	New     bool
	Turns   []int
	Patches []headless.Patch
	From    []string // for each patch, the step that made it ("t3:s:toolu_…")
	Content string   // a new file's content
	NewFrom string   // the step that created it
}

// Changes collects the session's edits by file, in the order files were
// first touched.
func (s *Session) Changes() []*FileChange {
	// Worked out again only when a step changed.
	if s.changes != nil && s.changesVer == s.stepVer {
		return s.changes
	}
	s.changes, s.changesVer = s.changesNow(), s.stepVer
	return s.changes
}

func (s *Session) changesNow() []*FileChange {
	byPath := map[string]*FileChange{}
	var order []*FileChange
	for _, t := range s.Turns {
		for _, it := range t.Items {
			if it.Kind != KStep {
				continue
			}
			st := it.Step
			switch st.Tool {
			case "Edit", "MultiEdit", "Write":
			default:
				continue
			}
			if st.Status != OK {
				continue
			}
			path := readInput(st.Input).str("file_path")
			fc := byPath[path]
			if fc == nil {
				fc = &FileChange{Path: path}
				byPath[path] = fc
				order = append(order, fc)
			}
			if n := len(fc.Turns); n == 0 || fc.Turns[n-1] != t.N {
				fc.Turns = append(fc.Turns, t.N)
			}
			var r struct {
				Type    string `json:"type"`
				Content string `json:"content"`
			}
			_ = json.Unmarshal(st.Result, &r)
			stepRef := fmt.Sprintf("t%d:s:%s", t.N, st.ID)
			if r.Type == "create" {
				fc.New, fc.Content, fc.NewFrom = true, r.Content, stepRef
				fc.Add += countLines(r.Content)
				continue
			}
			for _, p := range headless.Patches(st.Result) {
				fc.Patches = append(fc.Patches, p)
				fc.From = append(fc.From, stepRef)
				for _, l := range p.Lines {
					switch {
					case strings.HasPrefix(l, "+"):
						fc.Add++
					case strings.HasPrefix(l, "-"):
						fc.Del++
					}
				}
			}
		}
	}
	return order
}

// TreeFile is one file git reports as changed in the working tree.
type TreeFile struct {
	Path      string
	Add, Del  int
	Binary    bool
	Untracked bool
	Ours      bool // this session edited it
}

// Tree is what git sees in dir: changed and untracked files against HEAD.
type Tree struct {
	Dir   string
	Head  string
	Root  string
	Files []TreeFile
	Err   string
	at    time.Time
}

var trees = struct {
	sync.Mutex
	m       map[string]*Tree
	reading map[string]bool
}{m: map[string]*Tree{}, reading: map[string]bool{}}

// WorkingTree is the last reading of git for dir. It never runs git while
// a frame is drawn: a reading older than 5 seconds is refreshed in the
// background, and the next frame picks it up. The first call returns an
// empty tree marked as reading.
func WorkingTree(dir string) *Tree {
	trees.Lock()
	defer trees.Unlock()
	t := trees.m[dir]
	if (t == nil || time.Since(t.at) > 5*time.Second) && !trees.reading[dir] {
		trees.reading[dir] = true
		go func() {
			nt := readTree(dir)
			trees.Lock()
			trees.m[dir], trees.reading[dir] = nt, false
			trees.Unlock()
		}()
	}
	if t == nil {
		return &Tree{Dir: dir, Err: "reading git…"}
	}
	return t
}

// git runs git in dir, giving up after 10 seconds.
func git(dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).Output()
}

func readTree(dir string) *Tree {
	t := &Tree{Dir: dir, at: time.Now()}
	top, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		t.Err = "not a git repository"
		return t
	}
	root := strings.TrimSpace(string(top))
	t.Root = root
	base := "HEAD"
	if head, err := git(dir, "rev-parse", "--short", "HEAD"); err == nil {
		t.Head = strings.TrimSpace(string(head))
	} else {
		t.Head, base = "no commits yet", "--cached" // staged files are all there is
	}
	// -z keeps odd paths unquoted; no renames, so each path is a real file.
	out, _ := git(root, "diff", base, "--numstat", "-z", "--no-renames")
	for _, rec := range strings.Split(string(out), "\x00") {
		f := strings.SplitN(rec, "\t", 3)
		if len(f) != 3 || f[2] == "" {
			continue
		}
		tf := TreeFile{Path: filepath.Join(root, f[2])}
		if f[0] == "-" {
			tf.Binary = true
		} else {
			tf.Add, _ = strconv.Atoi(f[0])
			tf.Del, _ = strconv.Atoi(f[1])
		}
		t.Files = append(t.Files, tf)
	}
	// From the top, so a session in a subfolder still sees the whole repo.
	untracked, _ := git(root, "ls-files", "--others", "--exclude-standard", "-z")
	for _, p := range strings.Split(string(untracked), "\x00") {
		if p != "" {
			t.Files = append(t.Files, TreeFile{Path: filepath.Join(root, p), Untracked: true})
		}
	}
	sort.Slice(t.Files, func(i, j int) bool { return t.Files[i].Path < t.Files[j].Path })
	return t
}

// realPath resolves symlinks (macOS's /tmp is /private/tmp), so the same
// file matches however it was named.
func realPath(p string) string {
	realPaths.Lock()
	defer realPaths.Unlock()
	if r, ok := realPaths.m[p]; ok {
		return r
	}
	r := p
	if x, err := filepath.EvalSymlinks(p); err == nil {
		r = x
	} else if x, err := filepath.EvalSymlinks(filepath.Dir(p)); err == nil {
		r = filepath.Join(x, filepath.Base(p))
	}
	realPaths.m[p] = r
	return r
}

var realPaths = struct {
	sync.Mutex
	m map[string]string
}{m: map[string]string{}}

// ChangesView draws the session's own edits, then the working tree as git
// sees it, with the files this session didn't touch marked as such.
func (s *Session) ChangesView(o Options) []Line {
	w := min(o.Width, capRow)
	d := drawer{s: s, t: &Turn{}, o: o, cw: w}
	var out []Line
	add := func(ref, left, right string) {
		if ref != "" && ref == o.Selected {
			bar := faint("▍")
			b := bgSelU
			if o.Focused {
				bar, b = paint(cOrange, "▍"), bgSel
			}
			left = bar + strings.TrimPrefix(left, " ")
			out = append(out, Line{Text: row(b, left, right, o.Width, w), Ref: ref})
			return
		}
		out = append(out, Line{Text: row("", left, right, o.Width, w), Ref: ref})
	}
	rule := func(title, meta string) {
		if len(out) > 0 {
			add("", "", "")
		}
		head := "  " + faint("▾") + " " + paint(cSub+bold, title)
		if meta != "" {
			head += "  " + dim(meta)
		}
		add("", head+" "+faint(strings.Repeat("─", max(0, w-len([]rune(stripANSI(head)))-3))), "")
	}

	changes := s.Changes()
	ours := map[string]bool{}
	adds, dels := 0, 0
	for _, fc := range changes {
		ours[realPath(fc.Path)] = true
		adds, dels = adds+fc.Add, dels+fc.Del
	}
	meta := "nothing edited yet"
	if len(changes) > 0 {
		meta = fmt.Sprintf("%s · +%d −%d", plural(len(changes), "file"), adds, dels)
		seen := 0
		for _, fc := range changes {
			if o.Marks[fc.Path] {
				seen++
			}
		}
		meta += fmt.Sprintf(" · %d of %d reviewed · alt+r marks one", seen, len(changes))
	}
	rule("This session", meta)
	for _, fc := range changes {
		ref := "chg:" + fc.Path
		open := o.Open[ref]
		arrow := faint("▸")
		if open {
			arrow = faint("▾")
		}
		counts := paint(cGreen, fmt.Sprintf("+%d", fc.Add)) + " " + paint(cRed, fmt.Sprintf("−%d", fc.Del))
		if fc.New {
			counts = paint(cGreen, fmt.Sprintf("new · %d lines", fc.Add))
		}
		var turns []string
		for _, n := range fc.Turns {
			turns = append(turns, fmt.Sprintf("#%d", n))
		}
		name := text(d.rel(fc.Path))
		mark := "  "
		if o.Marks[fc.Path] {
			mark, name = paint(cGreen, "✓ "), dim(d.rel(fc.Path))
		}
		add(ref, "  "+arrow+" "+mark+name, counts+"   "+dim(strings.Join(turns, " ")))
		if !open {
			continue
		}
		pad := strings.Repeat(" ", 7)
		bw := w - 16
		// Each hunk under a row naming the turn that made it; enter on it
		// goes to that step in the conversation.
		hunk := func(from, what string) {
			turn, _, _ := strings.Cut(from, ":")
			add("jump:"+from, pad+paint(cBlue, "@ ")+dim(what)+"  "+sub("#"+strings.TrimPrefix(turn, "t")), dim("enter goes to the step"))
		}
		if fc.New {
			hunk(fc.NewFrom, "created")
			for i, l := range strings.Split(strings.TrimRight(fc.Content, "\n"), "\n") {
				if i >= 40 && !o.Verbose {
					add("", pad+dim("… ctrl+o shows the rest"), "")
					break
				}
				add("", pad+paint(cGreen, "▏")+dim(fmt.Sprintf("%5d ", i+1))+sub(truncateCells(expandTabs(l), bw)), "")
			}
			// Edits made after it was created follow.
		}
		for pi, p := range fc.Patches {
			from := ""
			if pi < len(fc.From) {
				from = fc.From[pi]
			}
			hunk(from, fmt.Sprintf("line %d", p.NewStart))
			oldN, newN := p.OldStart, p.NewStart
			for _, l := range p.Lines {
				if l == "" {
					l = " "
				}
				if l[0] == '\\' {
					continue // "\ No newline at end of file"
				}
				body := truncateCells(expandTabs(l[1:]), bw)
				switch l[0] {
				case '+':
					out = append(out, Line{Text: row(bgAdd, pad+dim(fmt.Sprintf("%5d ", newN))+paint(cGreen, "+")+" "+text(body), "", o.Width, w)})
					newN++
				case '-':
					out = append(out, Line{Text: row(bgDel, pad+dim(fmt.Sprintf("%5d ", oldN))+paint(cRed, "−")+" "+text(body), "", o.Width, w)})
					oldN++
				default:
					out = append(out, Line{Text: row(bgWell, pad+dim(fmt.Sprintf("%5d ", newN))+"  "+sub(body), "", o.Width, w)})
					oldN++
					newN++
				}
			}
		}
	}

	dir := firstNonEmpty(s.Info.Cwd, s.Cwd)
	if dir == "" {
		return out
	}
	tree := WorkingTree(dir)
	if tree.Err != "" {
		rule("Working tree", tree.Err)
		return out
	}
	others := 0
	for i := range tree.Files {
		if ours[realPath(tree.Files[i].Path)] {
			tree.Files[i].Ours = true
		} else {
			others++
		}
	}
	meta = fmt.Sprintf("%s changed against %s", plural(len(tree.Files), "file"), tree.Head)
	if others > 0 {
		meta += fmt.Sprintf(" · %d not from this session", others)
	}
	rule("Working tree", meta)
	if len(tree.Files) == 0 {
		add("", "    "+dim("clean"), "")
	}
	for i, f := range tree.Files {
		if i >= 60 && !o.Verbose {
			add("", "    "+dim(fmt.Sprintf("… %d more · ctrl+o shows all", len(tree.Files)-i)), "")
			break
		}
		counts := paint(cGreen, fmt.Sprintf("+%d", f.Add)) + " " + paint(cRed, fmt.Sprintf("−%d", f.Del))
		switch {
		case f.Untracked:
			counts = paint(cGreen, "new")
		case f.Binary:
			counts = dim("binary")
		}
		who := dim("not from this session")
		mark := dim("·")
		if f.Ours {
			who, mark = paint(cOrange, "this session"), paint(cOrange, "●")
		}
		tref := "tree:" + f.Path
		arrow := faint("▸")
		if o.Open[tref] {
			arrow = faint("▾")
		}
		add(tref, "  "+arrow+" "+mark+" "+text(d.rel(f.Path)), counts+"   "+who)
		if o.Open[tref] {
			for _, l := range treeDiff(tree, f) {
				b := bgWell
				switch {
				case strings.HasPrefix(l, "+"):
					b = bgAdd
				case strings.HasPrefix(l, "-"):
					b = bgDel
				case strings.HasPrefix(l, "@@"):
					out = append(out, Line{Text: row("", "       "+paint(cBlue, truncateCells(l, w-10)), "", o.Width, w)})
					continue
				}
				out = append(out, Line{Text: row(b, "       "+text(truncateCells(expandTabs(cleanOutput(l)), w-10)), "", o.Width, w)})
			}
		}
	}
	return out
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}

// RecentEdits draws the session's latest edits as diff blocks, newest
// first, for the rail beside the conversation. It stops at h lines. Each
// edit's block is drawn once; only how long ago it was changes per frame.
func (s *Session) RecentEdits(o Options, h int) []Line {
	w := o.Width
	d := drawer{s: s, t: &Turn{}, o: o, cw: w}
	var out []Line
	add := func(b, text string) {
		if len(out) < h {
			out = append(out, Line{Text: row(b, text, "", w, w)})
		}
	}
	edits := s.edits()
	head := " " + paint(cSub+bold, "Recent changes")
	if len(edits) > 0 {
		head += "  " + dim(plural(len(edits), "edit"))
	}
	add("", head)
	add("", " "+faint(strings.Repeat("─", max(0, w-2))))
	if len(edits) == 0 {
		add("", " "+dim("no edits yet"))
		return out
	}
	for i := len(edits) - 1; i >= 0 && len(out) < h; i-- {
		b := s.railBlock(&d, edits[i], w)
		ago := ""
		if st := edits[i].st; !st.End.IsZero() {
			ago = dim(" · " + dur(o.Now.Sub(st.End)) + " ago")
		}
		out = append(out, b.head)
		add(bgWell, b.counts+ago)
		for _, l := range b.body {
			if len(out) >= h {
				break
			}
			out = append(out, l)
		}
	}
	return out[:min(len(out), h)]
}

type edit struct {
	st   *Step
	turn int
}

// edits are the session's successful edits, oldest first, found again
// only when a step has changed.
func (s *Session) edits() []edit {
	if s.editsVer == s.stepVer+1 {
		return s.editList
	}
	s.editList = s.editList[:0]
	for _, t := range s.Turns {
		for _, it := range t.Items {
			if it.Kind == KStep && glyphFor(it.Step.Tool) == "✎" && it.Step.Status == OK {
				s.editList = append(s.editList, edit{it.Step, t.N})
			}
		}
	}
	s.editsVer = s.stepVer + 1
	return s.editList
}

// railBlock is one edit as the rail draws it, all but how long ago.
type railBlock struct {
	key    railKey
	head   Line
	counts string
	body   []Line
}

type railKey struct {
	w, turn, res int
	end          time.Time
	bases        string
}

func (s *Session) railBlock(d *drawer, e edit, w int) railBlock {
	st := e.st
	k := railKey{w: w, turn: e.turn, res: len(st.Result), end: st.End, bases: s.Info.Cwd + "|" + s.Cwd}
	if b, ok := s.rail[st]; ok && b.key == k {
		return b
	}
	var body []Line
	add := func(bg, text string) { body = append(body, Line{Text: row(bg, text, "", w, w)}) }
	path := d.rel(readInput(st.Input).str("file_path"))
	b := railBlock{key: k, counts: "   " + d.summary(st) + dim(fmt.Sprintf("  #%d", e.turn)),
		head: Line{Text: row(bgWell, " "+paint(cBlue, "✎ ")+text(truncateCells(path, w-4)), "", w, w)}}
	bw := w - 4
	var r struct {
		Type    string `json:"type"`
		Content string `json:"content"`
	}
	_ = json.Unmarshal(st.Result, &r)
	shown := 0
	if r.Type == "create" {
		for _, l := range strings.Split(strings.TrimRight(r.Content, "\n"), "\n") {
			if shown == 8 {
				add("", "  "+dim("…"))
				break
			}
			add(bgAdd, " "+paint(cGreen, "+")+" "+text(truncateCells(expandTabs(l), bw)))
			shown++
		}
	}
patches:
	for _, p := range headless.Patches(st.Result) {
		for _, l := range p.Lines {
			if l == "" || l[0] == ' ' || l[0] == '\\' {
				continue // only what changed; the rail is narrow
			}
			if shown == 8 {
				add("", "  "+dim("… the changes view has the rest"))
				break patches
			}
			body := truncateCells(expandTabs(l[1:]), bw)
			if l[0] == '+' {
				add(bgAdd, " "+paint(cGreen, "+")+" "+text(body))
			} else {
				add(bgDel, " "+paint(cRed, "−")+" "+text(body))
			}
			shown++
		}
	}
	add("", "")
	b.body = body
	if s.rail == nil {
		s.rail = map[*Step]railBlock{}
	}
	s.rail[st] = b
	return b
}

var diffs = struct {
	sync.Mutex
	m       map[string][]string
	reading map[string]bool
}{m: map[string][]string{}, reading: map[string]bool{}}

// treeDiff is git's own diff of one working-tree file against HEAD (or the
// file itself when it's untracked), read in the background: the first call
// says so and the next frame has it. It is read again with each new
// reading of the tree.
func treeDiff(t *Tree, f TreeFile) []string {
	key := fmt.Sprint(t.Root, "\x00", f.Path, "\x00", t.at.UnixNano())
	diffs.Lock()
	defer diffs.Unlock()
	if d, ok := diffs.m[key]; ok {
		return d
	}
	if !diffs.reading[key] {
		diffs.reading[key] = true
		go func() {
			var out []byte
			if f.Untracked {
				out, _ = git(t.Root, "diff", "--no-index", "--", "/dev/null", f.Path)
			} else {
				out, _ = git(t.Root, "diff", "HEAD", "--", f.Path)
			}
			var lines []string
			for _, l := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
				// The header lines say what the row already says.
				if strings.HasPrefix(l, "diff --git") || strings.HasPrefix(l, "index ") || strings.HasPrefix(l, "--- ") ||
					strings.HasPrefix(l, "+++ ") || strings.HasPrefix(l, "new file") || strings.HasPrefix(l, "\\") {
					continue
				}
				lines = append(lines, l)
			}
			if len(lines) > 400 {
				lines = append(lines[:400], fmt.Sprintf("… %d more lines", len(lines)-400))
			}
			diffs.Lock()
			diffs.m[key], diffs.reading[key] = lines, false
			diffs.Unlock()
		}()
	}
	return []string{"reading the diff…"}
}
