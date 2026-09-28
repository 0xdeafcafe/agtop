package jsonx

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestV1Compatible(t *testing.T) {
	var v struct {
		SessionID string `json:"sessionId"`
		N         int    `json:"n"`
	}
	// Another program's casing, a repeated key, and a string cut mid-emoji.
	in := []byte(`{"SessionId":"a","n":1,"n":2,"x":"` + "\xf0\x9f" + `"}`)
	if err := Unmarshal(in, &v); err != nil || v.SessionID != "a" || v.N != 2 {
		t.Fatalf("got %+v, %v: want what v1 read", v, err)
	}
	b, err := Marshal(map[string]string{"s": "cut \xf0\x9f"})
	if err != nil {
		t.Fatalf("a string cut mid-character must still write: %v", err)
	}
	if !bytes.Contains(b, []byte(`\ufffd`)) && !bytes.Contains(b, []byte("\ufffd")) {
		t.Fatalf("invalid UTF-8 should become U+FFFD: %s", b)
	}
}

func TestWriteAndIndent(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, map[string]string{"cmd": "a && b > c"}); err != nil {
		t.Fatal(err)
	}
	if buf.String() != `{"cmd":"a && b > c"}`+"\n" {
		t.Fatalf("got %q: one line, a newline, and nothing escaped", buf.String())
	}
	in := []byte(`{"a":[1,2],"b":"\u0026"}`)
	out, err := Indent(in)
	if err != nil {
		t.Fatal(err)
	}
	if want := "{\n  \"a\": [\n    1,\n    2\n  ],\n  \"b\": \"\\u0026\"\n}"; string(out) != want {
		t.Fatalf("got %s, want %s", out, want)
	}
	if string(in) != `{"a":[1,2],"b":"\u0026"}` {
		t.Fatal("Indent changed its input")
	}
	b, _ := MarshalIndent(map[string]int{"a": 1})
	if string(b) != "{\n  \"a\": 1\n}" {
		t.Fatalf("got %q", b)
	}
}

func TestDecodeAndReject(t *testing.T) {
	var v struct{ A int }
	if err := Decode(strings.NewReader(`{"a":1}`), &v); err != nil || v.A != 1 {
		t.Fatalf("%+v %v", v, err)
	}
	if err := Unmarshal([]byte(`{"a":1,"b":2}`), &v, RejectUnknown); err == nil {
		t.Fatal("RejectUnknown let an unknown name through")
	}
	if !Valid([]byte(`{"a":1}`)) || Valid([]byte(`{"a":`)) {
		t.Fatal("Valid")
	}
}

func TestDuration(t *testing.T) {
	// v2 has no default for time.Duration: Opts reads and writes it as v1
	// did, in nanoseconds, bare or behind a pointer.
	var v struct {
		D time.Duration  `json:"d"`
		P *time.Duration `json:"p,omitempty"`
	}
	if err := Unmarshal([]byte(`{"d":1500000000}`), &v); err != nil || v.D != 1500*time.Millisecond {
		t.Fatalf("%v %v", v.D, err)
	}
	if b, _ := Marshal(v); string(b) != `{"d":1500000000}` {
		t.Fatalf("got %s", b)
	}
}

func TestNilStaysNil(t *testing.T) {
	var v struct {
		S []string
		M map[string]int
	}
	b, _ := Marshal(v)
	if string(b) != `{"S":null,"M":null}` {
		t.Fatalf("got %s: nil should write null, as v1", b)
	}
	if err := Unmarshal(b, &v); err != nil || v.S != nil || v.M != nil {
		t.Fatal("null should read back nil")
	}
}
