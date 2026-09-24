package ui

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/0xdeafcafe/agtop/internal/convo"
)

// A list item's start: its indent, its bullet or number, and a task box.
var mdItem = regexp.MustCompile(`^(\s*)([-*+]|\d+[.)])\s(\[[ xX]\]\s)?`)

var (
	mdHeading = regexp.MustCompile(`^(#{1,6})(\s|$)`)
	mdRule    = regexp.MustCompile(`^\s*([-*_])(\s*[-*_]){2,}\s*$`)
	mdLink    = regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)[^)]*\)`)
)

const italic = "\x1b[3m"

// styles is each rune's colour for lines first to last. Markdown is read
// from the top for the frontmatter and code blocks it's in.
func (e *docEditor) styles(first, last int) map[int][]string {
	out := make(map[int][]string, last-first+1)
	var md mdState
	var hl *convo.Highlighter
	if e.kind == docJSON {
		hl = convo.NewHighlighter("json")
	}
	n, start := 0, 0
	for i := 0; i <= len(e.buf) && n <= last; i++ {
		if i < len(e.buf) && e.buf[i] != '\n' {
			continue
		}
		line := e.buf[start:i]
		show := n >= first
		switch {
		case e.kind == docMarkdown:
			if st := md.line(line, e.siblings, show); show {
				out[n] = st
			}
		case !show:
		case hl != nil:
			out[n] = runeStyles(hl.Line(string(line), cText), len(line))
		default:
			out[n] = fill(len(line), cText)
		}
		start, n = i+1, n+1
	}
	return out
}

func fill(n int, c string) []string {
	st := make([]string, n)
	for i := range st {
		st[i] = c
	}
	return st
}

// runeStyles reads a highlighted line back as each rune's colour.
func runeStyles(s string, n int) []string {
	st := make([]string, 0, n)
	cur := cText
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := strings.IndexByte(s[i:], 'm')
			if j < 0 {
				break
			}
			if seq := s[i : i+j+1]; seq == reset {
				cur = cText
			} else {
				cur = seq
			}
			i += j + 1
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		st = append(st, cur)
		i += size
	}
	for len(st) < n {
		st = append(st, cText)
	}
	return st[:n]
}

// mdState is what a markdown file's earlier lines leave open.
type mdState struct {
	n     int
	front int // 1 inside the frontmatter, 2 past it or there's none
	fence string
	code  *convo.Highlighter
}

// line colours one line of markdown, or, when show is false, only follows
// what it opens and closes.
func (s *mdState) line(rs []rune, sib map[string]bool, show bool) []string {
	defer func() { s.n++ }()
	text := string(rs)
	trim := strings.TrimSpace(text)
	var st []string
	if show {
		st = fill(len(rs), cText)
	}
	if s.n == 0 && trim == "---" {
		s.front = 1
		return fillIf(st, cDim)
	}
	if s.front == 1 {
		if trim == "---" {
			s.front = 2
			return fillIf(st, cDim)
		}
		if show {
			if k := strings.IndexRune(text, ':'); k > 0 && !strings.HasPrefix(trim, "#") && !strings.HasPrefix(trim, "-") {
				k = len([]rune(text[:k]))
				paintRange(st, 0, k, cBlue)
				paintRange(st, k, k+1, cDim)
				paintRange(st, k+1, len(st), cSub)
			} else if strings.HasPrefix(trim, "#") {
				fillIf(st, cDim)
			} else {
				fillIf(st, cSub)
			}
		}
		return st
	}
	s.front = 2
	if f := fenceOf(trim); f != "" && (s.fence == "" || strings.HasPrefix(trim, s.fence) && strings.TrimLeft(trim, s.fence[:1]) == "") {
		if s.fence == "" {
			s.fence = f
			s.code = convo.NewHighlighter(strings.TrimSpace(strings.TrimLeft(trim, f[:1])))
		} else {
			s.fence, s.code = "", nil
		}
		if show {
			fillIf(st, cDim)
			if k := strings.IndexFunc(text, func(r rune) bool { return r != ' ' && r != '`' && r != '~' }); k >= 0 {
				paintRange(st, len([]rune(text[:k])), len(st), cSub)
			}
		}
		return st
	}
	if s.fence != "" {
		switch {
		case !show:
			if s.code != nil {
				s.code.Line(text, cSub) // keeps its strings and comments going
			}
		case s.code != nil:
			st = runeStyles(s.code.Line(text, cSub), len(rs))
		default:
			fillIf(st, cSub)
		}
		return st
	}
	if !show {
		return nil
	}
	// Block starts, then what's inline in the rest.
	from := 0
	base := cText
	switch {
	case mdHeading.MatchString(text):
		m := mdHeading.FindStringSubmatch(text)
		base = cText + bold
		if len(m[1]) <= 2 {
			base = cOrange + bold
		}
		paintRange(st, 0, len(m[1]), cDim)
		from = len(m[1])
		paintRange(st, from, len(st), base)
	case mdRule.MatchString(text):
		return fillIf(st, cDim)
	case strings.HasPrefix(trim, ">"):
		k := len([]rune(text[:strings.Index(text, ">")])) + 1
		paintRange(st, 0, k, cOrange)
		base = cSub + italic
		from = k
		paintRange(st, from, len(st), base)
	case strings.HasPrefix(trim, "|"):
		for i, r := range rs {
			if r == '|' {
				st[i] = cDim
			}
		}
	case mdItem.MatchString(text):
		m := mdItem.FindStringSubmatchIndex(text)
		b0, b1 := len([]rune(text[:m[4]])), len([]rune(text[:m[5]]))
		paintRange(st, b0, b1, cOrange)
		from = len([]rune(text[:m[1]]))
		if m[6] >= 0 {
			c0 := len([]rune(text[:m[6]]))
			if strings.ContainsAny(text[m[6]:m[7]], "xX") {
				paintRange(st, c0, c0+3, cGreen)
				base = cDim + "\x1b[9m"
				paintRange(st, from, len(st), base)
			} else {
				paintRange(st, c0, c0+3, cSub)
			}
		}
	}
	mdInline(rs, st, from, base, sib)
	return st
}

func fillIf(st []string, c string) []string {
	for i := range st {
		st[i] = c
	}
	return st
}

func paintRange(st []string, from, to int, c string) {
	for i := max(0, from); i < min(to, len(st)); i++ {
		st[i] = c
	}
}

// fenceOf is the fence a line opens or closes a code block with.
func fenceOf(trim string) string {
	for _, f := range []string{"```", "~~~"} {
		if strings.HasPrefix(trim, f) {
			return f
		}
	}
	return ""
}

func wordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// mdInline colours code, emphasis, links and [[links]] in rs from from.
func mdInline(rs []rune, st []string, from int, base string, sib map[string]bool) {
	find := func(i int, pat string) int {
		p := []rune(pat)
		for j := i; j+len(p) <= len(rs); j++ {
			if string(rs[j:j+len(p)]) == pat {
				return j
			}
		}
		return -1
	}
	for i := from; i < len(rs); {
		r := rs[i]
		switch {
		case r == '`':
			if j := find(i+1, "`"); j > i {
				paintRange(st, i, i+1, cDim)
				paintRange(st, i+1, j, cYellow)
				paintRange(st, j, j+1, cDim)
				i = j + 1
				continue
			}
		case r == '<' && strings.HasPrefix(string(rs[i:min(len(rs), i+4)]), "<!--"):
			j := find(i+4, "-->")
			if j < 0 {
				j = len(rs) - 3
			}
			paintRange(st, i, j+3, cDim)
			i = j + 3
			continue
		case r == '[' && i+1 < len(rs) && rs[i+1] == '[':
			if j := find(i+2, "]]"); j > i {
				c := cBlue
				if sib != nil && !sib[string(rs[i+2:j])] {
					c = cSub // not written yet
				}
				paintRange(st, i, i+2, cDim)
				paintRange(st, i+2, j, c)
				paintRange(st, j, j+2, cDim)
				i = j + 2
				continue
			}
		case r == '[':
			if j := find(i+1, "]"); j > i && j+1 < len(rs) && rs[j+1] == '(' {
				if k := find(j+2, ")"); k > j {
					paintRange(st, i, i+1, cDim)
					paintRange(st, i+1, j, cBlue)
					paintRange(st, j, k+1, cDim)
					i = k + 1
					continue
				}
			}
		case (r == '*' || r == '_') && i+1 < len(rs) && rs[i+1] == r:
			if j := find(i+2, string([]rune{r, r})); j > i+2 {
				paintRange(st, i, i+2, cDim)
				paintRange(st, i+2, j, base+bold)
				paintRange(st, j, j+2, cDim)
				i = j + 2
				continue
			}
		case (r == '*' || r == '_') && i+1 < len(rs) && rs[i+1] != ' ' && (i == 0 || !wordRune(rs[i-1])):
			j := i + 1
			for j < len(rs) && !(rs[j] == r && rs[j-1] != ' ' && (j+1 >= len(rs) || r == '*' || !wordRune(rs[j+1]))) {
				j++
			}
			if j < len(rs) {
				paintRange(st, i, i+1, cDim)
				paintRange(st, i+1, j, base+italic)
				paintRange(st, j, j+1, cDim)
				i = j + 1
				continue
			}
		case r == 'h' && (strings.HasPrefix(string(rs[i:min(len(rs), i+8)]), "https://") || strings.HasPrefix(string(rs[i:min(len(rs), i+7)]), "http://")):
			j := i
			for j < len(rs) && rs[j] != ' ' && rs[j] != ')' && rs[j] != '>' {
				j++
			}
			paintRange(st, i, j, cBlue)
			i = j
			continue
		}
		st[i] = base
		i++
	}
}

// --- problems ---

// problems finds what's wrong with the file as it stands.
func (e *docEditor) problems() []docDiag {
	if e.diagVer == e.ver {
		return e.diags
	}
	e.diagVer = e.ver
	switch e.kind {
	case docJSON:
		e.diags = jsonProblems(string(e.buf))
	case docMarkdown:
		e.diags = mdProblems(e.path, string(e.buf))
	default:
		e.diags = nil
	}
	return e.diags
}

// jsonProblems checks JSON with encoding/json/jsontext: its syntax, and
// names given twice in one object.
func jsonProblems(text string) []docDiag {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	dec := jsontext.NewDecoder(strings.NewReader(text))
	_, err := dec.ReadValue()
	if err == nil {
		if rest := text[dec.InputOffset():]; strings.TrimSpace(rest) != "" {
			off := int(dec.InputOffset()) + len(rest) - len(strings.TrimLeft(rest, " \t\r\n"))
			l, c := lineCol(text, off)
			return []docDiag{{line: l, col: c, msg: "more after the JSON ends: a file holds one value"}}
		}
		return nil
	}
	off, msg := int(dec.InputOffset()), err.Error()
	var se *jsontext.SyntacticError
	if errors.As(err, &se) {
		off, msg = int(se.ByteOffset), se.Err.Error()
		switch {
		case msg == "unexpected EOF":
			msg = "it ends before every { [ or \" is closed"
		case strings.HasPrefix(msg, "duplicate object member name"):
			name := se.JSONPointer.LastToken()
			msg = fmt.Sprintf("%q is in this object twice; only one counts", name)
		case trailingComma(text, off):
			msg = "a comma before the close: JSON allows none there"
			off = strings.LastIndexByte(text[:off], ',')
		}
		if p := string(se.JSONPointer); p != "" && !strings.Contains(msg, "twice") {
			msg += " · in " + p
		}
	}
	l, c := lineCol(text, min(off, len(text)))
	return []docDiag{{line: l, col: c, msg: msg}}
}

// trailingComma says whether the closer at off follows a comma.
func trailingComma(text string, off int) bool {
	if off >= len(text) || (text[off] != '}' && text[off] != ']') {
		return false
	}
	before := strings.TrimRight(text[:off], " \t\r\n")
	return strings.HasSuffix(before, ",")
}

// lineCol is a byte offset as a line and a rune column, from zero.
func lineCol(text string, off int) (int, int) {
	before := text[:off]
	l := strings.Count(before, "\n")
	return l, len([]rune(before[strings.LastIndexByte(before, '\n')+1:]))
}

// formatJSON lays JSON out again with the file's indent, keeping the order
// of its names.
func formatJSON(text, indent string) (string, error) {
	v := jsontext.Value(strings.TrimSpace(text))
	if err := v.Indent(jsontext.WithIndent(indent)); err != nil {
		return "", err
	}
	return string(v) + "\n", nil
}

// mdProblems checks markdown: frontmatter and code blocks that never
// close, what a memory note, skill or agent needs in its frontmatter, and
// links to files that don't exist.
func mdProblems(path, text string) []docDiag {
	var out []docDiag
	lines := strings.Split(text, "\n")
	front := map[string]int{} // a frontmatter key, and its line
	frontEnd := -1
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		for i := 1; i < len(lines); i++ {
			t := strings.TrimSpace(lines[i])
			if t == "---" {
				frontEnd = i
				break
			}
			if k, _, ok := strings.Cut(t, ":"); ok && !strings.HasPrefix(t, "#") {
				if _, seen := front[k]; !seen {
					front[strings.TrimSpace(k)] = i
				}
			}
		}
		if frontEnd < 0 {
			out = append(out, docDiag{line: 0, msg: "the frontmatter never closes: it needs a --- line after it"})
			front = map[string]int{}
		}
	}
	fence, fenceAt := "", 0
	dir := filepath.Dir(path)
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if i <= frontEnd {
			continue
		}
		if f := fenceOf(t); f != "" && (fence == "" || strings.TrimLeft(t, f[:1]) == "") {
			if fence == "" {
				fence, fenceAt = f, i
			} else {
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		for _, m := range mdLink.FindAllStringSubmatchIndex(l, -1) {
			target := l[m[2]:m[3]]
			if strings.Contains(target, "://") || strings.HasPrefix(target, "#") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			target, _, _ = strings.Cut(target, "#")
			p := target
			if !filepath.IsAbs(p) {
				p = filepath.Join(dir, p)
			}
			if _, err := os.Stat(p); err != nil {
				out = append(out, docDiag{line: i, col: len([]rune(l[:m[2]])), msg: target + " doesn't exist", warn: true})
			}
		}
	}
	if fence != "" {
		out = append(out, docDiag{line: fenceAt, msg: "this code block never closes", warn: true})
	}

	need := func(keys []string, why string) {
		at := max(0, frontEnd)
		if frontEnd < 0 {
			out = append(out, docDiag{line: 0, msg: "no frontmatter: " + why, warn: true})
			return
		}
		var missing []string
		for _, k := range keys {
			if _, ok := front[k]; !ok {
				missing = append(missing, k)
			}
		}
		if len(missing) > 0 {
			out = append(out, docDiag{line: at, msg: "the frontmatter has no " + strings.Join(missing, " or ") + ": " + why, warn: true})
		}
	}
	base := filepath.Base(path)
	switch {
	case base == "MEMORY.md":
		if len(lines) > 200 {
			out = append(out, docDiag{line: 200, msg: "Claude loads only the first 200 lines of MEMORY.md", warn: true})
		}
	case filepath.Base(dir) == "memory":
		need([]string{"name", "description", "type"}, "a memory note needs a name, a one-line description and a type")
		if i, ok := front["type"]; ok {
			_, v, _ := strings.Cut(lines[i], ":")
			switch strings.Trim(strings.TrimSpace(v), `"'`) {
			case "user", "feedback", "project", "reference":
			default:
				out = append(out, docDiag{line: i, msg: "type is one of user, feedback, project or reference", warn: true})
			}
		}
	case base == "SKILL.md":
		need([]string{"name", "description"}, "Claude picks a skill by its description")
	case filepath.Base(dir) == "agents":
		need([]string{"name", "description"}, "Claude picks an agent by its description")
	}
	return out
}
