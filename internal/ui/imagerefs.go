package ui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/0xdeafcafe/rush/internal/convo"
)

// imageRefs are the images a Session's box holds, each standing in the text
// as [Image #N] where it was pasted or dropped, as Claude Code shows them.
// The marker is the image: deleting it takes the image off the message.
type imageRefs struct {
	N    int            `json:"n,omitempty"`    // the last number given
	Path map[int]string `json:"path,omitempty"` // each number's file
}

var imageMarkerRe = regexp.MustCompile(`\[Image #(\d+)\]`)

func imageMarker(n int) string { return fmt.Sprintf("[Image #%d]", n) }

// add keeps an image file and returns the marker that stands for it.
func (r *imageRefs) add(path string) string {
	if r.Path == nil {
		r.Path = map[int]string{}
	}
	r.N++
	r.Path[r.N] = path
	return imageMarker(r.N)
}

// inline puts a marker in place of each image file named in text: dropped
// or typed paths become the images they name, where they were.
func (r *imageRefs) inline(text string, look pathLookup) (string, bool) {
	var b strings.Builder
	last, found := 0, false
	for _, sp := range pathSpans(text) {
		p := imagePath(sp.tok, look)
		if p == "" {
			continue
		}
		b.WriteString(text[last:sp.from])
		b.WriteString(r.add(p))
		last, found = sp.to, true
	}
	if !found {
		return text, false
	}
	b.WriteString(text[last:])
	return b.String(), true
}

// resolve is a message about to be sent: its markers numbered 1, 2, 3 in
// the order they appear, and the images they stand for in that order. A
// marker whose image isn't this box's stays text as it is.
func (r *imageRefs) resolve(text string) (string, []string) {
	if len(r.Path) == 0 {
		return text, nil
	}
	var images []string
	seen := map[int]int{}
	out := imageMarkerRe.ReplaceAllStringFunc(text, func(mk string) string {
		id, _ := strconv.Atoi(imageMarkerRe.FindStringSubmatch(mk)[1])
		p, ok := r.Path[id]
		if !ok {
			return mk
		}
		n, again := seen[id]
		if !again {
			images = append(images, p)
			n = len(images)
			seen[id] = n
		}
		return imageMarker(n)
	})
	return out, images
}

// paths is text with each marker whose image is this box's as the file's
// path, [image: /a.png], which Claude Code opens itself.
func (r imageRefs) paths(text string) string {
	if len(r.Path) == 0 {
		return text
	}
	return imageMarkerRe.ReplaceAllStringFunc(text, func(mk string) string {
		id, _ := strconv.Atoi(imageMarkerRe.FindStringSubmatch(mk)[1])
		if p, ok := r.Path[id]; ok {
			return "[image: " + p + "]"
		}
		return mk
	})
}

// clone is a copy that doesn't share the map.
func (r imageRefs) clone() imageRefs {
	c := imageRefs{N: r.N}
	if len(r.Path) > 0 {
		c.Path = make(map[int]string, len(r.Path))
		for k, v := range r.Path {
			c.Path[k] = v
		}
	}
	return c
}

// chipRe is what deletes as one unit in a box: a long paste's chip, or an
// image's marker.
var chipRe = regexp.MustCompile(convo.PasteChipRe.String() + `|\[Image #\d+\]`)

// chipSpans are where buf's chips are, as rune offsets.
func chipSpans(buf []rune) []seg {
	s := string(buf)
	if !strings.Contains(s, "[") {
		return nil
	}
	var out []seg
	for _, loc := range chipRe.FindAllStringIndex(s, -1) {
		from := utf8.RuneCountInString(s[:loc[0]])
		out = append(out, seg{from, from + utf8.RuneCountInString(s[loc[0]:loc[1]])})
	}
	return out
}

// chipOn is the chip with the character at p in it.
func chipOn(buf []rune, p int) (seg, bool) {
	for _, c := range chipSpans(buf) {
		if p >= c.from && p < c.to {
			return c, true
		}
	}
	return seg{}, false
}

// outOfChip moves p, when it's inside a chip, to the chip's edge in the
// direction it was going: forward to after it, else to its start.
func outOfChip(spans []seg, p int, forward bool) int {
	for _, c := range spans {
		if p > c.from && p < c.to {
			if forward {
				return c.to
			}
			return c.from
		}
	}
	return p
}

// shown is a Session's box text as drawn: each image's [Image #N] as its
// name, [▣ shot.png]. at maps each drawn position back to the text's; a
// place inside a name is inside its marker, so a click there picks it.
func (r imageRefs) shown(buf []rune) (out []rune, at []int) {
	s := string(buf)
	if len(r.Path) == 0 || !strings.Contains(s, "[Image #") {
		return buf, nil
	}
	p := 0 // in buf
	for _, loc := range imageMarkerRe.FindAllStringSubmatchIndex(s, -1) {
		id, _ := strconv.Atoi(s[loc[2]:loc[3]])
		path, ok := r.Path[id]
		if !ok {
			continue
		}
		from := utf8.RuneCountInString(s[:loc[0]])
		for ; p < from; p++ {
			out, at = append(out, buf[p]), append(at, p)
		}
		for i, ch := range "[▣ " + chipName(path) + "]" {
			out, at = append(out, ch), append(at, from+min(i, 1))
		}
		p = from + utf8.RuneCountInString(s[loc[0]:loc[1]])
	}
	for ; p < len(buf); p++ {
		out, at = append(out, buf[p]), append(at, p)
	}
	return out, append(at, len(buf))
}

// chipName is an image's name in a box: its file's, cut short when long,
// with nothing in it that would end the chip early.
func chipName(path string) string {
	n := []rune(strings.NewReplacer("]", ")", "\n", " ").Replace(convo.ImageLabel(path)))
	if len(n) > 32 {
		n = append(append(n[:20:20], '…'), n[len(n)-11:]...)
	}
	return string(n)
}
