package fleet

import (
	"sync"
	"time"
)

// Automatic disk meters are housekeeping, not foreground work. Share one
// budget across temp and worktree scans so concurrent meters cannot saturate
// a core. Two milliseconds of walking earns eighteen milliseconds of rest.
var backgroundDisk struct {
	sync.Mutex
	pace diskPacer
}

type diskPacer struct {
	last time.Time
	work time.Duration
}

func DiskUsageBackground(dirs []TempDir) int64 {
	backgroundDisk.Lock()
	defer backgroundDisk.Unlock()
	p := &backgroundDisk.pace
	p.last = time.Now() // time between walks does not consume the work budget
	n := diskUsage(dirs, p)
	p.yield()
	return n
}

func (p *diskPacer) yield() {
	if p == nil {
		return
	}
	now := time.Now()
	p.work += now.Sub(p.last)
	if p.work >= 2*time.Millisecond {
		// An unusually slow filesystem call need not impose an unbounded sleep.
		time.Sleep(min(100*time.Millisecond, p.work*9))
		p.work = 0
		now = time.Now()
	}
	p.last = now
}
