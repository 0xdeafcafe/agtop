//go:build darwin

package squeeze

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	block        = 64 << 10 // decmpfs compresses 64 KB at a time
	typeZlibFork = 4        // zlib blocks in the resource fork
	ufCompressed = 0x20     // UF_COMPRESSED
)

// Compressed reports whether a file is stored compressed already.
func Compressed(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Flags&ufCompressed != 0
}

func diskUse(info os.FileInfo) int64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return st.Blocks * 512
	}
	return info.Size()
}

// The resource map every decmpfs resource fork ends with: one resource of
// type 'cmpf', id 1, no name.
var forkMap = []byte{
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0x1c, 0, 0x32, 0, 0, 'c', 'm',
	'p', 'f', 0, 0, 0, 0x0a, 0, 1, 0xff, 0xff, 0, 0, 0, 0, 0, 0,
	0, 0,
}

// encode builds the decmpfs header and resource fork for data.
func encode(data []byte) (header, fork []byte, err error) {
	n := (len(data) + block - 1) / block
	var blocks bytes.Buffer
	table := make([]byte, 4+8*n)
	binary.LittleEndian.PutUint32(table, uint32(n))
	off := uint32(len(table))
	for i := 0; i < n; i++ {
		chunk := data[i*block : min((i+1)*block, len(data))]
		var z bytes.Buffer
		w, _ := zlib.NewWriterLevel(&z, zlib.BestCompression)
		if _, err := w.Write(chunk); err != nil {
			return nil, nil, err
		}
		if err := w.Close(); err != nil {
			return nil, nil, err
		}
		b := z.Bytes()
		if len(b) >= len(chunk) {
			// Doesn't shrink: stored as it is, marked by a leading 0xff.
			b = append([]byte{0xff}, chunk...)
		}
		binary.LittleEndian.PutUint32(table[4+8*i:], off)
		binary.LittleEndian.PutUint32(table[8+8*i:], uint32(len(b)))
		off += uint32(len(b))
		blocks.Write(b)
	}
	res := append(table, blocks.Bytes()...)
	dataLen := 4 + len(res)
	fork = make([]byte, 0x100, 0x100+dataLen+len(forkMap))
	binary.BigEndian.PutUint32(fork[0:], 0x100)
	binary.BigEndian.PutUint32(fork[4:], uint32(0x100+dataLen))
	binary.BigEndian.PutUint32(fork[8:], uint32(dataLen))
	binary.BigEndian.PutUint32(fork[12:], uint32(len(forkMap)))
	fork = binary.BigEndian.AppendUint32(fork, uint32(len(res)))
	fork = append(fork, res...)
	fork = append(fork, forkMap...)
	header = make([]byte, 16)
	copy(header, "fpmc")
	binary.LittleEndian.PutUint32(header[4:], typeZlibFork)
	binary.LittleEndian.PutUint64(header[8:], uint64(len(data)))
	return header, fork, nil
}

// File compresses one file in place. It writes a compressed copy beside
// it, reads that back through the system and compares every byte, and
// only then puts it in the file's place, and only if the file hasn't
// changed meanwhile. Any failure leaves the file as it was.
func File(path string) error {
	before, err := os.Stat(path)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	header, fork, err := encode(data)
	if err != nil {
		return err
	}
	if len(fork) >= len(data) {
		return errors.New("doesn't compress")
	}
	tmp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".squeeze")
	_ = os.Remove(tmp)
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, before.Mode().Perm())
	if err != nil {
		return err
	}
	f.Close()
	done := false
	defer func() {
		if !done {
			_ = unix.Chflags(tmp, 0)
			_ = os.Remove(tmp)
		}
	}()
	if err := unix.Setxattr(tmp, "com.apple.ResourceFork", fork, 0); err != nil {
		return fmt.Errorf("resource fork: %w", err)
	}
	if err := unix.Setxattr(tmp, "com.apple.decmpfs", header, 0); err != nil {
		return fmt.Errorf("decmpfs: %w", err)
	}
	if err := unix.Chflags(tmp, ufCompressed); err != nil {
		return fmt.Errorf("chflags: %w", err)
	}
	back, err := os.ReadFile(tmp)
	if err != nil || !bytes.Equal(back, data) {
		return errors.New("the compressed copy doesn't read back the same")
	}
	_ = os.Chtimes(tmp, before.ModTime(), before.ModTime())
	now, err := os.Stat(path)
	if err != nil || now.Size() != before.Size() || !now.ModTime().Equal(before.ModTime()) {
		return errors.New("written to meanwhile")
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	done = true
	return nil
}
