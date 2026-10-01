package convo

import "strings"

// folded is a turn the reader collapsed: what you said, exactly as it is
// open, and one dim line where the rest was. Nothing above the line moves.
func (d *drawer) folded() {
	d.head()
	d.lines[len(d.lines)-1].Ref = d.ref // the gap is the turn's too: the box and the line read as one
	meta := strings.ReplaceAll(d.meta(), "   ", " · ")
	if meta != "" {
		meta += " · "
	}
	d.add(d.ref, "", d.spine()+blanks(gutter-1)+faint("▸ ")+d.mark()+dim(meta+"show"), "")
}
