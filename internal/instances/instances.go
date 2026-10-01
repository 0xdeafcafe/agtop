// Package instances is the roster of running rush views, so `rush reload`
// and #reload all can tell the others to reload.
package instances

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/0xdeafcafe/rush/internal/proc"
	"github.com/0xdeafcafe/rush/internal/state"
)

// Dir holds one file per running view: <pid>, with the arguments it runs.
func Dir() string { return filepath.Join(state.Dir(), "ui") }

// Register lists this view until the returned func is called. Call it once
// the SIGUSR1 handler is set: a view that isn't listed is never signalled.
func Register() func() {
	p := filepath.Join(Dir(), strconv.Itoa(os.Getpid()))
	if os.MkdirAll(Dir(), 0o700) != nil || os.WriteFile(p, []byte(strings.Join(os.Args, "\x00")), 0o600) != nil {
		return func() {}
	}
	return func() { _ = os.Remove(p) }
}

// Reload signals every listed view but skip to reload, and says how many.
// A pid is signalled only while the process there still runs the arguments
// it listed (a reused pid doesn't); otherwise its entry is dropped.
func Reload(skip int) int {
	ents, _ := os.ReadDir(Dir())
	n := 0
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == skip {
			continue
		}
		p := filepath.Join(Dir(), e.Name())
		b, _ := os.ReadFile(p)
		if !slices.Equal(proc.Args(pid), strings.Split(string(b), "\x00")) || len(b) == 0 {
			_ = os.Remove(p)
			continue
		}
		if syscall.Kill(pid, syscall.SIGUSR1) == nil {
			n++
		}
	}
	return n
}
