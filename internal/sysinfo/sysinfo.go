// Package sysinfo is the machine's battery and free disk, for the top bar.
// Both are read in the background at most every so often, so drawing a
// frame never waits on them.
package sysinfo

import (
	"os"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// Battery is the machine's battery; Present is false on one without.
type Battery struct {
	Present  bool
	Percent  int
	Charging bool // plugged in, whether or not it is filling
}

// Disk is the space on the volume agents work on (the home folder's).
type Disk struct {
	Free, Total uint64
}

const every = 30 * time.Second

var cur struct {
	sync.Mutex
	bat     Battery
	disk    Disk
	at      time.Time
	reading bool
}

// Now returns the last readings, starting a new one when they're stale.
// Before the first has finished they're zero.
func Now() (Battery, Disk) {
	cur.Lock()
	defer cur.Unlock()
	if !cur.reading && time.Since(cur.at) >= every {
		cur.reading = true
		go refresh()
	}
	return cur.bat, cur.disk
}

func refresh() {
	b := readBattery()
	d := readDisk()
	cur.Lock()
	cur.bat, cur.disk, cur.at, cur.reading = b, d, time.Now(), false
	cur.Unlock()
}

func readDisk() Disk {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/"
	}
	var st unix.Statfs_t
	if unix.Statfs(home, &st) != nil {
		return Disk{}
	}
	bs := uint64(st.Bsize)
	return Disk{Free: uint64(st.Bavail) * bs, Total: uint64(st.Blocks) * bs}
}
