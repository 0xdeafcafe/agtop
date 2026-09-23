package daemon

import (
	"bytes"
	"io"
	"testing"
)

type chunks struct{ parts [][]byte }

func (c *chunks) Read(p []byte) (int, error) {
	if len(c.parts) == 0 {
		return 0, io.EOF
	}
	n := copy(p, c.parts[0])
	c.parts = c.parts[1:]
	return n, nil
}

func TestDetachMarkerSplitAcrossReads(t *testing.T) {
	var out bytes.Buffer
	m := detachMarker
	r := &chunks{parts: [][]byte{[]byte("hello"), m[:len(m)-1], m[len(m)-1:]}}
	if err := pumpOut(r, &out); err != errDetach {
		t.Fatalf("detach missed: %v", err)
	}
	if out.String() != "hello" {
		t.Fatalf("marker leaked into output: %q", out.String())
	}
}

func TestTrailingEscapeIsNotHeldBack(t *testing.T) {
	var out bytes.Buffer
	r := &chunks{parts: [][]byte{[]byte("frame\x1b[?25h")}}
	_ = pumpOut(r, &out)
	if out.String() != "frame\x1b[?25h" {
		t.Fatalf("got %q", out.String())
	}
}

func TestOutputMentioningEKICKEDDoesNotDetach(t *testing.T) {
	var out bytes.Buffer
	r := &chunks{parts: [][]byte{[]byte("grep EKICKED attach.go\n")}}
	if err := pumpOut(r, &out); err != io.EOF || out.Len() == 0 {
		t.Fatalf("stopped early or dropped output: %v %q", err, out.String())
	}
}
