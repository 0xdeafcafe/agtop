package ui

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/0xdeafcafe/agtop/internal/headless"
)

// imageRefs are the images a Session's box holds, each standing in the text
// as [Image #N] where it was pasted or dropped, as Claude Code shows them.
// The marker is the image: deleting it takes the image off the message.
type imageRefs struct {
	N    int            `json:"n,omitempty"`    // the last number given
	Path map[int]string `json:"path,omitempty"` // each number's file
}

var imageMarkerRe = regexp.MustCompile(`\[Image #(\d+)\]`)

// add keeps an image file and returns the marker that stands for it.
func (r *imageRefs) add(path string) string {
	if r.Path == nil {
		r.Path = map[int]string{}
	}
	r.N++
	r.Path[r.N] = path
	return headless.ImageMarker(r.N)
}

// inline puts a marker in place of each image file named in text: dropped
// or typed paths become the images they name, where they were.
func (r *imageRefs) inline(text string) (string, bool) {
	var b strings.Builder
	last, found := 0, false
	for _, sp := range pathSpans(text) {
		p := imagePath(sp.tok)
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
		return headless.ImageMarker(n)
	})
	return out, images
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
var chipRe = regexp.MustCompile(`\[Pasted text #\d+ \+\d+ lines\]|\[Image #\d+\]`)

// dropChipAfter deletes a whole chip or marker when delete lands on its
// start.
func dropChipAfter(buf []rune, pos int) ([]rune, bool) {
	if pos >= len(buf) || buf[pos] != '[' {
		return buf, false
	}
	after := string(buf[pos:])
	loc := chipRe.FindStringIndex(after)
	if loc == nil || loc[0] != 0 {
		return buf, false
	}
	n := len([]rune(after[:loc[1]]))
	return append(append([]rune{}, buf[:pos]...), buf[pos+n:]...), true
}
