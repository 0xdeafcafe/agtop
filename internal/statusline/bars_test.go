package statusline

import (
	"reflect"
	"testing"
)

func TestOldDefaultBarsMigrateWithoutChangingCustomLayouts(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	old := Layout{Lines: [][]string{{"today", "usage"}, {"ram", "tokens", "cpu", "net", "disk", "battery", "tmp"}}, Sep: " · "}
	if err := SaveBars(Bars{Top: old, Agent: DefaultAgent()}); err != nil {
		t.Fatal(err)
	}
	if got := LoadBars(); !reflect.DeepEqual(got.Top, DefaultTop()) {
		t.Fatalf("old default not migrated: %+v", got.Top)
	}
	custom := Layout{Lines: [][]string{{"cpu", "today", "usage"}, {"battery", "ram"}}, Sep: " | "}
	if err := SaveBars(Bars{Top: custom, Agent: DefaultAgent()}); err != nil {
		t.Fatal(err)
	}
	if got := LoadBars(); !reflect.DeepEqual(got.Top, custom) {
		t.Fatalf("custom layout changed: %+v", got.Top)
	}
	old.Sep = " / "
	if err := SaveBars(Bars{Top: old}); err != nil {
		t.Fatal(err)
	}
	if got := LoadBars(); !reflect.DeepEqual(got.Top, old) {
		t.Fatal("custom separator reset")
	}
}

func TestPreviousQuietDefaultBecomesOneLine(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	previous := Layout{Lines: [][]string{{"today", "plan"}, {"memory", "system", "statushelp"}}, Sep: " · "}
	if err := SaveBars(Bars{Top: previous, Agent: DefaultAgent()}); err != nil {
		t.Fatal(err)
	}
	got := LoadBars()
	if !reflect.DeepEqual(got.Top, DefaultTop()) || len(got.Top.Lines[1]) != 0 {
		t.Fatalf("old quiet default not condensed: %+v", got.Top)
	}
}
