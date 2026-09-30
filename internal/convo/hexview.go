package convo

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// od -c's dump of bytes, its escapes and octals made out by eye, is drawn
// as a hex viewer: each byte in hex, then as text, anything past ASCII
// picked out, so a stray BOM or a non-breaking space shows for what it is.
// od puts sixteen bytes to a line as the viewer does, so its lines are
// swapped one for one and the rest of the output lines up as it did.

// hexRows are the lines of an od -c dump in lines, drawn as hex rows, by
// line; nil when there's none.
func hexRows(lines []string) map[int]string {
	var out map[int]string
	for i := 0; i < len(lines); i++ {
		off, bs, ok := odLine(lines[i])
		if !ok || off != 0 {
			continue
		}
		// A dump starts at 0 and each line after is sixteen bytes on.
		run := []int{i}
		dumps := [][]byte{bs}
		garbled := false
		for j := i + 1; j < len(lines); j++ {
			o, b, ok := odLine(lines[j])
			end := len(b) == 0 && o > off && o < off+16 // the offset the dump ends at
			if !ok || o != off+16 && !end {
				// macOS's od misdraws a character split over two lines; a
				// dump it garbled is left as it printed it, not guessed at.
				garbled = strings.HasPrefix(lines[j], fmt.Sprintf("%07o", off+16))
				break
			}
			if len(b) == 0 && o-off < len(dumps[len(dumps)-1]) {
				// The end's offset says how much of the last line was
				// bytes; macOS pads it out with blanks.
				dumps[len(dumps)-1] = dumps[len(dumps)-1][:o-off]
			}
			off = o
			run, dumps = append(run, j), append(dumps, b)
		}
		if garbled || len(run) < 2 && len(bs) < 4 {
			i = run[len(run)-1]
			continue // too little to be sure it's od, or not to be trusted
		}
		if out == nil {
			out = map[int]string{}
		}
		at := 0
		for k, i := range run {
			out[i] = hexRow(at, dumps[k], lostLead(dumps[k]), 16)
			at += len(dumps[k])
		}
		i = run[len(run)-1]
	}
	return out
}

// odLine reads a line of od -c: an octal offset, then up to sixteen bytes
// four columns each. A multi-byte character GNU od writes once and marks
// its other bytes **.
func odLine(l string) (int, []byte, bool) {
	l = strings.TrimRight(l, "\r")
	n := 0
	for n < len(l) && l[n] >= '0' && l[n] <= '7' {
		n++
	}
	if n < 7 {
		return 0, nil, false
	}
	off, err := strconv.ParseInt(l[:n], 8, 64)
	if err != nil {
		return 0, nil, false
	}
	rest := []rune(l[n:]) // four columns a byte, and é is one column
	if len(rest) == 0 {
		return int(off), nil, true // the dump's end
	}
	if len(rest)%4 == 1 && rest[0] == ' ' {
		rest = rest[1:] // macOS's od puts a space after the offset
	}
	if len(rest)%4 != 0 || len(rest)/4 > 16 {
		return 0, nil, false
	}
	var out []byte
	skip := 0
	for k := 0; k < len(rest); k += 4 {
		tok := strings.TrimLeft(string(rest[k:k+4]), " ")
		switch {
		case tok == "":
			out = append(out, ' ')
		case tok == "**" && skip > 0:
			skip--
		case len(tok) == 3 && isOctal(tok):
			v, _ := strconv.ParseUint(tok, 8, 8)
			out = append(out, byte(v))
		case len(tok) == 2 && tok[0] == '\\':
			b, ok := odEscapes[tok[1]]
			if !ok {
				return 0, nil, false
			}
			out = append(out, b)
		case utf8.RuneCountInString(tok) == 1:
			out = append(out, tok...)
			skip = len(tok) - 1
		default:
			return 0, nil, false
		}
	}
	return int(off), out, true
}

var odEscapes = map[byte]byte{'0': 0, 'a': 7, 'b': 8, 't': 9, 'n': 10, 'v': 11, 'f': 12, 'r': 13, '\\': '\\'}

func isOctal(s string) bool {
	for i := range len(s) {
		if s[i] < '0' || s[i] > '7' {
			return false
		}
	}
	return true
}

// hexRow is up to per bytes at off as a hex viewer draws them: the
// offset, the bytes in hex in groups of eight, then as text, each coloured
// by its class. What can't be shown as text is a dot, and the byte od
// lost, when it did (lost is -1 when not), is ?? in red.
func hexRow(off int, bs []byte, lost, per int) string {
	var b strings.Builder
	b.WriteString(faint(fmt.Sprintf("%08x", off)) + "  ")
	for i := range per {
		if i > 0 && i%8 == 0 {
			b.WriteByte(' ')
		}
		switch {
		case i >= len(bs):
			b.WriteString("   ")
		case i == lost:
			b.WriteString(paint(cRed, "??") + " ")
		default:
			b.WriteString(paint(byteInk(bs[i]), fmt.Sprintf("%02x", bs[i])) + " ")
		}
	}
	b.WriteString(" " + faint("│"))
	for i, c := range bs {
		switch {
		case i == lost:
			b.WriteString(paint(cRed, "?"))
		case c > 0x20 && c < 0x7f || c == ' ':
			b.WriteString(paint(cText, string(rune(c))))
		default:
			b.WriteString(paint(byteInk(c), "·"))
		}
	}
	b.WriteString(faint("│"))
	return b.String()
}

// byteInk is a byte's colour by its class: NUL faint, whitespace blue,
// other control characters dim, past ASCII orange, the rest as text.
func byteInk(c byte) string {
	switch {
	case c == 0:
		return cFaint
	case c == ' ' || c >= '\t' && c <= '\r':
		return cBlue
	case c < 0x20 || c == 0x7f:
		return cDim
	case c >= 0x80:
		return cOrange
	}
	return cText
}

// lostLead is where in a line macOS's od lost the first byte of a
// character it split over two lines, or -1. It prints the next byte in
// the lead's place, so the line ends on UTF-8 continuation bytes with no
// lead before them, and the first of them is the one it got wrong.
func lostLead(d []byte) int {
	k := len(d) - 1
	for k >= 0 && d[k]&0xc0 == 0x80 {
		k--
	}
	if k < len(d)-1 && (k < 0 || d[k] < 0xc0) {
		return k + 1
	}
	return -1
}
