package host

import (
	"bytes"
	"strings"
	"testing"
)

// Big lines are kept deflated and come back out as they went in; small
// ones, deltas and the host's own lines are kept as they are.
func TestRingPacking(t *testing.T) {
	var p packer
	var u unpacker
	big := []byte(`{"type":"user","message":{"content":"` + strings.Repeat("some file contents ", 2000) + `"}}`)
	lines := [][]byte{
		big,
		[]byte(`{"type":"assistant","message":{"content":"short"}}`),
		[]byte(`{"type":"stream_event","event":{"text":"` + strings.Repeat("x", 4000) + `"}}`),
		[]byte(`{"agtop_sent":true,"message":{"content":"` + strings.Repeat("y", 4000) + `"}}`),
		append([]byte(nil), big...), // again, through the reused compressor
	}
	for i, l := range lines {
		k := p.pack(l)
		if packs := i == 0 || i == 4; packs != (k[0] == packed) {
			t.Fatalf("line %d packed=%v", i, k[0] == packed)
		}
		if k[0] == packed && len(k) >= len(l)/4 {
			t.Fatalf("line %d packed to %d of %d", i, len(k), len(l))
		}
		if lineLen(k) != len(l) {
			t.Fatalf("line %d: lineLen %d, want %d", i, lineLen(k), len(l))
		}
		var out bytes.Buffer
		if err := u.writeTo(&out, k); err != nil || !bytes.Equal(out.Bytes(), l) {
			t.Fatalf("line %d came back as %.60q (%v)", i, out.Bytes(), err)
		}
	}
}
