package claude

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// What Claude Code loads from a project's memory and instructions, when,
// and what looks untidy about them.
//
// At the start of every session Claude Code reads the CLAUDE.md files (the
// user's, the project's and its parent folders', CLAUDE.local.md), the
// rules without a paths: list, whatever they @-import, and the first
// IndexLines lines (at most IndexBytes) of the auto memory's MEMORY.md. A
// memory note is read only when it's recalled, which goes by its line in
// MEMORY.md or its description.

const (
	// IndexLines and IndexBytes are how much of MEMORY.md Claude Code
	// loads (Claude Code 2.1: its lines after 200 are cut, and at 25,000
	// bytes whatever comes first).
	IndexLines = 200
	IndexBytes = 25_000
	// IndexLineLen is what Claude Code asks each line of MEMORY.md to stay
	// under.
	IndexLineLen = 150
	// BigInstructions is when the instructions loaded every session are
	// worth trimming, in estimated tokens; BigFile is the size of one file
	// Claude Code itself warns about.
	BigInstructions = 10_000
	BigFile         = 40_000
	importHops      = 5
)

// EstTokens is a rough token count for n bytes of text: a quarter.
func EstTokens(n int64) int64 { return (n + 3) / 4 }

// FrontMatter reads name, description and type from a markdown file's
// frontmatter, at any depth (a memory note keeps its type under metadata),
// and "paths" when a rule has a paths: list.
func FrontMatter(path string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	block := "" // the key whose value is a block (| or >) on the lines below
	for n := 0; sc.Scan() && n < 60; n++ {
		line := sc.Text()
		if block != "" {
			if t := strings.TrimSpace(line); t != "" && (line[0] == ' ' || line[0] == '\t') {
				out[block] = strings.TrimSpace(out[block] + " " + t)
				continue
			}
			block = ""
		}
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
			if out[k] != "" {
				break
			}
			switch v = strings.TrimSpace(v); v {
			case "|", ">", "|-", ">-", "|+", ">+":
				block = k
			default:
				out[k] = strings.Trim(v, `"'`)
			}
		case "paths", "globs":
			out["paths"] = "yes"
		}
	}
	return out
}

// MemIndex is a MEMORY.md, and how much of it is loaded.
type MemIndex struct {
	Path   string
	Exists bool
	Lines  int
	Bytes  int
	// LoadedLines and LoadedBytes are what reaches Claude.
	LoadedLines int
	LoadedBytes int
	Links       []MemLink
	Long        int // lines over IndexLineLen characters
}

// Cut is whether some of it isn't loaded.
func (ix MemIndex) Cut() bool { return ix.LoadedLines < ix.Lines }

// MemLink is a line of MEMORY.md pointing at a note.
type MemLink struct {
	Line int    // from 1
	File string // the note
	Past bool   // past what's loaded
}

var mdLink = regexp.MustCompile(`\]\(([^)\s]+\.md)\)`)

// ReadMemIndex reads a MEMORY.md.
func ReadMemIndex(path string) MemIndex {
	ix := MemIndex{Path: path}
	b, err := os.ReadFile(path)
	if err != nil {
		return ix
	}
	ix.Exists, ix.Bytes = true, len(b)
	text := strings.TrimRight(string(b), "\n")
	if strings.TrimSpace(text) == "" {
		return ix
	}
	lines := strings.Split(text, "\n")
	ix.Lines = len(lines)
	n := 0
	for i, l := range lines {
		loaded := i < IndexLines && n+len(l) <= IndexBytes
		if loaded {
			n += len(l) + 1
			ix.LoadedLines++
		}
		if utf8.RuneCountInString(l) > IndexLineLen {
			ix.Long++
		}
		for _, m := range mdLink.FindAllStringSubmatch(l, -1) {
			p := m[1]
			if strings.Contains(p, "://") {
				continue
			}
			if !filepath.IsAbs(p) {
				p = filepath.Join(filepath.Dir(path), p)
			}
			ix.Links = append(ix.Links, MemLink{Line: i + 1, File: filepath.Clean(p), Past: !loaded})
		}
	}
	ix.LoadedBytes = min(n, ix.Bytes)
	return ix
}

// MemNote is a memory note, next to MEMORY.md.
type MemNote struct {
	Path        string
	Name        string
	Description string
	Type        string
	Size        int64
	// Indexed is whether a loaded line of MEMORY.md points at it; PastCut,
	// whether one does only past what's loaded.
	Indexed bool
	PastCut bool
	Gone    []string // files it names that no longer exist
	Same    string   // the path of a note it repeats
}

// Instruction is a file loaded as instructions.
type Instruction struct {
	Path  string
	Scope string // user, project, local, parent, rule, import
	// From is the file that @-imports it, and Ref how it was written.
	From string
	Ref  string
	// OnDemand is a rule with paths: it loads when Claude reads a file that
	// matches them, not at the start.
	OnDemand bool
	Missing  bool
	Size     int64
}

// Instructions are the instruction files a session in cwd loads, the
// user's first, then the project's and each parent folder's nearest first
// (up to but not including home), each followed by what it @-imports.
func Instructions(cfg, cwd string) []Instruction {
	var out []Instruction
	seen := map[string]bool{}
	var add func(in Instruction, hops int)
	add = func(in Instruction, hops int) {
		if seen[in.Path] {
			return
		}
		st, err := os.Stat(in.Path)
		if in.Scope != "import" && (err != nil || st.IsDir()) {
			return
		}
		seen[in.Path] = true
		in.Missing = err != nil || st.IsDir()
		if !in.Missing {
			in.Size = st.Size()
		}
		out = append(out, in)
		if in.Missing || hops >= importHops {
			return
		}
		for _, im := range imports(in.Path) {
			add(Instruction{Path: im.path, Scope: "import", From: in.Path, Ref: im.ref, OnDemand: in.OnDemand}, hops+1)
		}
	}
	rules := func(root, scope string) {
		var files []string
		_ = filepath.WalkDir(filepath.Join(root, "rules"), func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(p, ".md") {
				files = append(files, p)
			}
			return nil
		})
		for _, p := range files {
			add(Instruction{Path: p, Scope: scope, OnDemand: FrontMatter(p)["paths"] != ""}, 0)
		}
	}
	add(Instruction{Path: filepath.Join(cfg, "CLAUDE.md"), Scope: "user"}, 0)
	home, _ := os.UserHomeDir()
	var dirs []string
	for d := cwd; d != "" && d != "/" && d != "." && d != home; d = filepath.Dir(d) {
		dirs = append(dirs, d)
	}
	for _, d := range dirs {
		scope := "parent"
		if d == cwd {
			scope = "project"
		}
		add(Instruction{Path: filepath.Join(d, "CLAUDE.md"), Scope: scope}, 0)
		add(Instruction{Path: filepath.Join(d, ".claude", "CLAUDE.md"), Scope: scope}, 0)
		add(Instruction{Path: filepath.Join(d, "CLAUDE.local.md"), Scope: "local"}, 0)
	}
	rules(cfg, "rule")
	for _, d := range dirs {
		rules(filepath.Join(d, ".claude"), "rule")
	}
	return out
}

type imported struct{ ref, path string }

var (
	atRef    = regexp.MustCompile(`(?:^|[\s(])@([^\s@()\[\]<>` + "`" + `"']+)`)
	codeSpan = regexp.MustCompile("`[^`]*`")
	fileExt  = regexp.MustCompile(`\.[A-Za-z][A-Za-z0-9]{0,5}$`)
)

// imports are the @paths a file imports, outside code, as Claude Code
// reads them: from home with ~/, else from the file's folder. One that
// doesn't exist counts only when it's plainly a file (a path, or a name
// with an extension), so a package name like @scope/pkg isn't one.
func imports(path string) []imported {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	home, _ := os.UserHomeDir()
	var out []imported
	fenced := false
	for _, l := range strings.Split(string(b), "\n") {
		if t := strings.TrimSpace(l); strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		for _, m := range atRef.FindAllStringSubmatch(codeSpan.ReplaceAllString(l, ""), -1) {
			ref := strings.TrimRight(m[1], ".,;:!?")
			p := ref
			switch {
			case strings.HasPrefix(p, "~/"):
				p = filepath.Join(home, p[2:])
			case !filepath.IsAbs(p):
				p = filepath.Join(filepath.Dir(path), p)
			}
			if _, err := os.Stat(p); err != nil {
				plain := strings.HasPrefix(ref, "./") || strings.HasPrefix(ref, "../") || strings.HasPrefix(ref, "~/") || strings.HasPrefix(ref, "/")
				if !plain && !fileExt.MatchString(ref) {
					continue
				}
			}
			out = append(out, imported{ref: ref, path: filepath.Clean(p)})
		}
	}
	return out
}

// MemReport is what a session in cwd loads from its instructions and its
// auto memory (in proj, the project's folder under projects/), and what's
// worth tidying.
type MemReport struct {
	Index MemIndex
	Notes []MemNote
	Instr []Instruction
	// Upfront is the bytes loaded every session: the instructions and
	// their imports, and what's loaded of MEMORY.md.
	Upfront  int64
	Problems []MemProblem
}

// MemProblem is something untidy, and what to do about it.
type MemProblem struct {
	Kind  string
	Title string
	Fix   string
	Path  string   // the file to open for it
	Files []string // the files it's about
}

// Note is the note at path.
func (r *MemReport) Note(path string) *MemNote {
	for i := range r.Notes {
		if r.Notes[i].Path == path {
			return &r.Notes[i]
		}
	}
	return nil
}

// CheckMemory reads what a session in cwd loads, and what's untidy in it.
func CheckMemory(cfg, cwd, proj string) MemReport {
	var r MemReport
	r.Instr = Instructions(cfg, cwd)
	var instr int64
	for _, in := range r.Instr {
		if !in.Missing && !in.OnDemand {
			instr += in.Size
		}
	}
	if proj != "" {
		dir := filepath.Join(proj, "memory")
		r.Index = ReadMemIndex(filepath.Join(dir, "MEMORY.md"))
		paths, _ := filepath.Glob(filepath.Join(dir, "*.md"))
		sort.Strings(paths)
		linked := map[string]int{} // 1 loaded, 2 only past the cut
		for _, l := range r.Index.Links {
			if !l.Past {
				linked[l.File] = 1
			} else if linked[l.File] == 0 {
				linked[l.File] = 2
			}
		}
		for _, p := range paths {
			if filepath.Base(p) == "MEMORY.md" {
				continue
			}
			fm := FrontMatter(p)
			n := MemNote{Path: p, Name: fm["name"], Description: fm["description"], Type: fm["type"],
				Indexed: linked[p] == 1, PastCut: linked[p] == 2}
			if st, err := os.Stat(p); err == nil {
				n.Size = st.Size()
			}
			n.Gone = goneFiles(p, cwd)
			r.Notes = append(r.Notes, n)
		}
		sameNotes(r.Notes)
	}
	r.Upfront = instr + int64(r.Index.LoadedBytes)
	r.Problems = memProblems(&r, instr)
	return r
}

var fileRef = regexp.MustCompile("`([~./A-Za-z0-9_-][A-Za-z0-9_./@~-]*/[A-Za-z0-9_.@-]+\\.[A-Za-z][A-Za-z0-9]{0,5})`")

// goneFiles are the files a note names, in backticks, that no longer
// exist: paths from the project's folder whose first folder is still
// there (so it's the same tree), or from home. A cheap check: it doesn't
// follow renames.
func goneFiles(note, cwd string) []string {
	if cwd == "" {
		return nil
	}
	if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
		return nil
	}
	b, err := os.ReadFile(note)
	if err != nil {
		return nil
	}
	home, _ := os.UserHomeDir()
	var out []string
	seen := map[string]bool{}
	for _, m := range fileRef.FindAllStringSubmatch(string(b), -1) {
		ref := m[1]
		if seen[ref] || strings.Contains(ref, "...") {
			continue
		}
		seen[ref] = true
		var p, top string
		switch {
		case strings.HasPrefix(ref, "~/"):
			p = filepath.Join(home, ref[2:])
			top = filepath.Join(home, strings.Split(ref[2:], "/")[0])
		case filepath.IsAbs(ref):
			if !strings.HasPrefix(ref, cwd+"/") {
				continue
			}
			p = ref
			top = filepath.Join(cwd, strings.Split(strings.TrimPrefix(ref, cwd+"/"), "/")[0])
		default:
			rel := strings.TrimPrefix(ref, "./")
			if strings.HasPrefix(rel, "../") {
				continue
			}
			p = filepath.Join(cwd, rel)
			top = filepath.Join(cwd, strings.Split(rel, "/")[0])
		}
		if top == p {
			continue
		}
		if st, err := os.Stat(top); err != nil || !st.IsDir() {
			continue
		}
		if _, err := os.Stat(p); err != nil {
			out = append(out, ref)
		}
	}
	return out
}

// sameNotes marks the notes that repeat another: the same name, or
// descriptions that share most of their words.
func sameNotes(notes []MemNote) {
	words := make([]map[string]bool, len(notes))
	for i, n := range notes {
		words[i] = wordSet(n.Description)
	}
	for i := range notes {
		for j := 0; j < i; j++ {
			a, b := notes[i], notes[j]
			same := a.Name != "" && squash(a.Name) == squash(b.Name)
			if !same && len(words[i]) >= 4 && len(words[j]) >= 4 {
				same = jaccard(words[i], words[j]) >= 0.7
			}
			if same {
				notes[i].Same = b.Path
				break
			}
		}
	}
}

func squash(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		if r >= 'A' && r <= 'Z' {
			return r + 'a' - 'A'
		}
		return -1
	}, s)
}

func wordSet(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		if len(w) > 2 {
			out[w] = true
		}
	}
	return out
}

func jaccard(a, b map[string]bool) float64 {
	both := 0
	for w := range a {
		if b[w] {
			both++
		}
	}
	return float64(both) / float64(len(a)+len(b)-both)
}

// memProblems are what's untidy, most pressing first.
func memProblems(r *MemReport, instr int64) []MemProblem {
	var out []MemProblem
	ix := r.Index
	names := func(paths []string) string {
		var s []string
		for i, p := range paths {
			if i == 3 {
				s = append(s, fmt.Sprintf("and %d more", len(paths)-3))
				break
			}
			s = append(s, strings.TrimSuffix(filepath.Base(p), ".md"))
		}
		return strings.Join(s, ", ")
	}
	if ix.Cut() {
		var past []string
		for _, n := range r.Notes {
			if n.PastCut {
				past = append(past, n.Path)
			}
		}
		fix := fmt.Sprintf("Claude never sees the %d lines past it", ix.Lines-ix.LoadedLines)
		if len(past) > 0 {
			fix += fmt.Sprintf(", so %d notes are hardly ever recalled (%s)", len(past), names(past))
		}
		out = append(out, MemProblem{Kind: "index-cut", Path: ix.Path, Files: past,
			Title: fmt.Sprintf("MEMORY.md is past what Claude loads: %d lines, %s; it reads the first %d lines or %s", ix.Lines, kb(ix.Bytes), IndexLines, kb(IndexBytes)),
			Fix:   fix + ". Merge or drop old lines, and keep each to a title and a short hook."})
	}
	var broken []string
	seen := map[string]bool{}
	for _, l := range ix.Links {
		if _, err := os.Stat(l.File); err != nil && !seen[l.File] {
			seen[l.File] = true
			broken = append(broken, l.File)
		}
	}
	if len(broken) > 0 {
		out = append(out, MemProblem{Kind: "broken-link", Path: ix.Path, Files: broken,
			Title: plural(len(broken), "line of MEMORY.md points", "lines of MEMORY.md point") + " at a note that doesn't exist",
			Fix:   "Take the lines out, or write the notes: " + names(broken) + "."})
	}
	var badImports []string
	for _, in := range r.Instr {
		if in.Scope == "import" && in.Missing {
			badImports = append(badImports, "@"+in.Ref+" in "+filepath.Base(in.From))
		}
	}
	if len(badImports) > 0 {
		var from string
		for _, in := range r.Instr {
			if in.Scope == "import" && in.Missing {
				from = in.From
				break
			}
		}
		out = append(out, MemProblem{Kind: "broken-import", Path: from,
			Title: plural(len(badImports), "@import doesn't", "@imports don't") + " lead to a file",
			Fix:   "Claude Code skips them without a word: " + strings.Join(badImports, ", ") + ". Fix the path or take it out."})
	}
	if est := EstTokens(instr); est > BigInstructions || biggest(r.Instr).Size > BigFile {
		b := biggest(r.Instr)
		out = append(out, MemProblem{Kind: "big-instructions", Path: b.Path,
			Title: fmt.Sprintf("≈%s tokens of CLAUDE.md and rules load every session", short(est)),
			Fix: fmt.Sprintf("Every request carries them; the biggest is %s (≈%s). Move what only some work needs into a skill or a rule with paths:, and cut the rest.",
				filepath.Base(b.Path), short(EstTokens(b.Size)))})
	}
	var orphans, same, gone, bare []string
	var goneWhat []string
	for _, n := range r.Notes {
		if !n.Indexed && !n.PastCut && ix.Exists {
			orphans = append(orphans, n.Path)
		}
		if n.Same != "" {
			same = append(same, strings.TrimSuffix(filepath.Base(n.Path), ".md")+" ≈ "+strings.TrimSuffix(filepath.Base(n.Same), ".md"))
		}
		if len(n.Gone) > 0 {
			gone = append(gone, n.Path)
			goneWhat = append(goneWhat, strings.TrimSuffix(filepath.Base(n.Path), ".md")+" ("+n.Gone[0]+")")
		}
		if n.Description == "" {
			bare = append(bare, n.Path)
		}
	}
	if len(orphans) > 0 {
		out = append(out, MemProblem{Kind: "orphan", Path: ix.Path, Files: orphans,
			Title: plural(len(orphans), "memory note isn't", "memory notes aren't") + " in MEMORY.md",
			Fix:   "Claude sees only the index up front, so these are found only if recall happens to pick them: " + names(orphans) + ". Add a line for each, or delete the ones you don't need."})
	}
	if len(same) > 0 {
		out = append(out, MemProblem{Kind: "duplicate", Path: r.Notes[0].Path,
			Title: plural(len(same), "memory note repeats", "memory notes repeat") + " another",
			Fix:   "Merge each pair into one note, and its MEMORY.md line: " + strings.Join(first(same, 3), "; ") + "."})
		for _, n := range r.Notes {
			if n.Same != "" {
				out[len(out)-1].Path, out[len(out)-1].Files = n.Path, []string{n.Path, n.Same}
				break
			}
		}
	}
	if len(gone) > 0 {
		out = append(out, MemProblem{Kind: "stale", Path: gone[0], Files: gone,
			Title: plural(len(gone), "memory note names", "memory notes name") + " files that are gone",
			Fix:   "They may be out of date: " + strings.Join(first(goneWhat, 3), ", ") + ". Check each is still true, then update or delete it."})
	}
	if ix.Long > 0 {
		out = append(out, MemProblem{Kind: "long-lines", Path: ix.Path,
			Title: plural(ix.Long, "line of MEMORY.md is", "lines of MEMORY.md are") + fmt.Sprintf(" over %d characters", IndexLineLen),
			Fix:   "MEMORY.md is loaded every session: keep each line a title and a short hook, and the detail in its note."})
	}
	if len(bare) > 0 {
		out = append(out, MemProblem{Kind: "no-description", Path: bare[0], Files: bare,
			Title: plural(len(bare), "memory note has", "memory notes have") + " no description",
			Fix:   "Recall picks notes by their description: give each a one-line description: in its frontmatter (" + names(bare) + ")."})
	}
	return out
}

func biggest(in []Instruction) Instruction {
	var b Instruction
	for _, i := range in {
		if !i.Missing && !i.OnDemand && i.Size > b.Size {
			b = i
		}
	}
	return b
}

func first(s []string, n int) []string {
	if len(s) > n {
		return append(s[:n:n], fmt.Sprintf("and %d more", len(s)-n))
	}
	return s
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

func kb(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d bytes", n)
	}
	return fmt.Sprintf("%.1f KB", float64(n)/1000)
}

// short is a token count in words, short.
func short(n int64) string {
	switch {
	case n >= 10_000:
		return fmt.Sprintf("%.0fk", float64(n)/1e3)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return fmt.Sprintf("%d", n)
}
