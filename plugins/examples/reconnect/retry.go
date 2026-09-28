package main

import (
	"strconv"
	"time"
)

// MaxDelay is the longest backoff between two tries.
const MaxDelay = 10 * time.Minute

// Retryable is whether a session stopped by an error of this kind may be
// continued. A usage limit, a login problem and anything else are left
// alone: sending "continue" wouldn't help and might spend what's left.
func Retryable(kind string) bool { return kind == "offline" || kind == "retryable" }

// Config is what the settings say.
type Config struct {
	Wait  time.Duration // before the first try, and the backoff's base
	Tries int           // continues sent before giving up
	// Alongside is retrying sessions agtop says it's retrying itself.
	Alongside bool
}

// ParseConfig reads the "wait", "tries" and "alongside" settings, falling
// back to their defaults.
func ParseConfig(v map[string]string) Config {
	c := Config{Wait: 15 * time.Second, Tries: 5}
	if d, err := time.ParseDuration(v["wait"]); err == nil && d > 0 {
		c.Wait = d
	}
	if n, err := strconv.Atoi(v["tries"]); err == nil && n > 0 {
		c.Tries = n
	}
	c.Alongside = v["alongside"] == "true"
	return c
}

// Backoff is how long to wait for a turn to start after try n (1-based)
// before the next: Wait, 2×Wait, 4×Wait … up to MaxDelay.
func (c Config) Backoff(n int) time.Duration {
	d := c.Wait
	for i := 1; i < n && d < MaxDelay; i++ {
		d *= 2
	}
	return min(d, MaxDelay)
}

// waiting is a session an error stopped, that the plugin means to continue.
type waiting struct {
	kind  string    // offline or retryable, the last error's
	name  string    // to say which, when giving up
	tries int       // continues sent
	next  time.Time // when to send the next, or give up; zero: wait for the network
}

// Action is something Due says to do now.
type Action struct {
	Session string
	Name    string
	GiveUp  bool // true: say so and forget it; false: send "continue"
	Try     int  // which try this is, from 1
}

// Retrier is the pure state machine: which sessions wait to be continued,
// and when. It does no I/O and reads no clock.
type Retrier struct {
	Config
	up   bool // whether the network answers, as far as events said
	wait map[string]*waiting
}

// NewRetrier starts with the network taken to be up: agtop says when it
// goes down.
func NewRetrier(c Config) *Retrier {
	return &Retrier{Config: c, up: true, wait: map[string]*waiting{}}
}

// Stopped takes a session stopped by an error of kind. One of the kinds
// worth retrying is remembered (keeping the tries it's had, if it stopped
// again after a continue); any other forgets it.
func (r *Retrier) Stopped(id, name, kind string, now time.Time) {
	if !Retryable(kind) {
		delete(r.wait, id)
		return
	}
	w := r.wait[id]
	if w == nil {
		w = &waiting{}
		r.wait[id] = w
	}
	w.kind, w.name = kind, name
	switch {
	case !r.up:
		w.next = time.Time{} // network.up will set it
	case w.tries == 0:
		// Up as far as we know. An offline stop usually comes with a
		// network.down; if it didn't, try after the wait all the same.
		w.next = now.Add(r.Wait)
	default:
		w.next = now.Add(r.Backoff(w.tries))
	}
}

// Started is a turn starting: the session is going again, by our continue
// or anyone's.
func (r *Retrier) Started(id string) { delete(r.wait, id) }

// Forget drops a session without trying it.
func (r *Retrier) Forget(id string) { delete(r.wait, id) }

// Down is the network going away: nothing is tried until it's back, and
// the time waited so far doesn't count against a session.
func (r *Retrier) Down() {
	r.up = false
	for _, w := range r.wait {
		w.next = time.Time{}
	}
}

// Up is the network back: each session waiting is continued now, then
// backs off as before.
func (r *Retrier) Up(now time.Time) {
	r.up = true
	for _, w := range r.wait {
		w.next = now
	}
}

// Due is what to do now: continue each session whose time has come, or
// give it up once it's had every try and the last one's backoff passed
// with no turn started. It advances the state as if each were done.
func (r *Retrier) Due(now time.Time) []Action {
	var out []Action
	for id, w := range r.wait {
		if w.next.IsZero() || now.Before(w.next) {
			continue
		}
		if w.tries >= r.Tries {
			out = append(out, Action{Session: id, Name: w.name, GiveUp: true, Try: w.tries})
			delete(r.wait, id)
			continue
		}
		w.tries++
		w.next = now.Add(r.Backoff(w.tries))
		out = append(out, Action{Session: id, Name: w.name, Try: w.tries})
	}
	return out
}

// Next is when Due next has something to do, or zero.
func (r *Retrier) Next() time.Time {
	var next time.Time
	for _, w := range r.wait {
		if !w.next.IsZero() && (next.IsZero() || w.next.Before(next)) {
			next = w.next
		}
	}
	return next
}

// Waiting is how many sessions wait to be continued.
func (r *Retrier) Waiting() int { return len(r.wait) }
