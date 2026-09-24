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
	w := min(o.Width, o.rowCap())
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
				add("", pad+paint(cGreen, "▏")+dim(fmt.Sprintf("%5d ", i+1))+diffText(nil, nil, l, cSub, bw), "")
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
				switch l[0] {
				case '+':
					out = append(out, Line{Text: row(bgAdd, pad+dim(fmt.Sprintf("%5d ", newN))+paint(cGreen, "+")+" "+diffText(nil, nil, l[1:], cText, bw), "", o.Width, w)})
					newN++
				case '-':
					out = append(out, Line{Text: row(bgDel, pad+dim(fmt.Sprintf("%5d ", oldN))+paint(cRed, "−")+" "+diffText(nil, nil, l[1:], cText, bw), "", o.Width, w)})
					oldN++
				default:
					out = append(out, Line{Text: row(bgWell, pad+dim(fmt.Sprintf("%5d ", newN))+"  "+diffText(nil, nil, l[1:], cSub, bw), "", o.Width, w)})
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
				out = append(out, Line{Text: row(b, "       "+diffText(nil, nil, cleanOutput(l), cText, w-10), "", o.Width, w)})
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
