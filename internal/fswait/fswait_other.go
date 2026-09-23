//go:build !darwin

package fswait

import "time"

// Grown blocks until one of files differs in size from what was read, or
// stop closes; it reports which.
func Grown(stop <-chan struct{}, files []File) bool {
	return poll(stop, files, 25*time.Millisecond)
}
