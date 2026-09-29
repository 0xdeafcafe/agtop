package main

import "testing"

// A plugin's output can't drive the terminal: escape sequences and other
// control characters go, newlines and tabs stay.
func TestStripControl(t *testing.T) {
	in := "ok\t1\n\x1b]52;c;aGk=\x07\x1b[2Jdone\x7f\u009b\r"
	if got, want := stripControl(in), "ok\t1\n]52;c;aGk=[2Jdone"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
