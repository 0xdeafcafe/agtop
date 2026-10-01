package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A provider's page lists the models it offers, each with its context
// window and price where rush knows them.
func TestProviderPageListsModels(t *testing.T) {
	m, _ := benchModel(160, 40)
	text := ansi.Strip(strings.Join(m.canDo(string(loginsKind), 120), "\n"))
	if !strings.Contains(text, "Models") || !strings.Contains(text, "opus") || !strings.Contains(text, "$") {
		t.Errorf("no models with prices:\n%s", text)
	}
	t.Log(text[strings.Index(text, "Models"):])
}
