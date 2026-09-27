//go:build !darwin

package sysinfo

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// readBattery reads Linux's power supply class; elsewhere there's none.
func readBattery() Battery {
	dirs, _ := filepath.Glob("/sys/class/power_supply/BAT*")
	if len(dirs) == 0 {
		return Battery{}
	}
	c, err := os.ReadFile(filepath.Join(dirs[0], "capacity"))
	if err != nil {
		return Battery{}
	}
	p, _ := strconv.Atoi(strings.TrimSpace(string(c)))
	st, _ := os.ReadFile(filepath.Join(dirs[0], "status"))
	s := strings.TrimSpace(string(st))
	return Battery{Present: true, Percent: p, Charging: s != "Discharging"}
}
