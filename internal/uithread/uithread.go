// Package uithread knows when the UI's goroutine is busy with a message or
// a frame, so what must never run there (the disk, a socket, a plugin,
// another process) can say so when it does, by name, instead of being
// found later by feel.
package uithread

import (
	"runtime"
	"strconv"
	"sync/atomic"
)

// busy is the UI's goroutine while it handles a message or draws a frame;
// zero between, and in every process but the view.
var busy atomic.Int64

// Breach is told what ran on the UI's goroutine that mustn't. The UI sets
// it: a test fails, a real run notes it in stalls.log.
var Breach = func(what string) {}

// Enter marks the calling goroutine as the UI's until the returned func.
func Enter() (leave func()) {
	was := busy.Swap(goid())
	return func() { busy.Store(was) }
}

// Off runs f as work handed off the UI would run: tests that run it at
// once, on the UI's goroutine, use it so f isn't taken for the UI's own.
func Off(f func()) {
	was := busy.Swap(0)
	defer busy.Store(was)
	f()
}

// Forbid is called by what waits: on the UI's goroutine, it's a breach.
func Forbid(what string) {
	if b := busy.Load(); b != 0 && goid() == b {
		Breach(what)
	}
}

// goid is the calling goroutine's id, from the top line of its stack:
// "goroutine 12 [running]:". A microsecond, so only when marking or
// checking.
func goid() int64 {
	var buf [32]byte
	s := buf[len("goroutine "):runtime.Stack(buf[:], false)]
	for i, c := range s {
		if c == ' ' {
			s = s[:i]
			break
		}
	}
	id, _ := strconv.ParseInt(string(s), 10, 64)
	return id
}
