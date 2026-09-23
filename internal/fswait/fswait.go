// Package fswait waits for files to grow without polling them: kqueue on
// macOS, a quick stat loop elsewhere.
package fswait

import (
	"os"
	"time"
)

// File is a file and the size it had when last read.
type File struct {
	Path string
	Size int64
}

// changed reports whether any file's size differs from what was read.
func changed(files []File) bool {
	for _, f := range files {
		if st, err := os.Stat(f.Path); err == nil && st.Size() != f.Size {
			return true
		}
	}
	return false
}

// poll is the fallback: a stat of each file every interval.
func poll(stop <-chan struct{}, files []File, every time.Duration) bool {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if changed(files) {
			return true
		}
		select {
		case <-stop:
			return false
		case <-t.C:
		}
	}
}
