// Package theme makes agtop's colours from the terminal's own: its
// background and its text. Every colour agtop draws is written as it looks
// on agtop's dark ground (Dark), and moved onto the terminal's ground by
// what kind of colour it is: text, a surface, or an accent.
package theme

import (
	"fmt"
	"image/color"
	"math"
)

// RGB is a 24-bit colour.
type RGB struct{ R, G, B uint8 }

// Of is c as 24 bits.
func Of(c color.Color) RGB {
	r, g, b, _ := c.RGBA()
	return RGB{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)}
}

// FG is the code that sets text to c.
func (c RGB) FG() string { return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", c.R, c.G, c.B) }

// BG is the code that sets the background to c.
func (c RGB) BG() string { return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", c.R, c.G, c.B) }

// Dark is whether c is nearer black than white, to the eye.
func (c RGB) Dark() bool { return Contrast(c, RGB{255, 255, 255}) > Contrast(c, RGB{}) }

func clamp(v float64) uint8 { return uint8(math.Round(min(255, max(0, v)))) }

// Mix is t of the way from a to b; t past 0 or 1 carries on beyond them.
func Mix(a, b RGB, t float64) RGB {
	l := func(x, y uint8) uint8 { return clamp(float64(x) + (float64(y)-float64(x))*t) }
	return RGB{l(a.R, b.R), l(a.G, b.G), l(a.B, b.B)}
}

// luminance is c's relative luminance, as WCAG has it.
func luminance(c RGB) float64 {
	ch := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(c.R) + 0.7152*ch(c.G) + 0.0722*ch(c.B)
}

// Contrast is the WCAG contrast ratio of a and b, from 1 to 21.
func Contrast(a, b RGB) float64 {
	la, lb := luminance(a), luminance(b)
	return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)
}

// Ground is a terminal's background and text, what every colour is made
// from.
type Ground struct{ BG, FG RGB }

var (
	// Dark is agtop's own ground, the one its colours are written for, and
	// what's used when the terminal doesn't say what it has.
	Dark = Ground{BG: RGB{17, 16, 14}, FG: RGB{226, 221, 211}}
	// Light is agtop's light ground, for a light terminal that doesn't say.
	Light = Ground{BG: RGB{250, 249, 245}, FG: RGB{40, 37, 32}}
)

// Terminal is the ground for a terminal whose background is bg and text
// fg, either of which it may not have said (nil). Text too close to the
// background to read is replaced by agtop's own for that kind of ground.
func Terminal(bg, fg *RGB) Ground {
	if bg == nil {
		return Dark
	}
	ref := Dark
	if !bg.Dark() {
		ref = Light
	}
	g := Ground{BG: *bg, FG: ref.FG}
	if fg != nil && Contrast(*bg, *fg) >= readable {
		g.FG = *fg
	}
	return g
}

// along is how far c lies from Dark's background toward its text, 0 at
// the background and 1 at the text.
func along(c RGB) float64 {
	return towards(Dark.BG, Dark.FG, c)
}

// towards is how far c lies from a toward b: its projection on the line
// between them.
func towards(a, b, c RGB) float64 {
	dr, dg, db := float64(b.R)-float64(a.R), float64(b.G)-float64(a.G), float64(b.B)-float64(a.B)
	cr, cg, cb := float64(c.R)-float64(a.R), float64(c.G)-float64(a.G), float64(c.B)-float64(a.B)
	n := dr*dr + dg*dg + db*db
	if n == 0 {
		return 0
	}
	return (cr*dr + cg*dg + cb*db) / n
}

// Ink is text or a rule, c on Dark: a shade between this ground's
// background and text that stands out from the background as c does from
// Dark's, scaled to how much the terminal's own text does. On Homebrew's
// green text, agtop's greys are greens.
func (g Ground) Ink(c RGB) RGB {
	if g == Dark {
		return c
	}
	if t := along(c); t > 1 {
		return Mix(g.BG, g.FG, t) // brighter than text: past it
	}
	// Contrast scaled in log, so the text maps to the text and the
	// background to the background.
	was, full := Contrast(c, Dark.BG), Contrast(g.FG, g.BG)
	want := math.Exp(math.Log(was) * math.Log(full) / math.Log(Contrast(Dark.FG, Dark.BG)))
	if was >= readable {
		// What you read stays readable, as far as the terminal's text is.
		want = max(want, min(readable, full))
	}
	lo, hi := 0.0, 1.0
	for range 20 {
		t := (lo + hi) / 2
		if Contrast(Mix(g.BG, g.FG, t), g.BG) < want {
			lo = t
		} else {
			hi = t
		}
	}
	return Mix(g.BG, g.FG, hi)
}

// Surface is a ground for a row or panel, c on Dark: as far from the
// background as c is on Dark's, carrying whatever hue c adds to it. On a
// light ground the hue is taken away from the channels it adds least to,
// so a green row is still green and not a glare.
func (g Ground) Surface(c RGB) RGB {
	if g == Dark {
		return c
	}
	d := [3]float64{
		float64(c.R) - float64(Dark.BG.R),
		float64(c.G) - float64(Dark.BG.G),
		float64(c.B) - float64(Dark.BG.B),
	}
	lo, hi := min(d[0], d[1], d[2]), max(d[0], d[1], d[2])
	// The part every channel moves by is a step toward the text, as
	// Ink; the rest is the hue.
	span := (float64(Dark.FG.R) + float64(Dark.FG.G) + float64(Dark.FG.B) -
		float64(Dark.BG.R) - float64(Dark.BG.G) - float64(Dark.BG.B)) / 3
	base := Mix(g.BG, g.FG, lo/span)
	hue := func(i int, v uint8) uint8 {
		if g.BG.Dark() {
			return clamp(float64(v) + d[i] - lo)
		}
		return clamp(float64(v) - (hi - d[i]))
	}
	return RGB{hue(0, base.R), hue(1, base.G), hue(2, base.B)}
}

// readable is the contrast text needs to be read with ease: WCAG's AA.
const readable = 4.5

// Accent is a colour that says something (done, failed, agtop's orange),
// c on Dark: the same colour, pushed toward the text until it stands out
// from this ground as much as it does from Dark's, or is readable.
func (g Ground) Accent(c RGB) RGB {
	if g == Dark {
		return c
	}
	want := min(readable, Contrast(c, Dark.BG))
	end := RGB{255, 255, 255}
	if !g.BG.Dark() {
		end = RGB{}
	}
	out := c
	for t := 0.05; Contrast(out, g.BG) < want && t <= 1; t += 0.05 {
		out = Mix(c, end, t)
	}
	return out
}

// Quiet is an accent held back toward the background, c on Dark: as far
// from the background toward accent (on Dark) as c is, on this ground.
func (g Ground) Quiet(c, accent RGB) RGB {
	if g == Dark {
		return c
	}
	return Mix(g.BG, g.Accent(accent), towards(Dark.BG, accent, c))
}
