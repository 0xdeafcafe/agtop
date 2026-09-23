package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The key names the view matches on, as Bubble Tea v2 spells them.
func TestKeyNames(t *testing.T) {
	cases := map[string]tea.KeyPressMsg{
		"enter":     {Code: tea.KeyEnter},
		"tab":       {Code: tea.KeyTab},
		"esc":       {Code: tea.KeyEscape},
		"backspace": {Code: tea.KeyBackspace},
		"up":        {Code: tea.KeyUp},
		"pgdown":    {Code: tea.KeyPgDown},
		"ctrl+r":    {Code: 'r', Mod: tea.ModCtrl},
		"ctrl+t":    {Code: 't', Mod: tea.ModCtrl},
		"shift+up":  {Code: tea.KeyUp, Mod: tea.ModShift},
	}
	for want, k := range cases {
		if got := k.String(); got != want {
			t.Errorf("%q printed as %q", want, got)
		}
	}
}

func TestFitAndAge(t *testing.T) {
	if s := fit("abcdef", 4); s != "abc…" {
		t.Fatal(s)
	}
	if s := fit("ab", 4); s != "ab  " {
		t.Fatalf("%q", s)
	}
}

func TestKeyBytesForTheLivePane(t *testing.T) {
	cases := map[string]tea.KeyPressMsg{
		"\r":     {Code: tea.KeyEnter},
		"\x1b[A": {Code: tea.KeyUp},
		"\x03":   {Code: 'c', Mod: tea.ModCtrl},
		"é":      {Code: 'é', Text: "é"},
	}
	for want, k := range cases {
		if got := string(keyBytes(k)); got != want {
			t.Errorf("%q sent %q", want, got)
		}
	}
}
