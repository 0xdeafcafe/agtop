package ui

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
)

// edit applies one text-editing key to buf with the cursor at pos, and says
// whether the key was an editing key. It gives every input the same keys:
// words with ctrl or alt and the arrows or backspace, the line with
// cmd+backspace or ctrl+u and ctrl+k, and new lines with shift+enter,
// alt+enter or ctrl+j.
func edit(buf []rune, pos int, k tea.KeyPressMsg, s string) ([]rune, int, bool) {
	pos = max(0, min(pos, len(buf)))
	switch s {
	case "left":
		return buf, max(0, pos-1), true
	case "right":
		return buf, min(len(buf), pos+1), true
	case "ctrl+left", "alt+left", "alt+b":
		return buf, wordLeft(buf, pos), true
	case "ctrl+right", "alt+right", "alt+f":
		return buf, wordRight(buf, pos), true
	case "home", "ctrl+a", "super+left":
		return buf, lineStart(buf, pos), true
	case "end", "ctrl+e", "super+right":
		return buf, lineEnd(buf, pos), true
	case "backspace", "ctrl+h", "shift+backspace":
		if pos == 0 {
			return buf, pos, true
		}
		return cut(buf, pos-1, pos), pos - 1, true
	case "delete":
		if pos >= len(buf) {
			return buf, pos, true
		}
		return cut(buf, pos, pos+1), pos, true
	case "ctrl+w", "alt+backspace", "ctrl+backspace":
		from := wordLeft(buf, pos)
		return cut(buf, from, pos), from, true
	case "alt+delete", "ctrl+delete", "alt+d":
		return cut(buf, pos, wordRight(buf, pos)), pos, true
	case "ctrl+u", "super+backspace":
		from := lineStart(buf, pos)
		if from == pos && pos > 0 {
			from = lineStart(buf, pos-1) // at a line's start, join and clear the one above
		}
		return cut(buf, from, pos), from, true
	case "ctrl+k", "super+delete":
		return cut(buf, pos, lineEnd(buf, pos)), pos, true
	case "shift+enter", "alt+enter", "ctrl+j":
		return insert(buf, pos, []rune{'\n'}), pos + 1, true
	}
	if k.Text != "" && k.Mod&^tea.ModShift == 0 {
		r := []rune(k.Text)
		return insert(buf, pos, r), pos + len(r), true
	}
	return buf, pos, false
}

func cut(buf []rune, from, to int) []rune {
	if from >= to {
		return buf
	}
	out := make([]rune, 0, len(buf)-(to-from))
	out = append(out, buf[:from]...)
	return append(out, buf[to:]...)
}

// cleanPaste makes pasted text safe to draw: tabs become spaces (a tab has
// no fixed width, so it broke wrapping), CRLF becomes LF, and other control
// characters are dropped.
func cleanPaste(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var b strings.Builder
	col := 0
	for _, r := range s {
		switch {
		case r == '\t':
			n := 4 - col%4
			b.WriteString(strings.Repeat(" ", n))
			col += n
		case r == '\n':
			b.WriteRune(r)
			col = 0
		case r == '\r' || unicode.IsControl(r):
		default:
			b.WriteRune(r)
			col++
		}
	}
	return b.String()
}

func insert(buf []rune, pos int, r []rune) []rune {
	out := make([]rune, 0, len(buf)+len(r))
	out = append(out, buf[:pos]...)
	out = append(out, r...)
	return append(out, buf[pos:]...)
}

func isWord(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

// wordLeft skips back over any gap, then over the word before it.
func wordLeft(buf []rune, pos int) int {
	for pos > 0 && !isWord(buf[pos-1]) {
		pos--
	}
	for pos > 0 && isWord(buf[pos-1]) {
		pos--
	}
	return pos
}

func wordRight(buf []rune, pos int) int {
	for pos < len(buf) && !isWord(buf[pos]) {
		pos++
	}
	for pos < len(buf) && isWord(buf[pos]) {
		pos++
	}
	return pos
}

func lineStart(buf []rune, pos int) int {
	for pos > 0 && buf[pos-1] != '\n' {
		pos--
	}
	return pos
}

func lineEnd(buf []rune, pos int) int {
	for pos < len(buf) && buf[pos] != '\n' {
		pos++
	}
	return pos
}

// editSel is edit with a selection. anchor is where a selection started
// (-1 for none). shift with the arrows, home and end extends it; typing
// replaces it; backspace and delete remove it. copied is set when ctrl+c
// copies it.
func editSel(buf []rune, pos, anchor int, k tea.KeyPressMsg, s string) (nbuf []rune, npos, nanchor int, copied string, ok bool) {
	pos = max(0, min(pos, len(buf)))
	if anchor > len(buf) {
		anchor = -1
	}
	if strings.HasPrefix(s, "shift+") || strings.HasPrefix(s, "ctrl+shift+") || strings.HasPrefix(s, "alt+shift+") {
		move := strings.NewReplacer("shift+", "").Replace(s)
		switch move {
		case "left", "right", "home", "end", "ctrl+left", "ctrl+right", "alt+left", "alt+right", "super+left", "super+right":
			if anchor < 0 {
				anchor = pos
			}
			_, np, _ := edit(buf, pos, tea.KeyPressMsg{}, move)
			return buf, np, anchor, "", true
		}
	}
	has := anchor >= 0 && anchor != pos
	from, to := min(anchor, pos), max(anchor, pos)
	if has {
		switch {
		case s == "ctrl+c":
			return buf, pos, -1, string(buf[from:to]), true
		case s == "backspace" || s == "delete" || s == "ctrl+h":
			return cut(buf, from, to), from, -1, "", true
		case k.Text != "" && k.Mod&^tea.ModShift == 0:
			nb := insert(cut(buf, from, to), from, []rune(k.Text))
			return nb, from + len([]rune(k.Text)), -1, "", true
		case s == "esc":
			return buf, pos, -1, "", true
		}
	}
	nb, np, used := edit(buf, pos, k, s)
	return nb, np, -1, "", used
}

// cursorPos is where the prompt's cursor sits. It's kept as a distance from
// the end, so anything that replaces the input leaves the cursor at its end.
func (m *Model) cursorPos() int { return max(0, len(m.input)-m.back) }

func (m *Model) setCursor(pos int) { m.back = max(0, len(m.input)-pos) }

// editInput runs an editing key against the main prompt.
func (m *Model) editInput(k tea.KeyPressMsg, s string) bool {
	buf, pos, anchor, copied, ok := editSel(m.input, m.cursorPos(), m.anchor-1, k, s)
	if ok {
		m.input = buf
		m.setCursor(pos)
		m.anchor = anchor + 1
		if copied != "" {
			m.copyText(copied)
		}
	}
	return ok
}

// copyText puts text on the clipboard through the terminal (OSC 52).
func (m *Model) copyText(t string) {
	m.pendingCopy = t
	m.flash(fmt.Sprintf("copied %d characters", len([]rune(t))), false)
}

// extractImages takes the image files named anywhere in text (a dropped or
// typed path, escaped or quoted, or a file:// URL) out of it, returning the
// rest of the text and the images.
func extractImages(text string) (string, []string) {
	var imgs []string
	var b strings.Builder
	last := 0
	for _, sp := range pathSpans(text) {
		p := imagePath(sp.tok)
		if p == "" {
			continue
		}
		imgs = append(imgs, p)
		b.WriteString(text[last:sp.from])
		last = sp.to
		if last < len(text) && text[last] == ' ' {
			last++ // and the space after it
		}
	}
	if imgs == nil {
		return text, nil
	}
	b.WriteString(text[last:])
	return strings.TrimSpace(b.String()), imgs
}

// imagePath is tok as an image file that exists, or "".
func imagePath(tok string) string {
	if rest, ok := strings.CutPrefix(tok, "file://"); ok {
		// Some apps drop a file URL rather than a path.
		if p, err := url.PathUnescape(strings.TrimPrefix(rest, "localhost")); err == nil {
			tok = p
		}
	}
	switch strings.ToLower(filepath.Ext(tok)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
	default:
		return ""
	}
	if strings.HasPrefix(tok, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			tok = filepath.Join(home, tok[2:])
		}
	}
	if st, err := os.Stat(tok); err != nil || st.IsDir() {
		return ""
	}
	return tok
}

type span struct {
	from, to int
	tok      string
}

// pathSpans splits text like splitPaths but keeps where each word was.
func pathSpans(text string) []span {
	var out []span
	var cur strings.Builder
	quote, esc, start := rune(0), false, -1
	flush := func(end int) {
		if start >= 0 {
			out = append(out, span{start, end, cur.String()})
		}
		cur.Reset()
		start = -1
	}
	for i, r := range text {
		plain := r == ' ' || r == '\t' || r == '\n' || r == '\r'
		if start < 0 && plain && quote == 0 && !esc {
			continue
		}
		if start < 0 {
			start = i
		}
		switch {
		case esc:
			cur.WriteRune(r)
			esc = false
		case r == '\\' && quote != '\'':
			esc = true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
		case plain:
			flush(i)
		default:
			cur.WriteRune(r)
		}
	}
	flush(len(text))
	return out
}

// imagePaths reads a paste as image files dropped onto the terminal: paths
// separated by spaces or newlines, quoted or with escaped spaces. It returns
// nil unless every piece is an image file that exists, so ordinary text is
// never swallowed.
func imagePaths(paste string) []string {
	var out []string
	for _, tok := range splitPaths(paste) {
		p := imagePath(tok)
		if p == "" {
			return nil
		}
		out = append(out, p)
	}
	return out
}

// splitPaths splits on unescaped, unquoted whitespace, undoing the escaping
// terminals add when a file is dropped on them.
func splitPaths(s string) []string {
	var out []string
	var cur strings.Builder
	quote := rune(0)
	esc := false
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range strings.TrimSpace(s) {
		switch {
		case esc:
			cur.WriteRune(r)
			esc = false
		case r == '\\' && quote != '\'':
			esc = true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			// Only plain whitespace: macOS puts a narrow no-break space
			// (U+202F) inside screenshot names.
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}

// chips draws attached images above an input box.
func chips(images []string, w int) string {
	if len(images) == 0 {
		return ""
	}
	var parts []string
	for _, p := range images {
		parts = append(parts, paint(cBlue, "▣ ")+paint(cText, filepath.Base(p)))
	}
	return fit("  "+strings.Join(parts, "   ")+dim("   backspace on an empty box removes the last"), w)
}
