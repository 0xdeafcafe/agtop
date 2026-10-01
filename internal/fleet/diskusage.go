package fleet

import (
	"os"

	"golang.org/x/sys/unix"
)

// statUsage is what's inside a folder, each entry looked at through the
// folder's own descriptor: a temp folder can hold a node_modules or two, and
// a whole path and a FileInfo for every file made walks allocate megabytes.
func statUsage(dir string) int64 { return statUsagePaced(dir, nil) }

func statUsagePaced(dir string, pace *diskPacer) int64 {
	pace.yield()
	f, err := os.Open(dir)
	if err != nil {
		return 0
	}
	defer f.Close()
	names, _ := f.Readdirnames(-1)
	fd := int(f.Fd())
	var n int64
	for _, name := range names {
		pace.yield()
		var st unix.Stat_t
		if unix.Fstatat(fd, name, &st, unix.AT_SYMLINK_NOFOLLOW) != nil {
			continue
		}
		n += st.Blocks * 512
		if st.Mode&unix.S_IFMT == unix.S_IFDIR {
			n += statUsagePaced(dir+"/"+name, pace)
		}
	}
	return n
}
