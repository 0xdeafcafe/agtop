package convo

// Highlighter colours a file line by line in the language its name says,
// carrying strings and block comments from one line to the next.
type Highlighter struct {
	l  *lang
	st hlState
}

// NewHighlighter is a highlighter for a file or fence tag name; nil when
// there's no language to highlight.
func NewHighlighter(name string) *Highlighter {
	l := langFor(name)
	if l == nil {
		return nil
	}
	return &Highlighter{l: l}
}

// Line is the next line, coloured over base. Its text is unchanged: only
// colour is added, and a reset at the end.
func (h *Highlighter) Line(s, base string) string { return highlight(h.l, &h.st, s, base, nil) }
