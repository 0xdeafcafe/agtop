package ui

import (
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// keyCodes is each named key's code, read off bubbletea itself so a name
// always gives back the key that prints as it.
var keyCodes = func() map[string]rune {
	out := map[string]rune{}
	add := func(c rune) {
		if n := (tea.Key{Code: c}).String(); n != "" && utf8.RuneCountInString(n) > 1 {
			out[n] = c
		}
	}
	for _, c := range []rune{tea.KeyEnter, tea.KeyTab, tea.KeyBackspace, tea.KeyEscape, tea.KeySpace} {
		add(c)
	}
	for c := tea.KeyExtended + 1; c < tea.KeyExtended+512; c++ {
		add(c)
	}
	return out
}()

// keyOf is the key press whose String is s, as the keymap names it: how a
// key you bound stands in for the default key agtop's handling expects.
func keyOf(s string) (tea.KeyPressMsg, bool) {
	var k tea.Key
	rest := s
	for {
		switch {
		case strings.HasPrefix(rest, "ctrl+") && len(rest) > 5:
			k.Mod |= tea.ModCtrl
			rest = rest[5:]
			continue
		case strings.HasPrefix(rest, "alt+") && len(rest) > 4:
			k.Mod |= tea.ModAlt
			rest = rest[4:]
			continue
		case strings.HasPrefix(rest, "shift+") && len(rest) > 6:
			k.Mod |= tea.ModShift
			rest = rest[6:]
			continue
		case strings.HasPrefix(rest, "super+") && len(rest) > 6:
			k.Mod |= tea.ModSuper
			rest = rest[6:]
			continue
		}
		break
	}
	if c, ok := keyCodes[rest]; ok {
		k.Code = c
		if c == tea.KeySpace && k.Mod == 0 {
			k.Text = " "
		}
	} else if r, n := utf8.DecodeRuneInString(rest); n == len(rest) && r != utf8.RuneError {
		k.Code = r
		if k.Mod&^tea.ModShift == 0 {
			k.Text = rest
		}
	} else {
		return tea.KeyPressMsg{}, false
	}
	return tea.KeyPressMsg(k), k.String() == s
}
