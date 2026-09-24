package convo

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Hit is one search result: where it is and the text around the match.
type Hit struct {
	Ref     string // the turn or step it jumps to
	Turn    int
	Who     string // you, claude, or the tool
	Snippet string
}

type query struct {
	words    []string
	kinds    map[string]bool // failed, edit, cmd, read, you, claude
	file     string
	from, to int // turn range; 0 means any
	unknown  []string
}

var knownKinds = map[string]bool{"failed": true, "edit": true, "cmd": true, "read": true, "you": true, "claude": true}

func parseQuery(q string) query {
	p := query{kinds: map[string]bool{}}
	for _, f := range strings.Fields(strings.ToLower(q)) {
		switch {
		case strings.HasPrefix(f, "is:"):
			k := strings.TrimPrefix(f, "is:")
			if !knownKinds[k] {
				p.unknown = append(p.unknown, f)
				continue
			}
			p.kinds[k] = true
		case strings.HasPrefix(f, "file:"):
			p.file = strings.TrimPrefix(f, "file:")
		case strings.HasPrefix(f, "turn:"):
			r := strings.TrimPrefix(f, "turn:")
			a, b, found := strings.Cut(r, "-")
			p.from, _ = strconv.Atoi(a)
			p.to = p.from
			if found {
				p.to, _ = strconv.Atoi(b)
				if b == "" {
					p.to = 1 << 30 // turn:5- runs to the end
				}
			}
			if p.to < p.from {
				p.from, p.to = p.to, p.from
			}
		default:
			p.words = append(p.words, f)
		}
	}
	return p
}

// matches reports whether every word is in text.
func (p query) matches(text string) bool {
	for _, w := range p.words {
		if i, _ := findFold(text, w); i < 0 {
			return false
		}
	}
	return true
}

// findFold finds w in s ignoring case, rune by rune, and returns the byte
// span of the match in s itself (lowercasing can change a string's byte
// length, so an index into a lowercased copy doesn't fit the original).
func findFold(s, w string) (start, end int) {
	if w == "" {
		return 0, 0
	}
	for i := range s {
		j, k := i, 0
		for k < len(w) && j < len(s) {
			a, na := utf8.DecodeRuneInString(s[j:])
			b, nb := utf8.DecodeRuneInString(w[k:])
			if unicode.ToLower(a) != unicode.ToLower(b) {
				break
			}
			j, k = j+na, k+nb
		}
		if k == len(w) {
			return i, j
		}
	}
	return -1, -1
}

// Search finds turns and steps that match q: plain words anywhere, narrowed
// by is:failed, is:edit, is:cmd, is:read, is:you, is:claude, file:<part of
// a path> and turn:12 or turn:10-13.
func (s *Session) Search(q string) []Hit {
	p := parseQuery(q)
	if len(p.words) == 0 && len(p.kinds) == 0 && p.file == "" && p.from == 0 {
		return nil
	}
	steps := p.kinds["failed"] || p.kinds["edit"] || p.kinds["cmd"] || p.kinds["read"] || p.file != ""
	var hits []Hit
	for _, t := range s.Turns {
		if p.from > 0 && (t.N < p.from || t.N > p.to) {
			continue
		}
		ref := fmt.Sprintf("t%d", t.N)
		if !steps && !p.kinds["claude"] && p.matches(t.Prompt) && (len(p.words) > 0 || p.kinds["you"] || p.from > 0) {
			hits = append(hits, Hit{Ref: ref, Turn: t.N, Who: "you", Snippet: snippet(t.Prompt, p.words)})
		}
		if p.kinds["you"] {
			continue
		}
		for _, it := range t.Items {
			switch it.Kind {
			case KText:
				if steps || len(p.words) == 0 && !p.kinds["claude"] || !p.matches(it.Text) {
					continue
				}
				hits = append(hits, Hit{Ref: ref, Turn: t.N, Who: "claude", Snippet: snippet(it.Text, p.words)})
			case KStep:
				if p.kinds["claude"] {
					continue
				}
				for _, st := range append([]*Step{it.Step}, it.Step.Children...) {
					if h, ok := p.step(st, ref); ok {
						h.Turn = t.N
						hits = append(hits, h)
					}
				}
			}
		}
	}
	return hits
}

func (p query) step(st *Step, turnRef string) (Hit, bool) {
	if hidden(st) {
		return Hit{}, false
	}
	in := readInput(st.Input)
	g := glyphFor(st.Tool)
	switch {
	case p.kinds["failed"] && st.Status != Failed:
		return Hit{}, false
	case p.kinds["edit"] && g != "✎":
		return Hit{}, false
	case p.kinds["cmd"] && g != "$":
		return Hit{}, false
	case p.kinds["read"] && g != "◧":
		return Hit{}, false
	case p.file != "" && !strings.Contains(strings.ToLower(in.str("file_path")+" "+in.str("path")+" "+in.str("command")), p.file):
		return Hit{}, false
	}
	label := firstNonEmpty(in.str("description"), in.str("command"), in.str("file_path"), in.str("pattern"), in.str("url"), in.str("query"), st.Tool)
	hay := label + "\n" + in.str("command") + "\n" + in.str("file_path") + "\n" + st.Output
	if !p.matches(hay) {
		return Hit{}, false
	}
	snip := oneLine(label)
	if len(p.words) > 0 && !p.matches(label) {
		snip = oneLine(label) + " · " + snippet(st.Output, p.words)
	}
	return Hit{Ref: turnRef + ":s:" + st.ID, Who: st.Tool, Snippet: snip}, true
}

// snippet is the text around the first word found, on one line.
func snippet(text string, words []string) string {
	t := oneLine(text)
	if len(words) == 0 {
		return t
	}
	i, _ := findFold(t, words[0])
	if i < 0 {
		return t
	}
	// About 30 characters before it, starting on a whole character.
	start := i
	for n := 0; n < 30 && start > 0; n++ {
		_, size := utf8.DecodeLastRuneInString(t[:start])
		start -= size
	}
	out := t[start:]
	if start > 0 {
		out = "…" + out
	}
	return out
}

// SearchView draws the hits as rows, with the first word lit.
func (s *Session) SearchView(q string, o Options) []Line {
	w := min(o.Width, o.rowCap())
	// The same query over an unchanged session gives the same hits.
	key := fmt.Sprint(q, "\x00", s.stepVer, len(s.Turns), s.Last.UnixNano())
	if s.searchKey != key {
		s.searchHits, s.searchKey = s.Search(q), key
	}
	hits := s.searchHits
	var out []Line
	head := fmt.Sprintf("%d matches", len(hits))
	if strings.TrimSpace(q) == "" {
		head = "type to search · is:failed is:edit is:cmd is:read is:you is:claude file:<name> turn:10-13"
	}
	if u := parseQuery(q).unknown; len(u) > 0 {
		head += " · " + strings.Join(u, " ") + " isn't a filter (is:failed is:edit is:cmd is:read is:you is:claude)"
	}
	out = append(out, Line{Text: row("", "  "+dim(head), dim("enter jumps · esc closes"), o.Width, w)}, Line{Text: ""})
	words := parseQuery(q).words
	for _, h := range hits {
		who := dim(fmt.Sprintf("%-8s", h.Who))
		if h.Who == "you" {
			who = paint(cWhite, fmt.Sprintf("%-8s", h.Who))
		}
		snip := h.Snippet
		if len(words) > 0 {
			if i, j := findFold(snip, words[0]); i >= 0 {
				snip = sub(snip[:i]) + paint(cYellow+bold, snip[i:j]) + sub(snip[j:])
			} else {
				snip = sub(snip)
			}
		} else {
			snip = sub(snip)
		}
		left := "  " + dim(fmt.Sprintf("#%-4d", h.Turn)) + " " + who + " " + snip
		b := ""
		if h.Ref == o.Selected {
			b = bgSelU
			if o.Focused {
				b = bgSel
			}
		}
		out = append(out, Line{Text: row(b, left, "", o.Width, w), Ref: h.Ref})
	}
	return out
}

// ParentRef is the ref of the step a subagent's step belongs to, or "" when
// ref isn't a subagent's step. Opening it shows the step.
func (s *Session) ParentRef(ref string) string {
	turn, id, ok := strings.Cut(ref, ":s:")
	if !ok {
		return ""
	}
	if st := s.byID[id]; st != nil && st.parent != nil {
		return turn + ":s:" + st.parent.ID
	}
	return ""
}
