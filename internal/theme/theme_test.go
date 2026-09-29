package theme

import "testing"

var (
	homebrew  = Ground{BG: RGB{0, 0, 0}, FG: RGB{40, 254, 20}}
	solarized = Ground{BG: RGB{0, 43, 54}, FG: RGB{131, 148, 150}} // its text is only 4.7:1
	white     = Ground{BG: RGB{255, 255, 255}, FG: RGB{0, 0, 0}}
	text      = RGB{226, 221, 211}
	dim       = RGB{138, 132, 122}
	orange    = RGB{217, 119, 87}
	sel       = RGB{0x2c, 0x28, 0x24}
	add       = RGB{0x16, 0x30, 0x1a}
)

func TestDarkIsRushsOwnColours(t *testing.T) {
	for _, c := range []RGB{text, dim, orange, sel, add} {
		if Dark.Ink(c) != c || Dark.Surface(c) != c || Dark.Accent(c) != c || Dark.Quiet(c, orange) != c {
			t.Errorf("%v moved on rush's own ground", c)
		}
	}
}

func TestTerminal(t *testing.T) {
	if g := Terminal(nil, nil); g != Dark {
		t.Errorf("a terminal that says nothing: %v, want Dark", g)
	}
	bg := RGB{255, 255, 255}
	if g := Terminal(&bg, nil); g.FG != Light.FG {
		t.Errorf("a light terminal that doesn't say its text: %v, want Light's", g.FG)
	}
	fg := RGB{250, 250, 250}
	if g := Terminal(&bg, &fg); g.FG != Light.FG {
		t.Errorf("text the background hides was kept: %v", g.FG)
	}
	if g := Terminal(&homebrew.BG, &homebrew.FG); g != homebrew {
		t.Errorf("Homebrew: %v", g)
	}
}

func TestTextFollowsTheTerminal(t *testing.T) {
	// On Homebrew rush's greys are greens.
	if c := homebrew.Ink(text); c.G <= c.R || c.G <= c.B {
		t.Errorf("Homebrew text %v isn't green", c)
	}
	for _, g := range []Ground{homebrew, white, Light, solarized} {
		if r, full := Contrast(g.Ink(text), g.BG), Contrast(g.FG, g.BG); r < full-0.05 {
			t.Errorf("%v: text at %.1f:1, want the terminal's %.1f", g, r, full)
		}
		if r := Contrast(g.Ink(dim), g.BG); r < 4.5 {
			t.Errorf("%v: dim text at %.1f:1, want 4.5", g, r)
		}
	}
}

func TestAccentsStayReadable(t *testing.T) {
	want := min(readable, Contrast(orange, Dark.BG))
	for _, g := range []Ground{homebrew, white, Light, solarized} {
		if r := Contrast(g.Accent(orange), g.BG); r < want-0.05 {
			t.Errorf("%v: orange at %.1f:1, want %.1f", g, r, want)
		}
	}
}

func TestSurfacesSitOnTheGround(t *testing.T) {
	for _, g := range []Ground{white, Light} {
		s := g.Surface(sel)
		if s == g.BG || s.Dark() {
			t.Errorf("%v: selection %v, want a light step off the background", g, s)
		}
		if a := g.Surface(add); a.G < a.R || a.G < a.B || a.Dark() {
			t.Errorf("%v: added line %v, want a light green", g, a)
		}
	}
	if s := homebrew.Surface(sel); s == homebrew.BG || !s.Dark() {
		t.Errorf("Homebrew: selection %v, want a dark step off black", s)
	}
}
