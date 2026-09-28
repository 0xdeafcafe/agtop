//go:build linux

package netwatch

import (
	"os"
	"strconv"
	"strings"
)

// counters is the bytes in and out of each of the machine's own
// interfaces, by their place in /proc/net/dev.
func counters() (map[int][2]uint64, bool) {
	b, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return nil, false
	}
	out := map[int][2]uint64{}
	i := 0
	for l := range strings.SplitSeq(string(b), "\n") {
		i++
		name, rest, found := strings.Cut(l, ":")
		name = strings.TrimSpace(name)
		if !found || tunnel(name) {
			continue
		}
		f := strings.Fields(rest)
		if len(f) < 9 {
			continue
		}
		r, _ := strconv.ParseUint(f[0], 10, 64)
		t, _ := strconv.ParseUint(f[8], 10, 64)
		out[i] = [2]uint64{r, t}
	}
	return out, true
}
