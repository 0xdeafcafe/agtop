package convo

import "testing"

// A screen drawn for another terminal comes back as its text: no padding,
// no gap to the right edge, one ~ row and one blank row where there were
// many.
func TestUnscreen(t *testing.T) {
	in := "── memory.md" + "                              " + "markdown   \n  1 ---   \n\n\n\n  ~\n  ~\n  ~\n" + "                                        ln 1, col 1   \nok\tpkg  3s"
	want := "── memory.md   markdown\n  1 ---\n\n  ~\n" + "                                        ln 1, col 1\nok\tpkg  3s"
	if got := unscreen(in); got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}
