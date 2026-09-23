//go:build darwin

package proc

import (
	"bytes"
	"encoding/binary"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

func list() []*Proc {
	kps, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil
	}
	out := make([]*Proc, 0, len(kps))
	for i := range kps {
		k := &kps[i]
		comm := k.Proc.P_comm[:]
		if n := bytes.IndexByte(comm, 0); n >= 0 {
			comm = comm[:n]
		}
		out = append(out, &Proc{
			PID:   int(k.Proc.P_pid),
			PPID:  int(k.Eproc.Ppid),
			Comm:  string(comm),
			Start: time.Unix(k.Proc.P_starttime.Sec, int64(k.Proc.P_starttime.Usec)*1000),
		})
	}
	return out
}

// rusage_info_v2 from <sys/resource.h>, only as far as the fields we read.
type rusageInfoV2 struct {
	UUID          [16]byte
	UserTime      uint64
	SystemTime    uint64
	PkgIdleWkups  uint64
	InterruptWk   uint64
	Pageins       uint64
	WiredSize     uint64
	ResidentSize  uint64
	PhysFootprint uint64
	_             [11]uint64
}

const (
	procInfoCallPidRusage = 9
	rusageInfoV2Flavor    = 2
)

var tickNanos = func() float64 {
	// Apple silicon reports rusage times in timebase ticks, Intel in ns.
	if f, err := unix.SysctlUint64("hw.tbfrequency"); err == nil && f > 0 && f != 1_000_000_000 {
		return 1e9 / float64(f)
	}
	return 1
}()

func fillUsage(p *Proc) {
	var ri rusageInfoV2
	_, _, e := unix.Syscall6(unix.SYS_PROC_INFO, procInfoCallPidRusage, uintptr(p.PID),
		rusageInfoV2Flavor, 0, uintptr(unsafe.Pointer(&ri)), 0)
	if e != 0 {
		return
	}
	p.Footprint = ri.PhysFootprint
	p.CPUTime = time.Duration(float64(ri.UserTime+ri.SystemTime) * tickNanos)
}

// Args reads a process's argv through kern.procargs2.
func Args(pid int) []string {
	b, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil || len(b) < 4 {
		return nil
	}
	argc := int(binary.LittleEndian.Uint32(b[:4]))
	b = b[4:]
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[i:]
	}
	b = bytes.TrimLeft(b, "\x00")
	args := make([]string, 0, argc)
	for len(args) < argc && len(b) > 0 {
		i := bytes.IndexByte(b, 0)
		if i < 0 {
			args = append(args, string(b))
			break
		}
		args = append(args, string(b[:i]))
		b = b[i+1:]
	}
	return args
}

func CommandLine(pid int) string { return strings.Join(Args(pid), " ") }

func Kill(pid int, sig syscall.Signal) error { return syscall.Kill(pid, sig) }
