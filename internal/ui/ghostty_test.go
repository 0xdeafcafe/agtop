package ui

import "testing"

func TestGhosttyOnOff(t *testing.T) {
	was := "palette = 1=#d8647e\nbackground = #141415\nfont-size = 13\nmouse-scroll-multiplier = 1\n"
	on := ghosttyOn(was)
	want := ghosttyHeld + "palette = 1=#d8647e\n" + ghosttyHeld + "background = #141415\nfont-size = 13\nmouse-scroll-multiplier = 1\n" + ghosttyTheme + "\n"
	if on != want {
		t.Fatalf("on:\n%s\nwant:\n%s", on, want)
	}
	if again := ghosttyOn(on); again != on {
		t.Fatalf("on twice changed it:\n%s", again)
	}
	if off := ghosttyOff(on); off != was {
		t.Fatalf("off:\n%q\nwant:\n%q", off, was)
	}
	if on := ghosttyOn(""); on != ghosttyTheme+"\n" {
		t.Fatalf("empty config: %q", on)
	}
}
