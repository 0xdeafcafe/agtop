package ui

import (
	"testing"
)

// ctrl+s in the bar steps the agents' order; by project, each repository
// is a section of its own, latest first.
func TestBarSorts(t *testing.T) {
	m, _ := benchModel(200, 45)
	m.snap.Agents[1].Repo = "/work/other"
	m.barSort = "project"
	gs := m.barAgentGroups("")
	if len(gs) != 2 || gs[0].title != "rush" || gs[1].title != "other" {
		t.Fatalf("by project: %+v", titles(gs))
	}
	m.barSort = "latest"
	if gs := m.barAgentGroups(""); len(gs) != 1 || len(gs[0].items) != 30 || gs[0].items[0].title != "agent number 0 doing things" {
		t.Fatalf("latest: %v", titles(gs))
	}
	m.barSort = "harness"
	if gs := m.barAgentGroups("number 1"); len(gs) == 0 {
		t.Fatal("a query found nothing by harness")
	}
}

func titles(gs []barGroup) []string {
	var out []string
	for _, g := range gs {
		out = append(out, g.title)
	}
	return out
}
