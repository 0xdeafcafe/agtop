package ui

import (
	"runtime"

	tea "charm.land/bubbletea/v2"
)

// optionKeys are the letters a US Mac keyboard's Option key types in place
// of the letter, when the terminal doesn't send Option as Meta.
var optionKeys = map[string]rune{
	"å": 'a', "∫": 'b', "ç": 'c', "∂": 'd', "ƒ": 'f', "©": 'g', "˙": 'h', "∆": 'j', "˚": 'k',
	"¬": 'l', "µ": 'm', "ø": 'o', "π": 'p', "œ": 'q', "®": 'r', "ß": 's', "†": 't', "√": 'v',
	"∑": 'w', "≈": 'x', "¥": 'y', "Ω": 'z',
}

// macOption is k as the ⌥ key it was on a Mac, so ⌥m picks what starts
// whether or not the terminal sends Option as Meta.
// ponytail: US layout only, and those characters can't be typed into rush
// on a Mac; a setting to turn it off when someone needs them.
func macOption(k tea.KeyPressMsg) tea.KeyPressMsg {
	if runtime.GOOS != "darwin" || k.Mod != 0 {
		return k
	}
	if r, ok := optionKeys[k.Text]; ok {
		return tea.KeyPressMsg{Code: r, Mod: tea.ModAlt}
	}
	return k
}
