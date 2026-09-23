//go:build !darwin

package proc

import "syscall"

// Linux support needs a /proc backend; until then the process columns stay empty.
func list() []*Proc                          { return nil }
func fillUsage(*Proc)                        {}
func Args(int) []string                      { return nil }
func CommandLine(int) string                 { return "" }
func Kill(pid int, sig syscall.Signal) error { return syscall.Kill(pid, sig) }
