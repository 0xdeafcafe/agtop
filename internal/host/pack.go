package host

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"io"
	"sync"
)

// The replay ring is most of a long-lived host's memory: whole messages and
// tool results, kept for a client that connects later. They're only read
// back on a connect, so the big ones are kept deflated, which about halves
// them (thinking signatures don't compress). The ring still counts every
// line at its full size, so what it keeps and replays is unchanged.

// packMin is the shortest line worth deflating.
const packMin = 2 << 10

// packed marks a deflated ring line; a line as Claude Code or the host
// wrote it starts with '{'.
const packed = 0

// packer deflates ring lines.
type packer struct{ buf bytes.Buffer }

// A compressor holds over a MB, so one is kept only while lines come in:
// the pool lets it go a couple of collections after the turn ends.
var deflaters sync.Pool

// pack is line as the ring keeps it: deflated when it's one of Claude
// Code's big lines, else as it is. Streamed deltas are dropped from the ring
// soon and the host's own lines are looked at again, so those stay as they
// are.
func (p *packer) pack(line []byte) []byte {
	if len(line) < packMin || !bytes.HasPrefix(line, []byte(`{"type":"`)) || isStreamEvent(line) {
		return line
	}
	p.buf.Reset()
	var n [binary.MaxVarintLen64 + 1]byte
	n[0] = packed
	p.buf.Write(n[:1+binary.PutUvarint(n[1:], uint64(len(line)))])
	zw, _ := deflaters.Get().(*flate.Writer)
	if zw == nil {
		zw, _ = flate.NewWriter(&p.buf, flate.BestSpeed)
	} else {
		zw.Reset(&p.buf)
	}
	defer deflaters.Put(zw)
	if _, err := zw.Write(line); err != nil || zw.Close() != nil || p.buf.Len() >= len(line) {
		return line
	}
	return bytes.Clone(p.buf.Bytes())
}

// lineLen is the length of a ring line as it was written.
func lineLen(l []byte) int {
	if len(l) == 0 || l[0] != packed {
		return len(l)
	}
	n, _ := binary.Uvarint(l[1:])
	return int(n)
}

// unpacker writes ring lines out as they were written.
type unpacker struct {
	zr io.ReadCloser
	r  bytes.Reader
}

// writeTo writes ring line l to w as it was written.
func (u *unpacker) writeTo(w io.Writer, l []byte) error {
	if len(l) == 0 || l[0] != packed {
		_, err := w.Write(l)
		return err
	}
	_, k := binary.Uvarint(l[1:])
	u.r.Reset(l[1+k:])
	if u.zr == nil {
		u.zr = flate.NewReader(&u.r)
	} else {
		_ = u.zr.(flate.Resetter).Reset(&u.r, nil)
	}
	_, err := io.Copy(w, u.zr)
	return err
}
