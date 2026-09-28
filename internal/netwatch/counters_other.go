//go:build !darwin && !linux

package netwatch

func counters() (map[int][2]uint64, bool) { return nil, false }
