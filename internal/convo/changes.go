package convo

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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
	Content string // a new file's content
}

// Changes collects the session's edits by file, in the order files were
// first touched.
func (s *Session) Changes() []*FileChange {
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
			if r.Type == "create" {
				fc.New, fc.Content = true, r.Content
				fc.Add += countLines(r.Content)
				continue
			}
			for _, p := range headless.Patches(st.Result) {
				fc.Patches = append(fc.Patches, p)
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
	Untracked bool
	Ours      bool // this session edited it
}

// Tree is what git sees in dir: changed and untracked files against HEAD,
// cached for a few seconds because the view redraws often.
type Tree struct {
	Dir   string
	Head  string
	Files []TreeFile
	Err   string
	at    time.Time
}

var trees = map[string]*Tree{}

// WorkingTree reads git for dir, at most every 5 seconds.
func WorkingTree(dir string) *Tree {
	if t := trees[dir]; t != nil && time.Since(t.at) < 5*time.Second {
		return t
	}
	t := &Tree{Dir: dir, at: time.Now()}
	trees[dir] = t
	head, err := exec.Command("git", "-C", dir, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		t.Err = "not a git repository"
		return t
	}
	t.Head = strings.TrimSpace(string(head))
	top, _ := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	root := strings.TrimSpace(string(top))
	out, _ := exec.Command("git", "-C", dir, "diff", "HEAD", "--numstat").Output()
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		f := strings.SplitN(sc.Text(), "\t", 3)
		if len(f) != 3 {
			continue
		}
		a, _ := strconv.Atoi(f[0])
		d, _ := strconv.Atoi(f[1])
		t.Files = append(t.Files, TreeFile{Path: filepath.Join(root, f[2]), Add: a, Del: d})
	}
	untracked, _ := exec.Command("git", "-C", dir, "ls-files", "--others", "--exclude-standard", "--full-name").Output()
	for _, p := range strings.Split(strings.TrimSpace(string(untracked)), "\n") {
		if p != "" {
			t.Files = append(t.Files, TreeFile{Path: filepath.Join(root, p), Untracked: true})
		}
	}
	sort.Slice(t.Files, func(i, j int) bool { return t.Files[i].Path < t.Files[j].Path })
	return t
}

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
		ours[fc.Path] = true
		adds, dels = adds+fc.Add, dels+fc.Del
	}
	meta := "nothing edited yet"
	if len(changes) > 0 {
		meta = fmt.Sprintf("%s · +%d −%d", plural(len(changes), "file"), adds, dels)
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
		add(ref, "  "+arrow+" "+text(d.rel(fc.Path)), counts+"   "+dim(strings.Join(turns, " ")))
		if !open {
			continue
		}
		pad := strings.Repeat(" ", 7)
		bw := w - 16
		if fc.New {
			for i, l := range strings.Split(strings.TrimRight(fc.Content, "\n"), "\n") {
				if i >= 40 && !o.Verbose {
					add("", pad+dim("… ctrl+o shows the rest"), "")
					break
				}
				add("", pad+paint(cGreen, "▏")+dim(fmt.Sprintf("%5d ", i+1))+sub(truncateCells(expandTabs(l), bw)), "")
			}
			continue
		}
		for pi, p := range fc.Patches {
			if pi > 0 {
				add("", pad+dim("  ..."), "")
			}
			oldN, newN := p.OldStart, p.NewStart
			for _, l := range p.Lines {
				if l == "" {
					l = " "
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
		if ours[tree.Files[i].Path] {
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
		if f.Untracked {
			counts = paint(cGreen, "new")
		}
		who := dim("not from this session")
		mark := dim("·")
		if f.Ours {
			who, mark = paint(cOrange, "this session"), paint(cOrange, "●")
		}
		add("", "    "+mark+" "+text(d.rel(f.Path)), counts+"   "+who)
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
	for _, p := range headless.Patches(st.Result) {
		for _, l := range p.Lines {
			if l == "" || l[0] == ' ' {
				continue // only what changed; the rail is narrow
			}
			if shown == 8 {
				add("", "  "+dim("… the changes view has the rest"))
				break
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
