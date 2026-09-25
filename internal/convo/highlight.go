package convo

import (
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

// A small syntax highlighter: one pass over a line's bytes, no regular
// expressions, a word's class looked up without allocating, and colour
// written only where it changes, never reset mid-line, so a background laid
// under the line (a diff's, or its changed words') holds across it.

// Syntax colours, muted to sit under agtop's text.
var (
	hlKw      = fg(204, 153, 205) // keywords
	hlStr     = fg(163, 190, 140) // strings
	hlNum     = fg(222, 165, 132) // numbers and constants
	hlComment = fg(122, 116, 108)
	// hlOutComment is a comment in what a tool printed, which is already
	// as quiet as hlComment: it goes a step further under.
	hlOutComment = fg(92, 87, 80)
	hlFn         = fg(137, 180, 222) // a call, a key, a variable
	hlType       = fg(120, 190, 175)
	hlSpace      = fg(92, 88, 82) // · and → marking spaces and tabs
)

// showSpace marks spaces and tabs in diffs, as · and →, the way an editor
// can; SetShowWhitespace turns it on.
var showSpace bool

// SetShowWhitespace turns marking spaces and tabs in diffs on or off.
func SetShowWhitespace(on bool) {
	if on != showSpace {
		showSpace = on
		palette++ // what's drawn already has them, or hasn't
	}
}

// lang is what the highlighter knows of a language.
type lang struct {
	words      map[string]byte // 'k' keyword, 't' type, 'c' constant
	line       string          // a line comment's start: //, #, --
	line2      string          // a second one, as # in PHP
	open, shut string          // a block comment's ends
	quotes     string          // characters that quote a string on one line
	multi      byte            // a quote that may run over lines: ` in Go and JS
	triple     bool            // """ and ''' strings, over lines
	caps       bool            // a capitalised word is a type
	vars       bool            // $name is a variable
	keys       bool            // a key before : (JSON, YAML, TOML's =)
	md         bool            // Markdown, drawn by its own rules
}

func set(class byte, ws string, into map[string]byte) map[string]byte {
	if into == nil {
		into = map[string]byte{}
	}
	for _, w := range strings.Fields(ws) {
		into[w] = class
	}
	return into
}

func words(kw, types, consts string) map[string]byte {
	m := set('k', kw, nil)
	set('t', types, m)
	return set('c', consts, m)
}

var (
	langGo = &lang{words: words(
		"break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var",
		"bool byte complex64 complex128 error float32 float64 int int8 int16 int32 int64 rune string uint uint8 uint16 uint32 uint64 uintptr any comparable",
		"true false nil iota"),
		line: "//", open: "/*", shut: "*/", quotes: `"'`, multi: '`', caps: true}
	langJS = &lang{words: words(
		"async await break case catch class const continue debugger default delete do else export extends finally for from function if import in instanceof let new of return static super switch this throw try typeof var void while with yield as type interface enum implements declare readonly keyof satisfies namespace abstract private protected public",
		"string number boolean any unknown never object void bigint symbol Promise Array Record Map Set",
		"true false null undefined NaN Infinity"),
		line: "//", open: "/*", shut: "*/", quotes: `"'`, multi: '`', caps: true}
	langPy = &lang{words: words(
		"and as assert async await break class continue def del elif else except finally for from global if import in is lambda nonlocal not or pass raise return try while with yield match case",
		"int str float bool list dict set tuple bytes object type self cls",
		"True False None"),
		line: "#", quotes: `"'`, triple: true}
	langRust = &lang{words: words(
		"as async await break const continue crate dyn else enum extern fn for if impl in let loop match mod move mut pub ref return static struct super trait type unsafe use where while",
		"i8 i16 i32 i64 i128 isize u8 u16 u32 u64 u128 usize f32 f64 bool char str String Vec Option Result Box Self self",
		"true false None Some Ok Err"),
		line: "//", open: "/*", shut: "*/", quotes: `"`, caps: true}
	langSh = &lang{words: words(
		"if then else elif fi for in do done while until case esac function return local export readonly unset set shift exit break continue source eval exec trap",
		"echo printf cd test read declare",
		"true false"),
		line: "#", quotes: `"'`, vars: true}
	langJSON = &lang{words: words("", "", "true false null"), quotes: `"`, keys: true, line: "//"}
	langYAML = &lang{words: words("", "", "true false null yes no on off ~"), line: "#", quotes: `"'`, keys: true}
	langTOML = &lang{words: words("", "", "true false"), line: "#", quotes: `"'`, keys: true, triple: true}
	langCSS  = &lang{words: words("important media import supports keyframes font-face", "", "inherit initial unset none auto"), open: "/*", shut: "*/", quotes: `"'`, line: "//"}
	langSQL  = &lang{words: words(
		"select from where and or not in is null as join left right inner outer full on group by order having limit offset insert into values update set delete create table index view drop alter add column primary key foreign references unique default distinct union all case when then else end with returning exists between like asc desc begin commit rollback if SELECT FROM WHERE AND OR NOT IN IS NULL AS JOIN LEFT RIGHT INNER OUTER ON GROUP BY ORDER HAVING LIMIT OFFSET INSERT INTO VALUES UPDATE SET DELETE CREATE TABLE INDEX VIEW DROP ALTER ADD COLUMN PRIMARY KEY FOREIGN REFERENCES UNIQUE DEFAULT DISTINCT UNION ALL CASE WHEN THEN ELSE END WITH RETURNING EXISTS BETWEEN LIKE ASC DESC BEGIN COMMIT ROLLBACK IF",
		"int integer bigint text varchar char boolean timestamp timestamptz date jsonb json uuid serial numeric real INT INTEGER BIGINT TEXT VARCHAR BOOLEAN TIMESTAMP DATE JSONB UUID SERIAL NUMERIC",
		"true false TRUE FALSE"),
		line: "--", open: "/*", shut: "*/", quotes: `'"`}
	langRuby = &lang{words: words(
		"alias and begin break case class def defined do else elsif end ensure for if in module next not or redo rescue retry return self super then undef unless until when while yield require require_relative attr_accessor attr_reader",
		"", "true false nil"),
		line: "#", quotes: `"'`, caps: true}
	// langC covers the C family: C, C++, C#, Java, Kotlin, Swift, Dart.
	langC = &lang{words: words(
		"auto break case catch class const continue default delete do else enum extern final finally for fun func goto if implements import in include interface let namespace new override package private protected public return sizeof static struct switch template this throw throws try typedef typename union using val var virtual void volatile when while guard defer extension protocol init self super import object data sealed suspend",
		"int long short char float double bool boolean byte string String unsigned signed size_t Int Long Double Float Bool Boolean",
		"true false null nullptr nil NULL"),
		line: "//", open: "/*", shut: "*/", quotes: `"'`, caps: true}
	langLua = &lang{words: words("and break do else elseif end for function goto if in local not or repeat return then until while", "", "true false nil"), line: "--", quotes: `"'`}
	langPHP = &lang{words: words(
		"abstract and array as break case catch class clone const continue declare default do echo else elseif empty endif extends final finally fn for foreach function global if implements include interface isset list match namespace new or print private protected public readonly require return static switch throw trait try unset use var while yield",
		"int string bool float array object mixed void", "true false null TRUE FALSE NULL"),
		line: "//", line2: "#", open: "/*", shut: "*/", quotes: `"'`, vars: true, caps: true}
	langMD = &lang{md: true}
)

// langByName is a language by a fence's tag or a file's extension.
var langByName = map[string]*lang{
	"go": langGo, "golang": langGo,
	"js": langJS, "javascript": langJS, "jsx": langJS, "mjs": langJS, "cjs": langJS,
	"ts": langJS, "typescript": langJS, "tsx": langJS, "mts": langJS, "cts": langJS,
	"py": langPy, "python": langPy, "python3": langPy, "pyi": langPy,
	"rs": langRust, "rust": langRust,
	"sh": langSh, "bash": langSh, "zsh": langSh, "shell": langSh, "console": langSh, "fish": langSh,
	"json": langJSON, "jsonc": langJSON, "json5": langJSON, "jsonl": langJSON,
	"yaml": langYAML, "yml": langYAML,
	"toml": langTOML, "ini": langTOML, "env": langTOML,
	"css": langCSS, "scss": langCSS, "less": langCSS,
	"sql": langSQL, "psql": langSQL,
	"rb": langRuby, "ruby": langRuby,
	"c": langC, "h": langC, "cpp": langC, "cc": langC, "hpp": langC, "c++": langC, "cs": langC, "csharp": langC,
	"java": langC, "kt": langC, "kts": langC, "kotlin": langC, "swift": langC, "dart": langC, "scala": langC, "zig": langC,
	"lua": langLua, "php": langPHP,
	"md": langMD, "markdown": langMD, "mdx": langMD,
}

// langFor is the language of a fence tag, a file name or an interpreter;
// nil when there's none to highlight.
func langFor(name string) *lang {
	name = strings.ToLower(strings.TrimSpace(name))
	if l, ok := langByName[name]; ok {
		return l
	}
	switch filepath.Base(name) {
	case "makefile", "dockerfile", ".bashrc", ".zshrc", ".profile":
		return langSh
	case "go.mod", "go.sum":
		return langGo
	}
	if ext := filepath.Ext(name); ext != "" {
		return langByName[ext[1:]]
	}
	return nil
}

// hlState carries a block comment or a string from one line to the next.
type hlState struct {
	block bool
	str   string // what closes the string still open
}

// emph is a span of a line (byte offsets) drawn on its own background, as
// the changed words of a diff's line, and the background to return to.
type emph struct {
	from, to int
	on, off  string
}

// highlight draws one line of code in language l over colour base, going
// on from st and leaving st for the next line. Plain text and a nil l come
// back in base.
func highlight(l *lang, st *hlState, s, base string, em *emph) string {
	return paintCode(l, st, s, base, em, false)
}

// paintCode is highlight, drawing each space as · and each tab as → and
// three spaces when marked, in a colour of their own; s keeps its tabs.
func paintCode(l *lang, st *hlState, s, base string, em *emph, marked bool) string {
	var b strings.Builder
	b.Grow(len(s) + 64)
	cur := ""
	inEm := false
	// out writes s[i:j] in colour c, switching the background where the
	// emphasis starts and ends inside it.
	out := func(i, j int, c string) {
		if c == hlComment && base == cOut {
			c = hlOutComment
		}
		for i < j {
			k := j
			if em != nil {
				if on := i >= em.from && i < em.to; on != inEm {
					if on {
						b.WriteString(em.on)
					} else {
						b.WriteString(em.off)
					}
					inEm = on
				}
				if i < em.from && em.from < k {
					k = em.from
				}
				if i < em.to && em.to < k {
					k = em.to
				}
			}
			if marked {
				if ws := strings.IndexAny(s[i:k], " \t"); ws == 0 {
					if cur != hlSpace {
						b.WriteString(hlSpace)
						cur = hlSpace
					}
					if s[i] == '\t' {
						b.WriteString("→   ")
					} else {
						b.WriteString("·")
					}
					i++
					continue
				} else if ws > 0 {
					k = i + ws
				}
			}
			if c != cur {
				b.WriteString(c)
				cur = c
			}
			b.WriteString(s[i:k])
			i = k
		}
	}
	if l == nil {
		out(0, len(s), base)
		return end(&b, inEm, em)
	}
	if l.md {
		markdown(st, s, base, out)
		return end(&b, inEm, em)
	}
	i := 0
	// What the last line left open.
	if st.block {
		if k := strings.Index(s, l.shut); k >= 0 {
			out(0, k+len(l.shut), hlComment)
			i, st.block = k+len(l.shut), false
		} else {
			out(0, len(s), hlComment)
			return end(&b, inEm, em)
		}
	}
	if st.str != "" {
		if k := strings.Index(s, st.str); k >= 0 {
			out(0, k+len(st.str), hlStr)
			i, st.str = k+len(st.str), ""
		} else {
			out(0, len(s), hlStr)
			return end(&b, inEm, em)
		}
	}
	for i < len(s) {
		c := s[i]
		switch {
		case l.line != "" && strings.HasPrefix(s[i:], l.line) && (l.line != "#" || i == 0 || s[i-1] == ' ' || s[i-1] == '\t'),
			l.line2 != "" && strings.HasPrefix(s[i:], l.line2):
			out(i, len(s), hlComment)
			i = len(s)
		case l.open != "" && strings.HasPrefix(s[i:], l.open):
			if k := strings.Index(s[i+len(l.open):], l.shut); k >= 0 {
				end := i + len(l.open) + k + len(l.shut)
				out(i, end, hlComment)
				i = end
			} else {
				out(i, len(s), hlComment)
				st.block, i = true, len(s)
			}
		case l.triple && (strings.HasPrefix(s[i:], `"""`) || strings.HasPrefix(s[i:], `'''`)):
			q := s[i : i+3]
			if k := strings.Index(s[i+3:], q); k >= 0 {
				out(i, i+3+k+3, hlStr)
				i += 3 + k + 3
			} else {
				out(i, len(s), hlStr)
				st.str, i = q, len(s)
			}
		case l.multi != 0 && c == l.multi:
			if k := strings.IndexByte(s[i+1:], c); k >= 0 {
				out(i, i+k+2, hlStr)
				i += k + 2
			} else {
				out(i, len(s), hlStr)
				st.str, i = string(c), len(s)
			}
		case strings.IndexByte(l.quotes, c) >= 0 && !(c == '\'' && i > 0 && isWord(s[i-1]) && l != langSh):
			j := i + 1
			for j < len(s) && s[j] != c {
				if s[j] == '\\' {
					j++
				}
				j++
			}
			j = min(j+1, len(s))
			col := hlStr
			if l.keys && keyAfter(s, j) {
				col = hlFn
			}
			out(i, j, col)
			i = j
		case l.vars && c == '$' && i+1 < len(s) && (isWord(s[i+1]) || s[i+1] == '{'):
			j := i + 1
			if s[j] == '{' {
				if k := strings.IndexByte(s[j:], '}'); k >= 0 {
					j += k + 1
				}
			} else {
				for j < len(s) && isWord(s[j]) {
					j++
				}
			}
			out(i, j, hlFn)
			i = j
		case c >= '0' && c <= '9' && (i == 0 || !isWord(s[i-1])):
			j := i + 1
			for j < len(s) && (isWord(s[j]) || s[j] == '.' && j+1 < len(s) && s[j+1] >= '0' && s[j+1] <= '9') {
				j++
			}
			out(i, j, hlNum)
			i = j
		case isWord(c) || c >= utf8.RuneSelf:
			j := i + 1
			for j < len(s) && (isWord(s[j]) || s[j] >= utf8.RuneSelf || l == langCSS && s[j] == '-') {
				j++
			}
			w := s[i:j]
			col := base
			switch l.words[w] {
			case 'k':
				col = hlKw
			case 't':
				col = hlType
			case 'c':
				col = hlNum
			default:
				switch {
				case l.keys && keyAfter(s, j) && lineStartOrIndent(s, i), next(s, j) == '(':
					col = hlFn
				case l.caps && c >= 'A' && c <= 'Z' && len(w) > 1:
					col = hlType
				}
			}
			out(i, j, col)
			i = j
		default:
			j := i + 1
			for j < len(s) && !isWord(s[j]) && s[j] < utf8.RuneSelf && s[j] != '"' && s[j] != '\'' && s[j] != '`' && s[j] != '/' && s[j] != '#' && s[j] != '$' && s[j] != '-' {
				j++
			}
			out(i, j, base)
			i = j
		}
	}
	return end(&b, inEm, em)
}

// markdown draws a line of Markdown: a heading, a quote, a rule and a
// fenced block whole; a list's mark and a table's pipes quiet; inline code,
// bold and links in the line. st carries a fence from line to line.
func markdown(st *hlState, s, base string, out func(i, j int, c string)) {
	t := strings.TrimLeft(s, " \t")
	ind := len(s) - len(t)
	if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
		switch fence := t[:3]; st.str {
		case "":
			st.str = fence
		case fence:
			st.str = ""
		}
		out(0, len(s), hlComment)
		return
	}
	if st.str != "" {
		out(0, len(s), hlStr)
		return
	}
	h := 0
	for h < len(t) && t[h] == '#' {
		h++
	}
	switch {
	case t == "":
		out(0, len(s), base)
		return
	case h > 0 && h <= 6 && (h == len(t) || t[h] == ' '):
		out(0, len(s), hlKw)
		return
	case t[0] == '>':
		out(0, len(s), hlComment)
		return
	case len(t) >= 3 && strings.Trim(t, string(t[0])+" ") == "" && strings.IndexByte("-*_=", t[0]) >= 0,
		t[0] == '|' && strings.Trim(t, "|-: ") == "":
		out(0, len(s), hlComment) // a rule, a heading's underline, a table's
		return
	}
	i := 0
	if m := listMark(t); m > 0 {
		out(0, ind+m, hlNum)
		i = ind + m
	}
	table := t[0] == '|'
	for i < len(s) {
		c := s[i]
		switch {
		case c == '`':
			n := 1
			for i+n < len(s) && s[i+n] == '`' {
				n++
			}
			j := i + n
			if k := strings.Index(s[j:], s[i:j]); k >= 0 {
				j += k + n
			}
			out(i, j, hlStr)
			i = j
		case c == '*' && i+1 < len(s) && s[i+1] == '*' && strings.Contains(s[i+2:], "**"):
			j := i + 2 + strings.Index(s[i+2:], "**") + 2
			out(i, j, hlType)
			i = j
		case c == '[' && mdLink(s[i:]) > 0:
			k := strings.IndexByte(s[i:], ']')
			j := i + mdLink(s[i:])
			out(i, i+1, hlComment)
			out(i+1, i+k, hlFn)
			out(i+k, j, hlComment)
			i = j
		case c == '|' && table:
			out(i, i+1, hlComment)
			i++
		default:
			j := i + 1
			for j < len(s) && strings.IndexByte("`*[|", s[j]) < 0 {
				j++
			}
			out(i, j, base)
			i = j
		}
	}
}

// listMark is how long a list item's mark is with its space: "- ", "* ",
// "1. ", "2) "; 0 when t isn't one.
func listMark(t string) int {
	if len(t) >= 2 && strings.IndexByte("-*+", t[0]) >= 0 && t[1] == ' ' {
		return 2
	}
	n := 0
	for n < len(t) && n < 9 && t[n] >= '0' && t[n] <= '9' {
		n++
	}
	if n > 0 && n+1 < len(t) && (t[n] == '.' || t[n] == ')') && t[n+1] == ' ' {
		return n + 2
	}
	return 0
}

// mdLink is how long the [text](url) that s starts with is, 0 when it
// doesn't start one.
func mdLink(s string) int {
	k := strings.IndexByte(s, ']')
	if k < 1 || !strings.HasPrefix(s[k:], "](") {
		return 0
	}
	e := strings.IndexByte(s[k:], ')')
	if e < 0 {
		return 0
	}
	return k + e + 1
}

// end closes a highlighted line: the emphasis off, then every style.
func end(b *strings.Builder, inEm bool, em *emph) string {
	if inEm {
		b.WriteString(em.off)
	}
	b.WriteString(reset)
	return b.String()
}

func isWord(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// next is the first byte after s[j:]'s spaces, or 0.
func next(s string, j int) byte {
	for j < len(s) && s[j] == ' ' {
		j++
	}
	if j < len(s) {
		return s[j]
	}
	return 0
}

// keyAfter is whether what ends at j is a key: a : or = follows.
func keyAfter(s string, j int) bool {
	n := next(s, j)
	return n == ':' || n == '='
}

// lineStartOrIndent is whether s[:i] is only indentation or a list's dash.
func lineStartOrIndent(s string, i int) bool {
	for k := 0; k < i; k++ {
		if s[k] != ' ' && s[k] != '\t' && s[k] != '-' {
			return false
		}
	}
	return true
}

// changed is the span of a that differs from b, by their common start and
// end, on UTF-8 boundaries; ok is false when nearly all of it changed, as
// then there's nothing worth pointing out.
func changed(a, b string) (from, to int, ok bool) {
	p := 0
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	for p > 0 && p < len(a) && !utf8.RuneStart(a[p]) {
		p--
	}
	q := 0
	for q < len(a)-p && q < len(b)-p && a[len(a)-1-q] == b[len(b)-1-q] {
		q++
	}
	for q > 0 && len(a)-q < len(a) && !utf8.RuneStart(a[len(a)-q]) {
		q--
	}
	from, to = p, len(a)-q
	if to <= from || p+q == 0 || (p+q)*4 < len(a) {
		return 0, 0, false
	}
	return from, to, true
}

// heredocLang is the language of the heredoc a command line starts: its
// interpreter's (python3 - <<EOF), else the file it writes (cat > x.go).
func heredocLang(line string) *lang {
	toks := fieldsOf(line)
	if len(toks) == 0 {
		return nil
	}
	switch p := filepath.Base(toks[0]); p {
	case "node", "bun", "deno", "tsx", "ts-node":
		return langJS
	case "cat", "tee":
	default:
		if l := langFor(p); l != nil {
			return l
		}
	}
	all := shellTokens(line)
	for i, t := range all {
		t = strings.TrimSpace(t)
		switch {
		case (t == ">" || t == ">>") && i+2 < len(all):
			return langFor(unquote(strings.TrimSpace(all[i+2])))
		case strings.HasPrefix(t, ">") && len(t) > 1 && t != ">>":
			return langFor(unquote(strings.TrimLeft(t, ">")))
		}
	}
	if toks[0] == "tee" && len(toks) > 1 {
		return langFor(unquote(toks[len(toks)-1]))
	}
	return nil
}

// codePrefix is how long the prefix is that a read or a search puts before
// a line of code: "  12→" or "12\t" from a read, "a.go:12:" or "a.go-12-"
// from grep, and the path in it, if any.
func codePrefix(l string) (n int, path string) {
	i := 0
	for i < len(l) && l[i] == ' ' {
		i++
	}
	// path: or path- before the number, as grep writes it; a path can
	// have a - of its own.
	start := i
	word := l[i:]
	if k := strings.IndexAny(word, " \t"); k >= 0 {
		word = word[:k]
	}
	for k := 1; k < len(word); k++ {
		if word[k] != ':' && word[k] != '-' {
			continue
		}
		j := i + k + 1
		m := j
		for m < len(l) && l[m] >= '0' && l[m] <= '9' {
			m++
		}
		if m > j && m < len(l) && (l[m] == ':' || l[m] == '-') && strings.Contains(l[i:i+k], ".") {
			return m + 1, l[i : i+k]
		}
	}
	j := start
	for j < len(l) && l[j] >= '0' && l[j] <= '9' {
		j++
	}
	if j == start {
		return 0, ""
	}
	switch {
	case strings.HasPrefix(l[j:], "→"):
		return j + len("→"), ""
	case j < len(l) && (l[j] == '\t' || l[j] == ':' || l[j] == '-'):
		// grep -n on one file: 12: for a match, 12- for the lines around.
		return j + 1, ""
	}
	return 0, ""
}

// lineNo is the line number a code prefix gives, 0 for none.
func lineNo(prefix string) int {
	prefix = strings.TrimRightFunc(prefix, func(r rune) bool { return r < '0' || r > '9' })
	k := len(prefix)
	for k > 0 && prefix[k-1] >= '0' && prefix[k-1] <= '9' {
		k--
	}
	n, _ := strconv.Atoi(prefix[k:])
	return n
}
