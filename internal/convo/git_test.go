package convo

import (
	"strings"
	"testing"
)

// A chain that reads a file, then asks git what's changed and what came
// before: the code keeps its language, and git's lines their own colours.
func TestChainGitStatusAndLog(t *testing.T) {
	s := New()
	s.Info.Cwd = "/work"
	d := &drawer{s: s, t: &Turn{}, o: Options{Width: 120, Open: map[string]bool{}, Verbose: true}, cw: 120}
	d.spans = d.chainSpans(`sed -n 1,2p internal/convo/style.go; git status --short; git log --oneline -3`)
	d.output(strings.Join([]string{
		"// SetColorBlind swaps green and red",
		"func SetColorBlind(on bool) {",
		" M internal/convo/model.go",
		"?? internal/convo/cards.go",
		"2c67fd6 feat(convo)!: an opened shell step lays its chain out",
		"3225847 (HEAD -> main) Merge the menubar",
	}, "\n"), 8, false)
	if len(d.lines) != 6 {
		t.Fatalf("got %d lines, want 6", len(d.lines))
	}
	for i, want := range []string{
		highlight(langFor("a.go"), &hlState{}, "// SetColorBlind swaps green and red", cOut, nil),
		"",
		paint(cYellow, "M") + " " + paint(cOut, "internal/convo/model.go"),
		paint(cDim, "?") + paint(cDim, "?") + " ",
		paint(cYellow, "2c67fd6") + paint(cOut, "") + " " + paint(cBlue, "feat") + paint(cDim, "(convo)") + paint(cRed, "!") + faint(":") + paint(cSub+bold, " an opened shell step lays its chain out"),
		faint("(") + paint(cGreen, "HEAD -> main") + faint(")") + " " + paint(cSub+bold, "Merge the menubar"),
	} {
		// The row's background comes back after each colour ends.
		if got := strings.ReplaceAll(d.lines[i].Text, bgWell, ""); want != "" && !strings.Contains(got, want) {
			t.Errorf("line %d: %q\n doesn't have %q", i, got, want)
		}
	}
}

// A log on its own, in full: the hash after "commit", and the first line of
// each message as its title.
func TestGitLogFull(t *testing.T) {
	d := &drawer{}
	var got []string
	for _, l := range []string{"commit 2c67fd6aa", "Author: A <a@b>", "", "    fix: the thing", "", "    More words."} {
		got = append(got, d.gitLine("log", l))
	}
	if !strings.Contains(got[0], paint(cYellow, "2c67fd6aa")) {
		t.Errorf("hash: %q", got[0])
	}
	if !strings.Contains(got[3], paint(cBlue, "fix")) {
		t.Errorf("title: %q", got[3])
	}
	if got[5] != paint(cOut, "    More words.") {
		t.Errorf("body: %q", got[5])
	}
	if s := New(); len((&drawer{s: s}).chainSpans("git log -5")) != 1 {
		t.Error("a log on its own has no span")
	}
}
