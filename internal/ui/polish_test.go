package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestComposerDoesNotRepeatSessionTitle(t *testing.T) {
	m, _ := benchModel(160, 45)
	a := m.focused()
	if a == nil {
		t.Fatal("missing focused session")
	}
	c := m.host
	a.DisplayName = "Unique session title"
	out := ansi.Strip(strings.Join(m.paneDock(a, c, 110, 30), "\n"))
	if strings.Contains(out, a.DisplayName) || strings.Contains(out, "to Unique") {
		t.Fatalf("duplicate composer title: %s", out)
	}
}

func TestCatalogDoesNotRepeatNavigationHeading(t *testing.T) {
	for _, w := range []int{80, 140, 220} {
		m := catalogTestModel(t, w, 42, pageProviders)
		out := ansi.Strip(strings.Join(m.dialogBody(w-6), "\n"))
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "Providers") {
				t.Fatalf("repeated Providers title: %s", out)
			}
		}
	}
}
