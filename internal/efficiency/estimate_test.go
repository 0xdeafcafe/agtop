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

func TestWorking(t *testing.T) {
	sv := &Saver{ID: "x", Uses: []string{"bash:x"}, Moves: []Metric{MetricToolKB}}
	on := Found{Status: On}
	v := &View{Uses: map[string]*SaverUse{}, split: map[string]*[2]sideAcc{}}

	if w := v.Working(sv, Found{Status: Partial}); w.Verdict != Half {
		t.Fatalf("half set up: %+v", w)
	}
	if w := v.Working(&Saver{ID: "s"}, on); w.Verdict != Unseen {
		t.Fatalf("a setting can't be seen: %+v", w)
	}
	if w := v.Working(sv, on); w.Verdict != Silent {
		t.Fatalf("on but never used: %+v", w)
	}

	v.Uses["x"] = &SaverUse{N: 3, Sessions: 2}
	if w := v.Working(sv, on); w.Verdict != Firing || w.Compared {
		t.Fatalf("used, nothing to compare: %+v", w)
	}
	sp := &[2]sideAcc{}
	sp[0].n, sp[0].b.Calls[ToolBash], sp[0].b.Bytes[ToolBash] = 1, 10, 600
	sp[1].n, sp[1].b.Calls[ToolBash], sp[1].b.Bytes[ToolBash] = 1, 10, 1000
	v.split["x"] = sp
	if w := v.Working(sv, on); !w.Compared || w.Change != -40 || !w.Few || w.Metric != MetricToolKB {
		t.Fatalf("with 60 B/call against 100: %+v", w)
	}
}
