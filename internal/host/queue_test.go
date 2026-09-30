package host

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJoinQueue(t *testing.T) {
	if got := JoinQueue([]string{"only"}); got != "only" {
		t.Errorf("one message: got %q", got)
	}
	want := "2 messages, queued while you worked. Each is its own message; take them in order.\n\n[Message 1 of 2]\nfix the test\n\n[Message 2 of 2]\nthen commit"
	if got := JoinQueue([]string{"fix the test", "then commit"}); got != want {
		t.Errorf("two messages:\ngot  %q\nwant %q", got, want)
	}
}

func TestQueueCut(t *testing.T) {
	for _, c := range []struct {
		q    []string
		sep  bool
		want int
	}{
		{[]string{"a", "b"}, false, 2},
		{[]string{"a", "b"}, true, 1},
		{[]string{"/compact", "then this"}, false, 1},
		{[]string{"a", "/compact", "b"}, false, 1},
		{[]string{"/Users/lw/x is broken", "b"}, false, 2},
	} {
		if got := queueCut(c.q, c.sep); got != c.want {
			t.Errorf("queueCut(%q, %v) = %d, want %d", c.q, c.sep, got, c.want)
		}
	}
}

// A message for a subagent is handed over at its next tool call, to it
// alone, and only once.
func TestInbox(t *testing.T) {
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "a1"), []byte("use the v2 API\n\n"), 0o600)
	var out bytes.Buffer
	if err := Inbox(d, strings.NewReader(`{"hook_event_name":"PostToolUse"}`), &out); err != nil || out.Len() != 0 {
		t.Fatalf("the main session's call took it: %q %v", out.String(), err)
	}
	if err := Inbox(d, strings.NewReader(`{"hook_event_name":"PostToolUse","agent_id":"a1","tool_name":"Bash"}`), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"hookEventName":"PostToolUse"`) || !strings.Contains(out.String(), "use the v2 API") {
		t.Fatalf("got %s", out.String())
	}
	out.Reset()
	Inbox(d, strings.NewReader(`{"hook_event_name":"PostToolUse","agent_id":"a1"}`), &out)
	if out.Len() != 0 {
		t.Fatalf("handed over twice: %s", out.String())
	}
}
