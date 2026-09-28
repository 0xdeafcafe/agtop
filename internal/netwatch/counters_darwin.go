//go:build darwin

package netwatch

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// counters is the bytes in and out of each of the machine's own
// interfaces, by index, read from the routing table's interface list: no
// process run. macOS hands them to a process that isn't root cut to 32
// bits, so they wrap every 4G; rates allow for that.
func counters() (map[int][2]uint64, bool) {
	b, err := syscall.RouteRIB(unix.NET_RT_IFLIST2, 0) //nolint:staticcheck // the sysctl x/sys can't name
	if err != nil {
		return nil, false
	}
	out := map[int][2]uint64{}
	for len(b) >= 4 {
		n := int(*(*uint16)(unsafe.Pointer(&b[0])))
		if n == 0 || n > len(b) {
			break
		}
		if b[3] == unix.RTM_IFINFO2 && n >= unix.SizeofIfMsghdr2 {
			m := (*unix.IfMsghdr2)(unsafe.Pointer(&b[0]))
			// Ethernet and Wi-Fi (6), and cellular (0xff): not loopback,
			// tunnels or bridges, whose bytes are counted where they leave.
			if m.Flags&unix.IFF_UP != 0 && (m.Data.Type == 6 || m.Data.Type == 0xff) {
				out[int(m.Index)] = [2]uint64{m.Data.Ibytes, m.Data.Obytes}
			}
		}
		b = b[n:]
	}
	return out, true
}
