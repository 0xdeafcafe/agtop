package convo

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// od -c's lines become hex rows one for one, its octals and escapes read
// back to the bytes they were.
func TestHexRows(t *testing.T) {
	// printf 'aé\xef\xbb\xbfb\xff\n' | od -c, with LANG=en_NZ.UTF-8 on macOS.
	out := []string{
		"before",
		"0000000    a   é  ** 357 273 277   b 377  \\n                            ",
		"0000011",
		"after",
	}
	rows := hexRows(out)
	if _, ok := rows[2]; len(rows) != 2 || !ok {
		t.Fatalf("want the dump's two lines, got %d", len(rows))
	}
	got := ansi.Strip(rows[1])
	if !strings.HasPrefix(got, "00000000  61 c3 a9 ef bb bf 62 ff  0a") || !strings.HasSuffix(got, "│a·····b··│") {
		t.Errorf("row = %q", got)
	}
	// The byte macOS's od loses, splitting a character over lines, is marked.
	// printf '\t} else if ... "\xef\xbb\xbf...' | od -c: the BOM split over two lines.
	garbled := []string{
		"0000100    s   t   r   i   n   g   (   d   a   t   a   )   ,       \" 273",
		"0000120  273 277   \"   )   )   )  \\n                                    ",
		"0000127",
	}
	garbled[0] = "0000000" + garbled[0][7:]
	garbled[1] = "0000020" + garbled[1][7:]
	garbled[2] = "0000027"
	rows = hexRows(garbled)
	if got := ansi.Strip(rows[0]); !strings.Contains(got, "22 ??  │") || !strings.HasSuffix(got, `"?│`) {
		t.Errorf("the byte od lost should be ??: %q", got)
	}
	if got := ansi.Strip(rows[1]); !strings.HasPrefix(got, "00000010  bb bf 22") {
		t.Errorf("the line after is as od printed it: %q", got)
	}
	if rows := hexRows([]string{"0000000 is a colour"}); rows != nil {
		t.Errorf("not od: %v", rows)
	}
}
