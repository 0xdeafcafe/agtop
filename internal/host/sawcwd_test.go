package host

import "testing"

// Only the agent going somewhere else moves its session: a session moved
// elsewhere isn't pulled back to where the agent still works.
func TestSawCwdIsAChange(t *testing.T) {
	var s server
	for i, c := range []struct {
		pid       int
		cwd, want string
	}{
		{1, "/lw", ""},    // where it starts
		{1, "/lw", "/lw"}, // still there: no move
		{1, "/wt", "/lw"}, // went somewhere: a move
		{2, "/wt", ""},    // a new process starts afresh
	} {
		if got := s.sawCwd(c.pid, c.cwd); got != c.want {
			t.Errorf("%d: got %q, want %q", i, got, c.want)
		}
	}
}
