package uithread

import "testing"

func TestForbidOnlyOnTheUI(t *testing.T) {
	var got []string
	Breach = func(what string) { got = append(got, what) }
	defer func() { Breach = func(string) {} }()

	Forbid("before") // nothing's the UI yet
	leave := Enter()
	Forbid("on")
	Off(func() { Forbid("handed off") })
	done := make(chan struct{})
	go func() { Forbid("elsewhere"); close(done) }()
	<-done
	leave()
	Forbid("after")

	if len(got) != 1 || got[0] != "on" {
		t.Fatalf("breaches %q, want just on", got)
	}
}

// Marking is paid once per message and once per frame.
func BenchmarkEnter(b *testing.B) {
	for b.Loop() {
		Enter()()
	}
}
