package ui

import (
	"runtime"
	"strings"

	"github.com/0xdeafcafe/rush/internal/cellw"
	"github.com/0xdeafcafe/rush/internal/keymap"
)

// kbKey is a key on the drawn keyboard: its name as keymap spells it, what
// its cap says, and how wide the cap is.
type kbKey struct {
	name, label string
	w           int
}

// kbRows are the keyboard's rows, each staggered as a real one's are.
var kbRows = func() [][]kbKey {
	plain := func(ks string) []kbKey {
		var out []kbKey
		for _, k := range strings.Split(ks, " ") {
			out = append(out, kbKey{k, k, 3})
		}
		return out
	}
	alt, super := "alt", "super"
	if runtime.GOOS == "darwin" {
		alt, super = "⌥", "⌘"
	}
	cat := func(parts ...[]kbKey) []kbKey {
		var out []kbKey
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	return [][]kbKey{
		{{"esc", "esc", 5}},
		cat(plain("` 1 2 3 4 5 6 7 8 9 0 - ="), []kbKey{{"backspace", "⌫", 6}}),
		cat([]kbKey{{"tab", "tab", 5}}, plain("q w e r t y u i o p [ ]"), []kbKey{{`\`, `\`, 4}}),
		cat([]kbKey{{"", "", 6}}, plain("a s d f g h j k l ; '"), []kbKey{{"enter", "enter", 7}}),
		cat([]kbKey{{"shift", "shift", 8}}, plain("z x c v b n m , . /"), []kbKey{{"shift", "shift", 9}}),
		{{"ctrl", "ctrl", 5}, {"alt", alt, 3}, {"super", super, 5}, {"space", "space", 17}, {"super", super, 5}, {"alt", alt, 3}, {"left", "←", 3}, {"up", "↑", 3}, {"down", "↓", 3}, {"right", "→", 3}},
	}
}()

// kbWide is the keyboard's width in cells.
const kbWide = 58

// shifted is the key each shifted character is typed on.
var shifted = map[string]string{
	"~": "`", "!": "1", "@": "2", "#": "3", "$": "4", "%": "5", "^": "6", "&": "7", "*": "8", "(": "9", ")": "0",
	"_": "-", "+": "=", "{": "[", "}": "]", "|": `\`, ":": ";", `"`: "'", "<": ",", ">": ".", "?": "/",
}

// keyParts are the keyboard's keys a binding is pressed with, a chord's
// keys all together.
func keyParts(seq keymap.Seq) map[string]bool {
	out := map[string]bool{}
	for _, k := range seq {
		base, mods := k, ""
		if i := strings.LastIndex(k[:max(0, len(k)-1)], "+"); i >= 0 {
			base, mods = k[i+1:], k[:i]
		}
		for _, mod := range strings.Split(mods, "+") {
			out[mod] = mods != ""
		}
		switch {
		case shifted[base] != "":
			out["shift"], base = true, shifted[base]
		case len(base) == 1 && base != strings.ToLower(base):
			out["shift"], base = true, strings.ToLower(base)
		}
		out[base] = true
	}
	delete(out, "")
	return out
}

// keyboard draws the keyboard with lit's keys lit, used's plainly and the
// rest faint, so a binding shows as where your fingers go.
func keyboard(lit, used map[string]bool) []string {
	out := make([]string, 0, len(kbRows))
	for _, row := range kbRows {
		caps := make([]string, 0, len(row))
		for _, k := range row {
			pad := k.w - cellw.String(k.label)
			txt := strings.Repeat(" ", pad/2) + k.label + strings.Repeat(" ", pad-pad/2)
			switch {
			case lit[k.name]:
				caps = append(caps, qCapOn+qInk+bold+txt+reset)
			case used[k.name]:
				caps = append(caps, qCap+cText+txt+reset)
			default:
				caps = append(caps, qCap+cFaint+txt+reset)
			}
		}
		out = append(out, strings.Join(caps, " "))
	}
	return out
}
