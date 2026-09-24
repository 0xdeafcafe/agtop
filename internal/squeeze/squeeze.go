// Package squeeze stores idle transcripts compressed the way macOS stores
// its own system files (decmpfs): the file keeps its name and contents,
// every program reads it as before, and the system decompresses as it
// reads. A file written to again goes back to being stored plainly.
package squeeze

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Result is what a pass did.
type Result struct {
	Files  int   // files compressed
	Before int64 // their disk use before
	After  int64 // and after
	Failed int   // files left as they were because something didn't check out
}

// Transcripts compresses the .jsonl transcripts under dirs untouched for
// idle, that aren't compressed already. A file that fails any check is left
// exactly as it was.
func Transcripts(dirs []string, idle time.Duration) Result {
	var r Result
	cutoff := time.Now().Add(-idle)
	for _, dir := range dirs {
		_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".jsonl") {
				return nil
			}
			info, err := d.Info()
			if err != nil || info.Size() < 64<<10 || info.ModTime().After(cutoff) || Compressed(info) {
				return nil
			}
			before := diskUse(info)
			if err := File(p); err != nil {
				r.Failed++
				return nil
			}
			r.Files++
			r.Before += before
			if st, err := os.Stat(p); err == nil {
				r.After += diskUse(st)
			}
			return nil
		})
	}
	return r
}
