package ui

import (
	"encoding/json/jsontext"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/0xdeafcafe/rush/internal/adapters/claude/headless"
	"github.com/0xdeafcafe/rush/internal/convo"
	"github.com/0xdeafcafe/rush/internal/fleet"
	"github.com/0xdeafcafe/rush/internal/host"
	"github.com/0xdeafcafe/rush/internal/jsonx"
	"github.com/0xdeafcafe/rush/internal/state"
)

const memNote = `---
name: real-db-in-tests
description: Run the real db in integration tests, never mocks
metadata:
  type: feedback
---

Integration tests hit a real database. Mocked tests passed last quarter while the prod migration failed.
**Why:** a mock hid a broken migration.
**How to apply:** any test under internal/store.

See [[testing-policy]].
`

// memAsk has the pane's session ask to run tool with in.
func memAsk(t *testing.T, tool string, in map[string]any) (*Model, *hostConn) {
	t.Helper()
	b, _ := jsonx.Marshal(in)
	c := &hostConn{kind: "claude", key: "k", client: &host.Client{}, sess: convo.New(), open: map[string]bool{}}
	m := &Model{snap: &fleet.Snapshot{}, store: &state.Store{}, host: c, paneFocus: true}
	c.sess.Apply(host.Sent{Text: "go"}, time.Now())
	c.sess.Apply(headless.Message{Role: "assistant", Blocks: []headless.Block{{Type: "tool_use", ID: "b1", Name: tool, Input: jsontext.Value(b)}}}, time.Now())
	c.sess.Apply(headless.PermissionRequest{ID: "r1", Tool: tool, ToolUseID: "b1", Input: jsontext.Value(b)}, time.Now())
	return m, c
}

func memDir(t *testing.T) string {
	dir := filepath.Join(t.TempDir(), "projects", "-src-app", "memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"MEMORY.md", "a.md", "b.md"} {
		_ = os.WriteFile(filepath.Join(dir, n), []byte("x\n"), 0o644)
	}
	return dir
}

// A memory note to write gets its own card: the note as it reads, with no
// frontmatter, its type and scope, and buttons; enter remembers it.
func TestMemoryCard(t *testing.T) {
	path := filepath.Join(memDir(t), "real-db.md")
	m, c := memAsk(t, "Write", map[string]any{"file_path": path, "content": memNote})
	a := &fleet.Agent{DisplayName: "x"}
	rows := m.cardRows(a, c, 78, 40)
	out := ansi.Strip(strings.Join(rows, "\n"))
	for _, w := range []string{"Remember this?", "feedback · this project", "Run the real db in integration tests, never mocks",
		"Why: a mock hid a broken migration.", "[[testing-policy]]", "new · 1 of 3 memories",
		"↵ Remember", "e Edit it first", "n Skip", "a Always ok"} {
		if !strings.Contains(out, w) {
			t.Errorf("card missing %q:\n%s", w, out)
		}
	}
	if strings.Contains(out, "---") || strings.Contains(out, "metadata:") {
		t.Errorf("the frontmatter shouldn't show:\n%s", out)
	}
	if len(c.btns) != 4 {
		t.Fatalf("want 4 buttons to click, got %d", len(c.btns))
	}

	// An edit of it is an update, drawn as what changes.
	_ = os.WriteFile(path, []byte(memNote), 0o644)
	m2, c2 := memAsk(t, "Edit", map[string]any{"file_path": path, "old_string": "a mock hid a broken migration.", "new_string": "a mock hid a broken migration in March."})
	up := ansi.Strip(strings.Join(m2.cardRows(a, c2, 78, 40), "\n"))
	for _, w := range []string{"Update this memory?", "− Why: a mock hid a broken migration.", "+ Why: a mock hid a broken migration in March.", "update · 1 of 3 memories"} {
		if !strings.Contains(up, w) {
			t.Errorf("update card missing %q:\n%s", w, up)
		}
	}
	if p := os.Getenv("RUSH_MEMCARD_PREVIEW"); p != "" {
		_ = os.WriteFile(p, []byte(out+"\n\n"+up+"\n"), 0o644)
	}

	// Plain letters answer only once the card has the keys; enter allows.
	m.paneKey(tea.KeyPressMsg{}, "up")
	if cmd := m.paneKey(tea.KeyPressMsg{}, "enter"); cmd == nil || c.cardFocus {
		t.Fatal("enter should allow the note")
	}
}

// Any other file keeps the plain card.
func TestPlainWriteCard(t *testing.T) {
	m, c := memAsk(t, "Write", map[string]any{"file_path": "/tmp/notes.md", "content": "hello\n"})
	out := ansi.Strip(strings.Join(m.cardRows(&fleet.Agent{}, c, 78, 40), "\n"))
	if strings.Contains(out, "Remember this?") || !strings.Contains(out, "change a file") {
		t.Errorf("a plain file should keep the plain card:\n%s", out)
	}
}
