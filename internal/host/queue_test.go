package host

import "testing"

func TestJoinQueue(t *testing.T) {
	if got := JoinQueue([]string{"only"}); got != "only" {
		t.Errorf("one message: got %q", got)
	}
	want := "2 messages, queued while you worked. Each is its own message; take them in order.\n\n[Message 1 of 2]\nfix the test\n\n[Message 2 of 2]\nthen commit"
	if got := JoinQueue([]string{"fix the test", "then commit"}); got != want {
		t.Errorf("two messages:\ngot  %q\nwant %q", got, want)
	}
}
