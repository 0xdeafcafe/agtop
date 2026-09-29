package ui

import (
	"strings"
	"testing"
)

func TestShownDirsFilterOrPath(t *testing.T) {
	p := &picker{dirs: []string{"/src/agtop", "/src/langwatch", "/work/agtop-docs"}}
	if got := p.shownDirs(); len(got) != 3 {
		t.Fatalf("nothing typed shows every folder, got %v", got)
	}
	p.query = []rune("agtop")
	if got := p.shownDirs(); len(got) != 2 || got[0] != "/src/agtop" || got[1] != "/work/agtop-docs" {
		t.Fatalf("typing narrows the list, got %v", got)
	}
	p.query = []rune("/elsewhere/new")
	if got := p.shownDirs(); len(got) != 1 || got[0] != "/elsewhere/new" {
		t.Fatalf("a typed path is offered itself, got %v", got)
	}
	p.query = []rune("/src/agtop")
	if got := p.shownDirs(); len(got) != 1 || got[0] != "/src/agtop" {
		t.Fatalf("a typed path that's listed is offered once, got %v", got)
	}
}

func TestMoveNoteCdsFirst(t *testing.T) {
	n := moveNote("/src/my repo", "/src/old")
	if !strings.Contains(n, "`cd '/src/my repo'`") || !strings.Contains(n, "/src/old") {
		t.Fatalf("the note should cd, quoted, and name where it was: %s", n)
	}
}
