package ui

import (
	"sync"

	tea "charm.land/bubbletea/v2"
)

// Work handed off the UI goroutine: nothing that touches the disk, runs a
// program or dials a socket runs in Update or View. A read whose result is
// drawn starts in the background and is taken in by a later frame; a write
// runs in a Cmd and says how it went when its message lands.

// goOff runs f off the UI goroutine. Tests swap it for one that runs f at
// once, so what they look at after a call has already happened.
var goOff = func(f func()) { go f() }

// cmdOff is how later hands its work to bubbletea: as the Cmd itself,
// which bubbletea runs in a goroutine of its own. Tests swap it for one
// that does the work at once.
var cmdOff = func(f func() tea.Msg) tea.Cmd { return f }

// later is f run off the UI goroutine, as a Cmd; apply takes its result in
// on the UI side when its message lands.
func later[T any](f func() T, apply func(m *Model, v T) tea.Cmd) tea.Cmd {
	return cmdOff(func() tea.Msg {
		v := f()
		return sheetMsg{apply: func(m *Model) tea.Cmd { return apply(m, v) }}
	})
}

// offRead is a value read off the UI goroutine and taken in on it: start
// begins a read unless one is already out, and take hands back what it
// read, once, when it's done. Until then the UI draws what it had.
type offRead[T any] struct {
	mu   sync.Mutex
	busy bool // a read is out, or done and not yet taken
	done bool
	v    T
	wake chan struct{} // closed when the read is done
}

// start runs f in the background unless a read is already out, and
// reports whether it started one.
func (r *offRead[T]) start(f func() T) bool {
	r.mu.Lock()
	if r.busy {
		r.mu.Unlock()
		return false
	}
	r.busy = true
	wake := make(chan struct{})
	r.wake = wake
	r.mu.Unlock()
	goOff(func() {
		v := f()
		r.mu.Lock()
		r.v, r.done = v, true
		r.mu.Unlock()
		close(wake)
	})
	return true
}

// take is the value read, once it's done; after it, a new read can start.
func (r *offRead[T]) take() (T, bool) {
	var zero T
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.done {
		return zero, false
	}
	v := r.v
	r.v, r.done, r.busy, r.wake = zero, false, false, nil
	return v, true
}

// out is whether a read is out and not yet taken.
func (r *offRead[T]) out() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.busy
}

// redraw is a Cmd that lands once the read out now is done, so the frame
// after it draws what was read rather than waiting for the next tick; nil
// when there's no read out.
func (r *offRead[T]) redraw() tea.Cmd {
	r.mu.Lock()
	wake := r.wake
	r.mu.Unlock()
	if wake == nil {
		return nil
	}
	return func() tea.Msg {
		<-wake
		return sheetMsg{apply: func(*Model) tea.Cmd { return nil }}
	}
}

// offReads are offRead by key: one read out at a time for each.
type offReads[K comparable, T any] struct {
	mu sync.Mutex
	m  map[K]*offRead[T]
}

// start reads for k in the background, unless a read for it is out.
func (o *offReads[K, T]) start(k K, f func() T) {
	o.mu.Lock()
	r := o.m[k]
	if r == nil {
		if o.m == nil {
			o.m = map[K]*offRead[T]{}
		}
		r = &offRead[T]{}
		o.m[k] = r
	}
	o.mu.Unlock()
	r.start(f)
}

// take is what was read for k, once it's in.
func (o *offReads[K, T]) take(k K) (T, bool) {
	o.mu.Lock()
	r := o.m[k]
	o.mu.Unlock()
	if r == nil {
		var zero T
		return zero, false
	}
	v, ok := r.take()
	if ok {
		o.mu.Lock()
		delete(o.m, k)
		o.mu.Unlock()
	}
	return v, ok
}

// out is whether a read for k is out.
func (o *offReads[K, T]) out(k K) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.m[k] != nil
}
