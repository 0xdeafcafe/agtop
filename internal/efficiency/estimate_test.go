package efficiency

import (
	"testing"
	"time"
)

func TestEstimate(t *testing.T) {
	v := &View{Q: Query{From: time.Now().Add(-30 * 24 * time.Hour), To: time.Now()}}
	v.Carry[ToolBash], v.Carry[ToolRead], v.LookCarry = 100, 40, 60
	v.Total.COut, v.Total.Out, v.Total.Think = 50, 1000, 400

	shell := &Saver{ID: "a", Cut: &Cut{Of: BaseShell, Low: 0.1, High: 0.5}}
	look := &Saver{ID: "b", Cut: &Cut{Of: BaseLook, Low: -0.1, High: 0.3}}
	text := &Saver{ID: "c", Cut: &Cut{Of: BaseText, Low: 0.5, High: 0.5}}
	none := &Saver{ID: "d"}

	if e, ok := v.Estimate(shell); !ok || e.Base != 100 || e.Low != 10 || e.High != 50 {
		t.Fatalf("shell: %+v", e)
	}
	if e, _ := v.Estimate(look); e.Base != 100 || e.Low != -10 || e.High != 30 {
		t.Fatalf("searching and reading is Read and the shell's searches: %+v", e)
	}
	if e, _ := v.Estimate(text); e.Base != 30 {
		t.Fatalf("what Claude writes leaves out thinking: %+v", e)
	}
	if _, ok := v.Estimate(none); ok {
		t.Fatal("a saver with no cut has no estimate")
	}
	list := []*Saver{none, text, look, shell}
	v.ByEstimate(list, nil)
	if list[0] != shell || list[3] != none {
		t.Fatalf("by estimate: %s %s %s %s", list[0].ID, list[1].ID, list[2].ID, list[3].ID)
	}
}
