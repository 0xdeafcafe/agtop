package jsonx

import (
	"strings"
	"testing"
)

func TestLineReader(t *testing.T) {
	long := strings.Repeat("x", 3<<20)
	l := NewLineReader(strings.NewReader("a\r\n\n" + long + "\nlast"))
	var got []string
	for {
		line, ok := l.Next()
		if !ok {
			break
		}
		got = append(got, string(line))
	}
	if len(got) != 4 || got[0] != "a" || got[1] != "" || got[2] != long || got[3] != "last" || l.err != nil {
		t.Fatalf("lines = %d, err %v", len(got), l.err)
	}
	if l.Next(); l.long != nil {
		t.Error("a long line's buffer should go once it's handled")
	}
	l = NewLineReader(strings.NewReader(strings.Repeat("y", MaxLine+10)))
	if _, ok := l.Next(); ok || l.err == nil {
		t.Error("a line over the limit should stop the reader with an error")
	}
}
