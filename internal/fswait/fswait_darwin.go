//go:build darwin

package fswait

import (
	"time"

	"golang.org/x/sys/unix"
)

// Grown blocks until one of files differs in size from what was read, or
// stop closes; it reports which. The kernel says when a file is written, so
// a quiet transcript costs nothing while it's watched.
func Grown(stop <-chan struct{}, files []File) bool {
	kq, err := unix.Kqueue()
	if err != nil {
		return poll(stop, files, 25*time.Millisecond)
	}
	defer unix.Close(kq)
	var changes []unix.Kevent_t
	for _, f := range files {
		fd, err := unix.Open(f.Path, unix.O_EVTONLY, 0)
		if err != nil {
			continue // not there yet: the checks below still see it appear
		}
		defer unix.Close(fd)
		var ev unix.Kevent_t
		unix.SetKevent(&ev, fd, unix.EVFILT_VNODE, unix.EV_ADD|unix.EV_CLEAR)
		ev.Fflags = unix.NOTE_WRITE | unix.NOTE_EXTEND | unix.NOTE_DELETE | unix.NOTE_RENAME
		changes = append(changes, ev)
	}
	if len(changes) == 0 {
		return poll(stop, files, 250*time.Millisecond)
	}
	if _, err := unix.Kevent(kq, changes, nil, nil); err != nil {
		return poll(stop, files, 25*time.Millisecond)
	}
	// Written between being read and being watched.
	if changed(files) {
		return true
	}
	events := make([]unix.Kevent_t, len(changes))
	// Wakes a few times a second to notice stop; a write wakes it at once.
	wait := unix.NsecToTimespec(int64(250 * time.Millisecond))
	for {
		n, err := unix.Kevent(kq, nil, events, &wait)
		if err != nil && err != unix.EINTR {
			return poll(stop, files, 25*time.Millisecond)
		}
		select {
		case <-stop:
			return false
		default:
		}
		if n > 0 && changed(files) {
			return true
		}
		for i := range n {
			if events[i].Fflags&(unix.NOTE_DELETE|unix.NOTE_RENAME) != 0 {
				return true // replaced: whoever reads it starts over
			}
		}
	}
}
