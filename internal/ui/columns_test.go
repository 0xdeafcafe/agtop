package ui

import (
	"strings"
	"testing"
)

// A column hidden in Settings leaves the list, and its room goes to names.
func TestHiddenColumns(t *testing.T) {
	m, _ := benchModel(200, 50)
	before := m.nameColumn(200)
	if h := m.columnHeader(200); !strings.Contains(h, "RAM") || !strings.Contains(h, "TIME") {
		t.Fatalf("header without its columns: %q", h)
	}
	m.store.Config.HideColumns = []string{"ram", "time"}
	h := m.columnHeader(200)
	if strings.Contains(h, "RAM") || strings.Contains(h, "TIME") || !strings.Contains(h, "COST") {
		t.Fatalf("hid ram and time, header: %q", h)
	}
	if m.nameColumn(200) < before {
		t.Fatalf("names got narrower: %d, was %d", m.nameColumn(200), before)
	}
}
