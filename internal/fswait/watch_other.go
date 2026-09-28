//go:build !darwin

package fswait

// Watcher, elsewhere, can't tell: every ask is a change.
type Watcher struct{}

func NewWatcher() *Watcher        { return &Watcher{} }
func (w *Watcher) Watch([]string) {}
func (w *Watcher) Changed() bool  { return true }
func (w *Watcher) Close()         {}
