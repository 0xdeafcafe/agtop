package sysinfo

import "testing"

func TestParsePmset(t *testing.T) {
	for _, c := range []struct {
		out  string
		want Battery
	}{
		{"Now drawing from 'AC Power'\n -InternalBattery-0 (id=28442723)\t7%; AC attached; not charging present: true\n", Battery{true, 7, true}},
		{"Now drawing from 'Battery Power'\n -InternalBattery-0 (id=1)\t64%; discharging; 3:12 remaining present: true\n", Battery{true, 64, false}},
		{"Now drawing from 'AC Power'\n", Battery{}}, // a desktop
	} {
		if got := parsePmset(c.out); got != c.want {
			t.Errorf("parsePmset(%q) = %+v, want %+v", c.out, got, c.want)
		}
	}
}
