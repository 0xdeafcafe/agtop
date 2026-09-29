package host

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"time"

	"github.com/0xdeafcafe/agtop/internal/headless"
	"github.com/0xdeafcafe/agtop/internal/jsonx"
)

// A client says hello first, with its protocol (op "hello", its Proto). One
// from eventsFrom on gets a Claude Code session as agtop's own events, as
// any other agent's session comes; one that says less, or sends no hello
// (an older agtop), gets Claude Code's lines as it always has. Hosts from
// before the hello take it for an op they don't know, and do nothing.

// eventsFrom is the first client protocol that reads Claude Code's
// sessions as agtop's own events.
const eventsFrom = 6

// helloWait is how long a new connection's first line may take to say
// what the client reads, before it's taken for an older agtop's.
const helloWait = 300 * time.Millisecond

// hello reads a connection's first line, waiting up to helloWait for it:
// the protocol the client says it speaks, or 0, and what's left to read of
// the connection as ops, the first line with it when that wasn't a hello.
func hello(nc net.Conn) (proto int, rest io.Reader) {
	r := bufio.NewReaderSize(nc, 64<<10)
	_ = nc.SetReadDeadline(time.Now().Add(helloWait))
	line, err := r.ReadSlice('\n')
	_ = nc.SetReadDeadline(time.Time{})
	if err == nil {
		var o op
		if jsonx.Unmarshal(line, &o) == nil && o.Op == "hello" {
			return o.Proto, r
		}
	}
	// No hello: what came is the start of the ops, read again from the top.
	return 0, io.MultiReader(bytes.NewReader(bytes.Clone(line)), r)
}

// encoder turns Claude Code's lines into agtop's own events, for one client
// that reads them. Everything else, the host's own lines and other agents'
// events, goes as it is.
type encoder struct {
	n   headless.Neutral
	buf bytes.Buffer
}

// isClaudeLine is a line as Claude Code wrote it, rather than the host,
// told by how it starts: most are, deltas above all.
func isClaudeLine(l []byte) bool {
	return bytes.HasPrefix(l, []byte(`{"type":"`)) && !bytes.HasPrefix(l, []byte(`{"type":"agtop_`))
}

// claudeEvent is a line of Claude Code's as its event, and false for the
// host's own lines. Claude doesn't always start with its type (a turn's
// result may not), so a line that doesn't is taken apart to tell.
func claudeEvent(l []byte) (headless.Event, bool) {
	if isClaudeLine(l) {
		ev, err := headless.Decode(l)
		return ev, err == nil
	}
	ev, err := Decode(l)
	h, ok := ev.(headless.Event)
	return h, err == nil && ok
}

// encode writes line to w as this client reads it, each line ended with a
// newline.
func (e *encoder) encode(w io.Writer, line []byte) error {
	ev, ok := claudeEvent(line)
	if !ok {
		return writeLine(w, line) // the host's own, or one it can't read
	}
	for _, out := range e.n.Event(ev) {
		b, err := eventLine(out)
		if err != nil {
			continue
		}
		if err := writeLine(w, b); err != nil {
			return err
		}
	}
	return nil
}

// replay writes a ring line, unpacked, as this client reads it.
func (e *encoder) replay(w io.Writer, u *unpacker, l []byte) error {
	e.buf.Reset()
	if err := u.writeTo(&e.buf, l); err != nil {
		return err
	}
	return e.encode(w, e.buf.Bytes())
}

func writeLine(w io.Writer, l []byte) error {
	if _, err := w.Write(l); err != nil {
		return err
	}
	_, err := w.Write([]byte{'\n'})
	return err
}
