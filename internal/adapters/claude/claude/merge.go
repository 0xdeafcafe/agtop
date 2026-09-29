package claude

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// history is what of a config folder is its past sessions: the
// transcripts, and the file copies /rewind puts back.
var history = []string{"projects", "file-history"}

// MergeHistory copies from's past sessions into to, so they're found and
// resumed there. A file to already has is left as it is. from is left
// whole: nothing is taken away.
func MergeHistory(from, to Account) error {
	var errs []error
	for _, dir := range history {
		src, dst := filepath.Join(from.ConfigDir, dir), filepath.Join(to.ConfigDir, dir)
		err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return err
			}
			if !d.Type().IsRegular() {
				return nil
			}
			rel, err := filepath.Rel(src, p)
			if err != nil {
				return err
			}
			return copyNew(p, filepath.Join(dst, rel))
		})
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// copyNew copies src to dst when there's nothing at dst yet, keeping its
// times: a past session's age is when it last changed.
func copyNew(src, dst string) error {
	if _, err := os.Lstat(dst); err == nil {
		return nil
	}
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+"-*")
	if err != nil {
		return err
	}
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	tmp.Close()
	_ = os.Chmod(tmp.Name(), st.Mode().Perm())
	_ = os.Chtimes(tmp.Name(), st.ModTime(), st.ModTime())
	return os.Rename(tmp.Name(), dst)
}
