package sysinfo

import (
	"regexp"
	"strconv"
	"strings"
)

var pmsetPct = regexp.MustCompile(`(\d+)%`)

// parsePmset reads `pmset -g batt`: its first line says what power it's
// drawing from, the next the battery's charge.
func parsePmset(out string) Battery {
	first, rest, _ := strings.Cut(out, "\n")
	if !strings.Contains(rest, "InternalBattery") {
		return Battery{}
	}
	m := pmsetPct.FindStringSubmatch(rest)
	if m == nil {
		return Battery{}
	}
	p, _ := strconv.Atoi(m[1])
	return Battery{Present: true, Percent: p, Charging: strings.Contains(first, "AC Power")}
}
