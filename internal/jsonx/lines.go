package jsonx

import (
	"bufio"
	"bytes"
	"io"
)

// MaxLine is the longest line a LineReader takes; tool results and file reads
// can make single lines very long.
const MaxLine = 64 << 20

// LineReader splits output into lines like bufio.Scanner, but a long line's
// buffer goes once it has been handled: one 30 MB tool result mustn't keep
// 32 MB for the rest of the session. Every session and every client of a
// host has one, so what it keeps between lines is kept small too.
type LineReader struct {
	r    *bufio.Reader
	long []byte
	err  error // why reading stopped, other than the end of the output
}

// Err is why reading stopped, if it wasn't the end of the output.
func (l *LineReader) Err() error { return l.err }

// NewLineReader reads r a line at a time.
func NewLineReader(r io.Reader) *LineReader {
	return &LineReader{r: bufio.NewReaderSize(r, 64<<10)}
}

// Next returns the next line without its newline. It is only good until
// the next call.
func (l *LineReader) Next() ([]byte, bool) {
	if cap(l.long) > 256<<10 {
		l.long = nil
	}
	line, err := l.r.ReadSlice('\n')
	if err == bufio.ErrBufferFull {
		l.long = append(l.long[:0], line...)
		for err == bufio.ErrBufferFull {
			line, err = l.r.ReadSlice('\n')
			l.long = append(l.long, line...)
			if len(l.long) > MaxLine {
				l.err = bufio.ErrTooLong
				return nil, false
			}
		}
		line = l.long
	}
	if err != nil && err != io.EOF {
		l.err = err
		return nil, false
	}
	if len(line) == 0 && err == io.EOF {
		return nil, false
	}
	line = bytes.TrimSuffix(line, []byte("\n"))
	return bytes.TrimSuffix(line, []byte("\r")), true
}
