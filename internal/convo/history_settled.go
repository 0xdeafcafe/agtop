package convo

import (
	"slices"
	"strconv"
	"strings"

	"github.com/0xdeafcafe/rush/internal/agent/tool"
)

// settled draws a turn you've gone past as what you'd look back for: what
// you said, what came straight back and what it ended on, the words either
// side of anything you sent mid-turn, and what the work made (pictures,
// drawings, commits, tests, failures). The steps between fold to a row a
// run saying what they changed; opened, they draw as an open turn's do.
func (d *drawer) settled() {
	d.head()
	items := d.t.Items
	first := slices.IndexFunc(items, func(it *Item) bool { return it.Kind == KStep })
	near := func(j int) bool { return j >= 0 && j < len(items) && items[j].Kind == KInterject }
	var run []*Item
	at := 0
	flush := func() {
		if len(run) > 0 {
			d.settledRun(d.ref+":run:"+strconv.Itoa(at), run)
			run = nil
		}
	}
	for i, it := range items {
		switch {
		case it.Kind == KStep && !d.artifact(it.Step):
			if len(run) == 0 {
				at = i
			}
			run = append(run, it)
			continue
		case it.Kind == KThinking:
			continue
		case it.Kind == KText && !(it.Answer || first < 0 || i < first || near(i-1) || near(i+1)):
			continue // narration between steps
		}
		flush()
		d.item(it)
	}
	flush()
	switch {
	case d.t.Stopped:
		d.add("", "", d.spine()+"   "+dim("⏹ stopped"), "")
	case d.t.Err != "":
		d.add("", "", d.spine()+blanks(gutter-1)+paint(cRed, "✗ "+d.t.Err), "")
	}
	if m := d.meta(); m != "" && d.t.Steps()+d.t.agentSteps() > 0 {
		d.add("", "", d.spine(), dim(m)+"  ")
	}
}

// artifact is a step whose result is the point of looking back: a picture
// it read or was given, or a drawing it showed.
func (d *drawer) artifact(st *Step) bool {
	return len(st.Images) > 0 || st.kind() == tool.Read && thumbable(d.abs(st.in().Path)) || strings.HasSuffix(st.Tool, "__show")
}

// settledRun is a run of steps in a turn gone past: one row saying how
// many, the files they changed and by how much, and any that failed, then
// the cards for what they made. Opened, every step as an open turn has it.
func (d *drawer) settledRun(ref string, run []*Item) {
	if d.o.Open[ref] {
		d.railed(gutter-2, true, func() {
			d.add(ref, "", d.spine()+blanks(gutter-1)+faint("▾ hide "+plural(len(run), "step")), "")
			for _, x := range run {
				d.step(x.Step, 0)
			}
		})
		return
	}
	files := map[string]bool{}
	add, del, failed := 0, 0, 0
	for _, x := range run {
		st := x.Step
		if st.Status == Failed {
			failed++
		}
		if k := st.kind(); st.Status != OK || k != tool.Edit && k != tool.Write {
			continue
		}
		// Counted as the changes view counts them: from what it did.
		files[st.in().Path] = true
		if o := st.out(); o.Created {
			add += countLines(st.in().Content)
		} else {
			for _, p := range o.Patches {
				for _, l := range p.Lines {
					switch {
					case strings.HasPrefix(l, "+"):
						add++
					case strings.HasPrefix(l, "-"):
						del++
					}
				}
			}
		}
	}
	left := d.spine() + blanks(gutter-1) + faint("▸ "+plural(len(run), "step"))
	if len(files) > 0 {
		left += faint(" · ") + dim(plural(len(files), "file")+" changed ") + paint(cGreen, "+"+strconv.Itoa(add)) + " " + paint(cRed, "−"+strconv.Itoa(del))
	}
	if failed > 0 {
		left += faint(" · ") + paint(cRed, "✗ "+strconv.Itoa(failed)+" failed")
	}
	d.worked = true
	d.add(ref, "", left, "")
	for _, x := range run {
		d.cards(x.Step, gutter+2)
	}
}
