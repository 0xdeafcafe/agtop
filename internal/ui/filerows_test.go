package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A file a command writes reads as the document it is: its tail numbered
// as the lines are in the whole file, past the part that wasn't read.
func TestFileRowsNumbered(t *testing.T) {
	p := filepath.Join(t.TempDir(), "big.go")
	var b strings.Builder
	for i := 1; i <= 3000; i++ {
		fmt.Fprintf(&b, "var x%d = %d // padding so the file is past one read\n", i, i)
	}
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, nos := tailLines(p, 3)
	if len(lines) != 3 || len(nos) != 3 || nos[2] != 3000 || lines[2] != "var x3000 = 3000 // padding so the file is past one read" {
		t.Fatalf("tail %q numbered %v", lines, nos)
	}
	c := &hostConn{tails: map[string]*jobTailed{p: {lines: lines, nos: nos}}}
	got := ansi.Strip(strings.Join(c.fileRows(p, lines[1:], 2, 120), "\n"))
	if !strings.Contains(got, "  2999  var x2999") || !strings.Contains(got, "  3000  var x3000") {
		t.Errorf("rows:\n%s", got)
	}
	log := filepath.Join(t.TempDir(), "run.log")
	if rows := c.fileRows(log, []string{"ok"}, 2, 80); !strings.Contains(ansi.Strip(rows[0]), "│ ok") {
		t.Errorf("a log stays as it is: %q", rows[0])
	}
}
