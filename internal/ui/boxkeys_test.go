package ui

import (
	"slices"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/fleet"
)

// The box's hints say what enter does, and how to get back what was
// cleared, while it can be.
func TestBoxKeys(t *testing.T) {
	m, _ := benchModel(200, 50)
	c, a := &hostConn{key: "k"}, &fleet.Agent{}
	has := func(pairs []string, words string) bool { return slices.Contains(pairs, words) }

	c.input = []rune("hello")
	if k := m.boxKeys(c, a, nil); !has(k, "send") || has(k, "stash") {
		t.Fatalf("typing, with no stash running: %q", k)
	}
	m.hooks = stashRunning("")
	if k := m.boxKeys(c, a, nil); !has(k, "send") || !has(k, "stash") || !has(k, "stashed · sent · cleared") {
		t.Fatalf("typing: %q", k)
	}
	c.input, c.clearedAt = nil, time.Now()
	if k := m.boxKeys(c, a, nil); !has(k, "bring back what you cleared") || has(k, "send") {
		t.Fatalf("just cleared: %q", k)
	}
	c.clearedAt = time.Now().Add(-time.Hour)
	if k := m.boxKeys(c, a, nil); has(k, "bring back what you cleared") {
		t.Fatalf("long after clearing: %q", k)
	}
}
