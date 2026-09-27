//go:build darwin

package sysinfo

import (
	"context"
	"os/exec"
	"time"
)

func readBattery() Battery {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/pmset", "-g", "batt").Output()
	if err != nil {
		return Battery{}
	}
	return parsePmset(string(out))
}
