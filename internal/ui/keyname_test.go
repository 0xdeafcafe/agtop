package ui

import (
	"strings"
	"testing"

	"github.com/0xdeafcafe/rush/internal/keymap"
)

func TestKeyOfRoundTrips(t *testing.T) {
	for _, a := range keymap.Defaults {
		for _, s := range a.Keys {
			if strings.Contains(s, " ") {
				continue // a chord: its keys arrive one at a time
			}
			k, ok := keyOf(s)
			if !ok || k.String() != s {
				t.Errorf("%s: keyOf(%q) = %q, %v", a.ID, s, k.String(), ok)
			}
		}
	}
	for _, s := range []string{"ctrl+enter", "alt+shift+up", "f5", "super+z", "x"} {
		if k, ok := keyOf(s); !ok || k.String() != s {
			t.Errorf("keyOf(%q) = %q", s, k.String())
		}
	}
}
