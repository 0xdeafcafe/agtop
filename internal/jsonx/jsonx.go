// Package jsonx is how rush reads and writes JSON: encoding/json/v2, always
// with Opts. Nothing else calls v2's Marshal or Unmarshal (rush's linter
// says so), so every call gets the same options.
package jsonx

import (
	"bytes"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"io"
	"time"
)

// Opts are the options every call gets. v2 is stricter than v1 by default;
// these keep what v1 read and wrote working, and cost nothing. Measured on
// 1.6M lines (5.9 GB) of Claude Code and Codex transcripts, v2 with these
// read 1.66× as fast as v1 and slightly faster than v2's defaults, and no
// line was rejected either way.
var Opts = json.JoinOptions(
	// A string cut mid-character still reads and writes, as with v1: v2
	// would refuse to write it at all, and a save would be lost.
	jsontext.AllowInvalidUTF8(true),
	// Other programs' files may repeat a key; the last one wins, as in v1.
	jsontext.AllowDuplicateNames(true),
	// v1 matched object names to fields in any case.
	json.MatchCaseInsensitiveNames(true),
	// v1 wrote a nil slice or map as null, which reads back as nil; v2
	// writes [] and {}, which read back empty but not nil.
	json.FormatNilSliceAsNull(true),
	json.FormatNilMapAsNull(true),
	// v1 wrote a map's keys in order, so rush's files don't churn
	// between saves; v2 only does when asked. It only affects writing.
	json.Deterministic(true),
	// v2 has no default for a time.Duration (and Go 1.27's v2 takes no
	// format tag): a number of nanoseconds, as v1 wrote it. It costs about
	// 2% on the transcripts.
	json.WithMarshalers(json.MarshalToFunc(func(e *jsontext.Encoder, d time.Duration) error {
		return e.WriteToken(jsontext.Int(int64(d)))
	})),
	json.WithUnmarshalers(json.UnmarshalFromFunc(func(dec *jsontext.Decoder, d *time.Duration) error {
		var n int64
		if err := json.UnmarshalDecode(dec, &n); err != nil {
			return err
		}
		*d = time.Duration(n)
		return nil
	})),
)

var indented = json.JoinOptions(Opts, jsontext.WithIndent("  "))

// RejectUnknown makes Unmarshal fail on a name no field takes.
var RejectUnknown = json.RejectUnknownMembers(true)

// Marshal is v in JSON.
func Marshal(v any, o ...json.Options) ([]byte, error) {
	return json.Marshal(v, join(Opts, o)...)
}

// MarshalWrite writes v to w as JSON as it goes, rather than making it
// whole in memory first: for a big file saved often.
func MarshalWrite(w io.Writer, v any, o ...json.Options) error {
	return json.MarshalWrite(w, v, join(Opts, o)...)
}

// MarshalIndent is v in JSON, indented two spaces a level.
func MarshalIndent(v any, o ...json.Options) ([]byte, error) {
	return json.Marshal(v, join(indented, o)...)
}

// Unmarshal reads the JSON in b into v.
func Unmarshal(b []byte, v any, o ...json.Options) error {
	return json.Unmarshal(b, v, join(Opts, o)...)
}

// Decode reads the one JSON value in r into v, reading r to its end.
func Decode(r io.Reader, v any, o ...json.Options) error {
	return json.UnmarshalRead(r, v, join(Opts, o)...)
}

// Write writes v to w as one line of JSON, newline included, as v1's
// Encoder did. <, > and & aren't escaped: v2 doesn't.
func Write(w io.Writer, v any) error {
	b, err := Marshal(v)
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

// Valid is whether b is one JSON value.
func Valid(b []byte) bool { return jsontext.Value(b).IsValid(Opts) }

// Indent is the JSON in b indented two spaces a level. Its strings and
// numbers stay as they were written.
func Indent(b []byte) ([]byte, error) {
	v := jsontext.Value(bytes.Clone(b))
	if err := v.Indent(jsontext.WithIndent("  ")); err != nil {
		return nil, err
	}
	return v, nil
}

// NewDecoder reads JSON from r a token or value at a time, with Opts.
func NewDecoder(r io.Reader) *jsontext.Decoder { return jsontext.NewDecoder(r, Opts) }

// DecodeValue reads d's next JSON value into v: for reading a big file a
// piece at a time, with NewDecoder.
func DecodeValue(d *jsontext.Decoder, v any) error { return json.UnmarshalDecode(d, v, Opts) }

func join(base json.Options, o []json.Options) []json.Options {
	if len(o) == 0 {
		return []json.Options{base}
	}
	return append([]json.Options{base}, o...)
}
