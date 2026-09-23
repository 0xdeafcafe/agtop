package convo

import (
	"reflect"
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
