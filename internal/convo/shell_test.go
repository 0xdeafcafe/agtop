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
		{"cat > internal/ui/zz_test.go <<'EOF'\npackage ui\n\nfunc x() {}\nEOF", "write", "✎", "internal/ui/zz_test.go", "3 lines"},
		{"cat <<'EOF' > out.txt\nhi\nEOF", "write", "✎", "out.txt", "1 line"},
		{"cat <<'EOF' > out.txt && echo done\nhi\nEOF", "", "", "", ""},
		{"cat > a.yaml <<'EOF'\nx: 1\nEOF\nbash build.sh", "", "", "", ""},
		{"python3 - <<'EOF'\nprint(1)\nprint(2)\nEOF", "script", "$", "python3 · print(1)", "2 lines"},
		{"python3 - <<'EOF'\nimport re\np = 'internal/ui/view.go'\ns = open(p).read()\nopen(p, 'w').write(s)\nEOF", "script", "$", "python3 · edits internal/ui/view.go", "4 lines"},
		{`git commit -m "readme: done"`, "commit", "$", "git commit", "“readme: done”"},
		{"cd /elsewhere && cat x.txt", "read", "◧", "x.txt", ""},
		{"sed -n 1,5p a.go; grep x b.go", "", "", "", ""},
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
