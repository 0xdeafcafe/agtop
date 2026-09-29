package ui

import (
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
)

// memo keeps what was read off the UI, by key, so a frame can ask for it
// without waiting. The first ask for a key starts the read in a goroutine
// and has nothing yet; a frame after it lands has it. One older than ttl
// is read again in the background, the old one kept meanwhile. Nothing
// waits but the lock, which is never held across a read.
type memo[K comparable, V any] struct {
	mu   sync.Mutex
	ttl  time.Duration // 0 keeps what was read until it's forgotten
	m    map[K]*memoEntry[V]
	read func(K) V
}

type memoEntry[V any] struct {
	v       V
	done    bool // read at least once
	reading bool
	at      time.Time
	gen     int // stale bumps it
	readGen int // the gen the kept value was read at
}

func newMemo[K comparable, V any](ttl time.Duration, read func(K) V) *memo[K, V] {
	return &memo[K, V]{ttl: ttl, m: map[K]*memoEntry[V]{}, read: read}
}

// get is what's kept for k, and whether it has been read yet.
func (c *memo[K, V]) get(k K) (V, bool) {
	c.mu.Lock()
	e := c.m[k]
	if e == nil {
		e = &memoEntry[V]{}
		c.m[k] = e
	}
	start := !e.reading && (!e.done || e.gen != e.readGen || c.ttl > 0 && time.Since(e.at) > c.ttl)
	if start {
		e.reading = true
		gen := e.gen
		c.mu.Unlock()
		goOff(func() { c.fill(k, e, gen) })
		c.mu.Lock()
	}
	v, ok := e.v, e.done
	c.mu.Unlock()
	return v, ok
}

func (c *memo[K, V]) fill(k K, e *memoEntry[V], gen int) {
	v := c.read(k)
	c.mu.Lock()
	defer c.mu.Unlock()
	e.reading = false
	e.v, e.done, e.at, e.readGen = v, true, time.Now(), gen
}

// stale has k read again in the background on its next ask, keeping what
// it has until then.
func (c *memo[K, V]) stale(k K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.m[k]; e != nil {
		e.gen++
	}
}

// forget drops what's kept for k: the next ask reads it afresh.
func (c *memo[K, V]) forget(k K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.m[k]; e != nil && !e.reading {
		delete(c.m, k)
	}
}

// pending is one result worked out off the UI for something open, like a
// sheet, which takes it in when it has landed. wait lands with it, so the
// frame after draws it rather than the next tick's.
type pending[T any] struct {
	ch   chan T
	done chan struct{}
}

func goPending[T any](f func() T) *pending[T] {
	p := &pending[T]{ch: make(chan T, 1), done: make(chan struct{})}
	goOff(func() {
		p.ch <- f()
		close(p.done)
	})
	return p
}

// take is the result once it's landed, without waiting; only the first
// take after it lands has it.
func (p *pending[T]) take() (T, bool) {
	var zero T
	if p == nil {
		return zero, false
	}
	select {
	case v := <-p.ch:
		return v, true
	default:
		return zero, false
	}
}

// wait is a Cmd that lands once the result has.
func (p *pending[T]) wait() tea.Cmd { return p.then(nil) }

// then is a Cmd that lands once the result has, and runs apply on the UI
// side, when there's one.
func (p *pending[T]) then(apply func(m *Model) tea.Cmd) tea.Cmd {
	if p == nil {
		return nil
	}
	return func() tea.Msg {
		<-p.done
		return sheetMsg{apply: func(m *Model) tea.Cmd {
			if apply == nil {
				return nil
			}
			return apply(m)
		}}
	}
}
