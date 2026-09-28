//go:build darwin

package fswait

import (
	"sync"

	"golang.org/x/sys/unix"
)

// Watcher notices writes to a set of files and folders: a file written to,
// grown, replaced or removed, or a folder gaining, losing or renaming an
// entry. The kernel queues what happened, so asking costs one system call
// and nothing is missed between asks.
type Watcher struct {
	mu    sync.Mutex
	kq    int
	fds   map[string]int
	dirty bool // something changed since the last ask, or it can't tell
}

// NewWatcher is a watcher of nothing yet; one that can't be made reports
// every ask as a change.
func NewWatcher() *Watcher {
	w := &Watcher{kq: -1, fds: map[string]int{}, dirty: true}
	if kq, err := unix.Kqueue(); err == nil {
		unix.CloseOnExec(kq)
		w.kq = kq
	}
	return w
}

// Watch makes paths the set watched, keeping those already watched. One
// that isn't there yet is skipped: watch its folder to see it arrive.
func (w *Watcher) Watch(paths []string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.kq < 0 {
		return
	}
	want := make(map[string]bool, len(paths))
	for _, p := range paths {
		want[p] = true
	}
	for p, fd := range w.fds {
		if !want[p] {
			unix.Close(fd) // closing it drops its registration
			delete(w.fds, p)
		}
	}
	var changes []unix.Kevent_t
	for p := range want {
		if _, ok := w.fds[p]; ok {
			continue
		}
		fd, err := unix.Open(p, unix.O_EVTONLY|unix.O_CLOEXEC, 0)
		if err != nil {
			continue
		}
		w.fds[p] = fd
		var ev unix.Kevent_t
		unix.SetKevent(&ev, fd, unix.EVFILT_VNODE, unix.EV_ADD|unix.EV_CLEAR)
		ev.Fflags = unix.NOTE_WRITE | unix.NOTE_EXTEND | unix.NOTE_DELETE | unix.NOTE_RENAME | unix.NOTE_ATTRIB
		changes = append(changes, ev)
	}
	if len(changes) > 0 {
		if _, err := unix.Kevent(w.kq, changes, nil, nil); err != nil {
			w.dirty = true
		}
	}
}

// Changed reports whether anything watched changed since the last ask.
func (w *Watcher) Changed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.kq < 0 {
		return true
	}
	changed := w.dirty
	w.dirty = false
	events := make([]unix.Kevent_t, 32)
	var zero unix.Timespec
	for {
		n, err := unix.Kevent(w.kq, nil, events, &zero)
		if err != nil && err != unix.EINTR {
			return true
		}
		if n > 0 {
			changed = true
		}
		for i := range n {
			if events[i].Fflags&(unix.NOTE_DELETE|unix.NOTE_RENAME) != 0 {
				// Gone or replaced: watch whatever is there now.
				for p, fd := range w.fds {
					if uint64(fd) == uint64(events[i].Ident) {
						unix.Close(fd)
						delete(w.fds, p)
					}
				}
			}
		}
		if n < len(events) {
			return changed
		}
	}
}

// Close stops watching.
func (w *Watcher) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for p, fd := range w.fds {
		unix.Close(fd)
		delete(w.fds, p)
	}
	if w.kq >= 0 {
		unix.Close(w.kq)
		w.kq = -1
	}
}
