package convo

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestShellLines(t *testing.T) {
	for _, c := range []struct {
		cmd  string
		want []shLine
	}{
		{`cd /x; grep -n '"a"\|";"' *.go | grep -v _test | head`, []shLine{
			{text: "cd /x"},
			{text: `grep -n '"a"\|";"' *.go`},
			{text: "| grep -v _test", depth: 1},
			{text: "| head", depth: 1},
		}},
		{`go build ./... && go test ./x 2>&1 | tail -5 || echo "a; b | c"`, []shLine{
			{text: "go build ./..."},
			{text: "&& go test ./x 2>&1"},
			{text: "| tail -5", depth: 1},
			{text: `|| echo "a; b | c"`},
		}},
		{"cat > x <<'EOF'\na; b | c\nEOF\necho $(ls | wc -l) && ok", []shLine{
			{text: "cat > x <<'EOF'"},
			{text: "a; b | c", verbatim: true},
			{text: "EOF", verbatim: true},
			{text: "echo $(ls | wc -l)"},
			{text: "&& ok"},
		}},
	} {
		if got := shellLines(c.cmd); !reflect.DeepEqual(got, c.want) {
			t.Errorf("shellLines(%q)\n got %+v\nwant %+v", c.cmd, got, c.want)
		}
	}
}

func TestShellShape(t *testing.T) {
	s := New()
	s.Info.Cwd = "/work"
	d := &drawer{s: s}
	for _, c := range []struct {
		cmd, kind, glyph, what, res string
	}{
		{"sed -n 435,446p internal/ui/question_test.go", "read", "◧", "internal/ui/question_test.go", "lines 435–446"},
		{"sed -n '/^## 12/,$p' /work/design.md | head -60", "read", "◧", "design.md", ""},
		{"cat a.go b.go", "read", "◧", "a.go, b.go", ""},
		{"tail -n 20 /tmp/log.txt", "read", "◧", "/tmp/log.txt", ""},
		{`grep -n "m\.scroll" internal/ui/*.go | grep -v _test`, "search", "⌕", `m\.scroll in internal/ui/*.go`, ""},
		{`rg -g '*.go' -e foo`, "search", "⌕", "foo", ""},
		{`grep -n "langFor\|heredocLang" *.go`, "search", "⌕", "langFor|heredocLang in *.go", ""},
		{"cat > internal/ui/zz_test.go <<'EOF'\npackage ui\n\nfunc x() {}\nEOF", "write", "✎", "internal/ui/zz_test.go", "3 lines"},
		{"cat <<'EOF' > out.txt\nhi\nEOF", "write", "✎", "out.txt", "1 line"},
		{"cat <<'EOF' > out.txt && echo done\nhi\nEOF", "", "", "", ""},
		{"cat > a.yaml <<'EOF'\nx: 1\nEOF\nbash build.sh", "", "", "", ""},
		{"python3 - <<'EOF'\nprint(1)\nprint(2)\nEOF", "script", "$", "python3 · print(1)", "2 lines"},
		{"python3 - <<'EOF'\nimport re\np = 'internal/ui/view.go'\ns = open(p).read()\nopen(p, 'w').write(s)\nEOF", "script", "$", "python3 · edits internal/ui/view.go", "4 lines"},
		{`git commit -m "readme: done"`, "commit", "$", "git commit", "“readme: done”"},
		{"cd /elsewhere && cat x.txt", "read", "◧", "x.txt", ""},
		{"sed -n 1,5p a.go; grep x b.go", "", "", "", ""},
		{"grep -rn x internal\ngrep -n y a.go", "", "", "", ""},
		{"grep -n x \\\n  a.go", "search", "⌕", "x in a.go", ""},
		{"go test ./...", "", "", "", ""},
		{"head -5", "", "", "", ""},
	} {
		sh := d.shellShape(c.cmd)
		if sh.kind != c.kind || sh.glyph != c.glyph || sh.what != c.what || sh.res != c.res {
			t.Errorf("shellShape(%q) = %+v, want %s %s %q %q", c.cmd, sh, c.kind, c.glyph, c.what, c.res)
		}
	}
	if sh := d.shellShape("cd /elsewhere && cat x.txt"); sh.in != "/elsewhere" {
		t.Errorf("a cd into another folder should say so, got %q", sh.in)
	}
}

func TestErrorLinePlace(t *testing.T) {
	for out, want := range map[string]string{
		"--- FAIL: TestDoing (0.00s)\n    doing_test.go:22: got x, want y\nFAIL\nFAIL\tgithub.com/x/claude\t0.2s\nFAIL": "doing_test.go:22: got x, want y",
		"# pkg\ninternal/ui/cleanup.go:64:10: m.clean undefined\ninternal/ui/cleanup.go:424:10: too many errors":        "internal/ui/cleanup.go:64:10: m.clean undefined",
	} {
		s := New()
		d := &drawer{s: s, t: &Turn{}, o: Options{Width: 200}, cw: 200}
		d.errorLine(&Step{Tool: "Bash", Status: Failed, Output: out}, 8, "r")
		if len(d.lines) != 1 || !strings.Contains(stripANSI(d.lines[0].Text), want) {
			t.Errorf("errorLine(%q) = %v, want %q", out, d.lines, want)
		}
	}
}

func TestChainOutputHighlighted(t *testing.T) {
	s := New()
	s.Info.Cwd = "/work"
	d := &drawer{s: s, t: &Turn{}, o: Options{Width: 200, Verbose: true, Open: map[string]bool{}}, cw: 200}
	cmd := "sed -n 1,3p main.go; echo ---; cat settings.json; git log --format=%s -1"
	in, _ := json.Marshal(map[string]string{"command": cmd})
	res, _ := json.Marshal(map[string]string{"stdout": "package main\n\nfunc main() {}\n---\n{\"a\": true}\nfix: for the thing\n"})
	d.body(&Step{Tool: "Bash", Input: in, Result: res, Status: OK}, 4)
	var out string
	for _, l := range d.lines {
		out += l.Text + "\n"
	}
	for _, want := range []string{hlKw + "package", hlKw + "func", hlNum + "true"} {
		if !strings.Contains(out, want) {
			t.Errorf("chain output should highlight %q:\n%q", want, out)
		}
	}
	if strings.Contains(out, hlKw+"---") {
		t.Errorf("the echo's line isn't code:\n%q", out)
	}
}

// A search's hits start their code in one column, less the indentation
// they all share.
func TestSearchHitsAligned(t *testing.T) {
	s := New()
	s.Info.Cwd = "/work"
	d := &drawer{s: s, t: &Turn{}, o: Options{Width: 200, Verbose: true, Open: map[string]bool{}}, cw: 200}
	in, _ := json.Marshal(map[string]string{"command": `grep -rn "x" .`})
	out := "render.go:2267:\t\tlg := x\n../ui/docstyle.go:35:\t\t\tif x {\n"
	res, _ := json.Marshal(map[string]string{"stdout": out})
	d.body(&Step{Tool: "Bash", Input: in, Result: res, Status: OK}, 4)
	var got []string
	for _, l := range d.lines {
		if t := stripANSI(l.Text); strings.Contains(t, ".go:") {
			got = append(got, strings.TrimSpace(strings.TrimLeft(t, " ▏")))
		}
	}
	want := []string{"render.go:2267:      lg := x", "../ui/docstyle.go:35:    if x {"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("hits = %q, want %q", got, want)
	}
}

// Two greps on their own lines, the second with lines around its match:
// each part is highlighted in its own file's language, and a string one
// match leaves open doesn't colour the next.
func TestSearchChainHighlighted(t *testing.T) {
	s := New()
	s.Info.Cwd = "/work"
	d := &drawer{s: s, t: &Turn{}, o: Options{Width: 200, Verbose: true, Open: map[string]bool{}}, cw: 200}
	cmd := "grep -rn \"type Agent\" internal\ngrep -n \"func x\" -A2 a-b/c.go | head -30"
	in, _ := json.Marshal(map[string]string{"command": cmd})
	out := "internal/fleet/fleet.go:20:type Agent struct { s := `open\n" +
		"a-b/x.go:9:\treturn nil\n" +
		"823:func x() {\n824-\treturn nil\n825-}\n"
	res, _ := json.Marshal(map[string]string{"stdout": out})
	d.body(&Step{Tool: "Bash", Input: in, Result: res, Status: OK}, 4)
	var got []string
	for _, l := range d.lines {
		got = append(got, l.Text)
	}
	all := strings.Join(got, "\n")
	for _, want := range []string{hlKw + "type", hlKw + "func", hlKw + "return"} {
		if !strings.Contains(all, want) {
			t.Errorf("search chain output should highlight %q:\n%q", want, all)
		}
	}
	if strings.Count(all, hlKw+"return") != 2 {
		t.Errorf("the open string shouldn't reach the next match:\n%q", all)
	}
}

// A script's heredoc, a grep and a git diff in one step: the grep's hits
// are highlighted in their file's language after the script's output, and
// the diff's lines are coloured by what they do.
func TestHeredocChainWithDiff(t *testing.T) {
	s := New()
	s.Info.Cwd = "/work"
	d := &drawer{s: s, t: &Turn{}, o: Options{Width: 200, Verbose: true, Open: map[string]bool{}}, cw: 200}
	cmd := "python3 - <<'EOF'\nprint('ok')\nEOF\ngrep -n \"confirm\" internal/ui/keys.go | head\ngit diff internal/ui/fleetslash.go | head -20"
	in, _ := json.Marshal(map[string]string{"command": cmd})
	out := "patched\n40:\t\tif m.confirm != nil {\n" +
		"diff --git a/internal/ui/fleetslash.go b/internal/ui/fleetslash.go\n" +
		"--- a/internal/ui/fleetslash.go\n+++ b/internal/ui/fleetslash.go\n@@ -1,2 +1,3 @@\n" +
		" \tvar x = 1\n+\treturn nil\n-\tbreak\n"
	res, _ := json.Marshal(map[string]string{"stdout": out})
	d.body(&Step{Tool: "Bash", Input: in, Result: res, Status: OK}, 4)
	var got []string
	for _, l := range d.lines {
		got = append(got, l.Text)
	}
	all := strings.Join(got, "\n")
	for _, want := range []string{hlKw + "if", hlKw + "var", hlKw + "return", hlKw + "break", plusSign(), minusSign(), paint(cBlue, "@@ -1,2 +1,3 @@")} {
		if !strings.Contains(all, want) {
			t.Errorf("output should have %q:\n%q", want, all)
		}
	}
}
