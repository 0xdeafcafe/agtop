package ui

import (
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/convo"
)

// pastes keeps the long pastes a box shows as chips; the text goes out in
// full when the message is sent.
type pastes struct {
	n    int
	text map[int]string
}

var pasteRe = regexp.MustCompile(`\[Pasted text #(\d+) \+\d+ lines\]`)

// isLongPaste is a paste worth folding into a chip.
func isLongPaste(s string) bool { return strings.Count(s, "\n") >= 3 || len(s) > 800 }

func chipFor(id int, text string) string {
	return fmt.Sprintf("[Pasted text #%d +%d lines]", id, strings.Count(strings.TrimRight(text, "\n"), "\n")+1)
}

// add keeps text and returns the chip that stands for it.
func (p *pastes) add(text string) string {
	if p.text == nil {
		p.text = map[int]string{}
	}
	p.n++
	p.text[p.n] = text
	return chipFor(p.n, text)
}

// expand puts the pasted text back in place of each chip. Tagged, each
// goes between <pasted_content> tags as Claude Code sends a paste: Claude
// knows the words were pasted, and the conversation shows it as its chip.
func (p *pastes) expand(s string, tagged bool) string {
	if len(p.text) == 0 {
		return s
	}
	return pasteRe.ReplaceAllStringFunc(s, func(chip string) string {
		id, _ := strconv.Atoi(pasteRe.FindStringSubmatch(chip)[1])
		t, ok := p.text[id]
		if !ok {
			return chip
		}
		if tagged {
			tag := fmt.Sprintf(`id="%04x"`, rand.IntN(0x10000))
			return "\n\n<pasted_content " + tag + ">\n" + strings.TrimRight(t, "\n") + "\n</pasted_content " + tag + ">\n"
		}
		return t
	})
}

// unfold is a sent message back in a box: each tagged paste a chip again.
func (p *pastes) unfold(s string) []rune {
	return []rune(strings.TrimSpace(convo.EachPaste(s, p.add)))
}

// lastIn is the id of the last chip in the draft, 0 if there is none.
func (p *pastes) lastIn(buf []rune) int {
	ms := pasteRe.FindAllStringSubmatch(string(buf), -1)
	for i := len(ms) - 1; i >= 0; i-- {
		if id, _ := strconv.Atoi(ms[i][1]); p.text[id] != "" {
			return id
		}
	}
	return 0
}

// dropChip deletes a whole chip when backspace lands on its end.
func dropChip(buf []rune, pos int) ([]rune, int, bool) {
	if pos == 0 || buf[pos-1] != ']' {
		return buf, pos, false
	}
	before := string(buf[:pos])
	loc := pasteRe.FindAllStringIndex(before, -1)
	if len(loc) == 0 || loc[len(loc)-1][1] != len(before) {
		return buf, pos, false
	}
	from := len([]rune(before[:loc[len(loc)-1][0]]))
	out := append(append([]rune{}, buf[:from]...), buf[pos:]...)
	return out, from, true
}

// editedMsg carries text back from $EDITOR: into paste id, or (id 0) the
// whole draft, of the Session's box or the main one.
type editedMsg struct {
	pane bool
	id   int
	text string
	err  error
}

// editInEditor opens text in $VISUAL or $EDITOR and sends back the result.
func editInEditor(text string, pane bool, id int) tea.Cmd {
	f, err := os.CreateTemp("", "agtop-*.md")
	if err != nil {
		return func() tea.Msg { return editedMsg{err: err} }
	}
	_, _ = f.WriteString(text)
	_ = f.Close()
	ed := firstNonEmpty(os.Getenv("VISUAL"), os.Getenv("EDITOR"), "vi")
	c := exec.Command("sh", "-c", ed+` "$1"`, "sh", f.Name())
	return tea.ExecProcess(c, func(err error) tea.Msg {
		defer os.Remove(f.Name())
		b, rerr := os.ReadFile(f.Name())
		if err == nil {
			err = rerr
		}
		return editedMsg{pane: pane, id: id, text: strings.TrimRight(string(b), "\n"), err: err}
	})
}

// editDraft is ctrl+g: the last paste in the draft, or the whole draft.
func editDraft(p *pastes, buf []rune, pane bool) tea.Cmd {
	if id := p.lastIn(buf); id != 0 {
		return editInEditor(p.text[id], pane, id)
	}
	return editInEditor(string(buf), pane, 0)
}

// applyEdit puts what came back from the editor into a box.
func applyEdit(p *pastes, buf []rune, id int, text string) []rune {
	if id == 0 {
		return []rune(text)
	}
	old := p.text[id]
	p.text[id] = text
	return []rune(strings.Replace(string(buf), chipFor(id, old), chipFor(id, text), 1))
}
