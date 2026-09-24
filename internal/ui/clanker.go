package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

type mood int

const (
	moodIdle mood = iota
	moodWorking
	moodNeedsYou
	moodSleepy
)

// clkState is everything clanker is drawn from.
type clkState struct {
	md   mood
	tick int   // seconds
	fx   clkFX // what he's reacting to, drawn at its own quicker pace
	mark bool  // he's the "ag" monogram for now
	rich bool  // today's spend is high: now and then a coin drops off him
}

// clanker draws the header: a pixel invader, solid in one colour, who now
// and then fades into the "ag" monogram and back, a light passing over
// whichever he is. His body never moves; what changes is his colour, his
// pose, his eyes, and what drifts off him.
func clanker(s clkState) []string {
	var g clkGrid
	switch s.fx.kind {
	case fxMorphIn:
		n := fxLen[fxMorphIn] - clkShine
		if s.fx.frame < n {
			g.crossfade(s, false, s.fx.frame, n)
			return g.lines()
		}
		g.mark(s.md)
		g.shimmer(s.fx.frame-n, clkShine, cText, .7)
		return g.lines()
	case fxMorphOut:
		g.crossfade(s, true, s.fx.frame, fxLen[fxMorphOut])
		return g.lines()
	}
	if s.mark && s.fx.kind == fxNone && (s.md == moodIdle || s.md == moodWorking) {
		g.mark(s.md)
		return g.lines()
	}
	g.sprite(s)
	return g.lines()
}

const (
	clkCycle = 45 // seconds from one turn into the monogram to the next
	clkShown = 4  // seconds the monogram stays
	clkShine = 14 // frames a light takes to pass over him
	clkW     = 15 // every frame's width, so the header's text never moves
	clkH     = 4
	clkX     = 2 // where he stands; the two columns either side are for what drifts off him
	clkBodyW = 11
	clkFace  = 1 // the row with his eyes, all a narrow header has room for
)

// He's 11×8 pixels, two to a row: ▀ is a top pixel, ▄ a bottom one. clkCrab
// is his rest pose, clkArms his other step, arms up.
var (
	clkCrab = []string{
		"  ▀▄   ▄▀  ",
		" ▄█▀███▀█▄ ",
		"█▀███████▀█",
		"▀ ▀▄▄ ▄▄▀ ▀",
	}
	clkArms = []string{
		"▄ ▀▄   ▄▀ ▄",
		"█▄█▀███▀█▄█",
		"▀█████████▀",
		" ▄▀     ▀▄ ",
	}
	clkEyes = [2]int{3, 7} // columns of his eyes on clkFace: pixel holes under his brow
	clkAG   = []string{
		" ▄▄▄   ▄▄▄▄",
		" ▄▄▄█ █   █",
		"▀▄▄▄█  ▀▀▀█",
		"       ▄▄▄▀",
	}
)

// clkGrid is a frame as cells, so what drifts off him lands around him
// without touching him, and a light can pass over only his body.
type clkGrid struct {
	r    [clkH][clkW]rune
	c    [clkH][clkW]string // foreground
	bg   [clkH][clkW]string // background, for an eye lit inside his head
	body [clkH][clkW]bool
	on   bool // he's drawn: fx keeps off his body
}

// put writes s from (y, x); its spaces leave cells as they are.
func (g *clkGrid) put(y, x int, s, c string, body bool) {
	for _, r := range s {
		if r != ' ' && y >= 0 && y < clkH && x >= 0 && x < clkW {
			g.r[y][x], g.c[y][x], g.body[y][x] = r, c, body
		}
		x++
	}
}

// dot drops one cell of fx, only where it's empty and off his body.
func (g *clkGrid) dot(y, x int, r rune, c string) {
	if y < 0 || y >= clkH || x < 0 || x >= clkW || g.r[y][x] != 0 {
		return
	}
	if g.on && y > 0 && x >= clkX && x < clkX+clkBodyW {
		return
	}
	g.r[y][x], g.c[y][x] = r, c
}

func (g *clkGrid) lines() []string {
	out := make([]string, clkH)
	for y := range g.r {
		var sb strings.Builder
		for x := 0; x < clkW; {
			c, bg, run := g.c[y][x], g.bg[y][x], x
			for run < clkW && g.c[y][run] == c && g.bg[y][run] == bg {
				run++
			}
			cells := []rune(string(g.r[y][x:run]))
			for i, r := range cells {
				if r == 0 {
					cells[i] = ' '
				}
			}
			switch {
			case c == "":
				sb.WriteString(string(cells))
			case bg != "":
				sb.WriteString(clkBG(bg) + paint(c, string(cells)))
			default:
				sb.WriteString(paint(c, string(cells)))
			}
			x = run
		}
		out[y] = sb.String()
	}
	return out
}

// shimmer passes a band of light over his body, low left to high right;
// f of n is how far across it is.
func (g *clkGrid) shimmer(f, n int, tint string, strength float64) {
	p := -3 + 19*float64(f)/float64(max(1, n-1))
	for y := range g.r {
		for x := range g.r[y] {
			if !g.body[y][x] {
				continue
			}
			d := math.Abs(float64(x) + float64(y)*0.7 - p)
			if d < 2 {
				k := strength * (1 - d/2)
				g.c[y][x] = clkMix(g.c[y][x], tint, k)
				if g.bg[y][x] != "" {
					g.bg[y][x] = clkMix(g.bg[y][x], tint, k)
				}
			}
		}
	}
}

// clkEye is how his eyes look: open (a hole in him), shut (filled in), or
// lit in a colour for a moment.
type clkEye struct {
	shut  bool
	winkL bool // only the left one shut
	lit   string
}

// sprite is the invader. Idle he blinks, and winks now and then; working he
// steps from one pose to the other every other second and a pale drop of
// paint drifts off him; needing you he holds his arms up and his ! glows
// and fades; asleep his eyes are shut and z's drift up.
func (g *clkGrid) sprite(s clkState) {
	md, tick, fx := s.md, s.tick, s.fx
	b := clkBody(md)
	pose := clkCrab
	var eye clkEye
	switch md {
	case moodIdle:
		switch {
		case tick%9 == 4:
			eye.shut = true
		case tick%37 == 20:
			eye.winkL = true
		}
	case moodWorking:
		if tick/2%2 == 1 {
			pose = clkArms
		}
		if tick%11 == 6 {
			eye.shut = true
		}
	case moodNeedsYou:
		b, pose = cYellow, clkArms
		c := clkMix(cYellow, cFaint, .55)
		if tick%2 == 0 {
			c = cYellow
		}
		g.put(0, clkX+clkBodyW+1, "!", c, false)
	case moodSleepy:
		eye.shut = true
		switch tick % 3 {
		case 0:
			g.put(1, clkX+clkBodyW+1, "z", cFaint, false)
		case 1:
			g.put(0, clkX+clkBodyW, "Z", cDim, false)
			g.put(1, clkX+clkBodyW+1, "z", cFaint, false)
		default:
			g.put(0, clkX+clkBodyW, "Z", cDim, false)
		}
	}
	b, eye = fx.dress(b, eye)
	for y, l := range pose {
		g.put(y, clkX, l, b, true)
	}
	for i, x := range clkEyes {
		x += clkX
		switch {
		case eye.shut || (eye.winkL && i == 0):
			g.r[clkFace][x] = '█'
		case eye.lit != "":
			g.r[clkFace][x], g.c[clkFace][x], g.bg[clkFace][x] = '▄', eye.lit, b
		}
	}
	g.on = true
	if fx.kind != fxNone {
		fx.draw(g)
		return
	}
	if md == moodWorking {
		g.drops(tick, []rune("·."), clkPaints(), 5)
	}
	if s.rich && tick%30 < 3 {
		g.dot(1+tick%30, clkW-1, []rune("$¢.")[tick%30], clkMix(cYellow, cFaint, float64(tick%30)*.3))
	}
}

// drops drift off him a second at a time: one falls a row a second down a
// side and fades. One in every `every` seconds starts one.
func (g *clkGrid) drops(tick int, look []rune, paints []string, every int) {
	for age := len(look) - 1; age >= 0; age-- {
		h := clkHash(tick - age)
		if h%every != 0 {
			continue
		}
		x := h / 7 % 2
		if h/3%2 == 0 {
			x = clkW - 1 - x
		}
		c := clkMix(paints[h/11%len(paints)], cFaint, .3+.4*float64(age))
		g.dot(2+age, x, look[age], c)
	}
}

// mark is the monogram, in his colour.
func (g *clkGrid) mark(md mood) {
	for y, l := range clkAG {
		g.put(y, clkX, l, clkBody(md), true)
	}
}

// crossfade turns him into the monogram (or back): he dims almost to
// nothing, and the other brightens up out of it; f of n frames in.
func (g *clkGrid) crossfade(s clkState, back bool, f, n int) {
	s.fx = clkFX{}
	half := n / 2
	showMark := f >= half
	if back {
		showMark = !showMark
	}
	if showMark {
		g.mark(s.md)
	} else {
		g.sprite(s)
	}
	t := float64(half-1-f) / float64(half-1) // 1 → 0 over the first half
	if f >= half {
		t = float64(f-half) / float64(n-half-1) // 0 → 1 over the second
	}
	for y := range g.r {
		for x := range g.r[y] {
			if g.r[y][x] != 0 && g.c[y][x] != "" {
				g.c[y][x] = clkMix(cFaint, g.c[y][x], .15+.85*t)
			}
			if g.bg[y][x] != "" {
				g.bg[y][x] = clkMix(cFaint, g.bg[y][x], .15+.85*t)
			}
		}
	}
}

func clkBody(md mood) string {
	if md == moodIdle || md == moodSleepy {
		return cDim
	}
	return cOrange
}

func clkPaints() []string { return []string{cOrange, cYellow, cBlue, cGreen} }

// clkBG is a palette colour as a background.
func clkBG(c string) string {
	return strings.Replace(strings.TrimSuffix(c, bold), "[38;", "[48;", 1)
}

// clkMix blends two of the palette's colours, t of the way from a to b;
// bold on either is dropped.
func clkMix(a, b string, t float64) string {
	t = min(1, max(0, t))
	var ar, ag, ab, br, bg, bb int
	_, errA := fmt.Sscanf(strings.TrimSuffix(a, bold), "\x1b[38;2;%d;%d;%dm", &ar, &ag, &ab)
	_, errB := fmt.Sscanf(strings.TrimSuffix(b, bold), "\x1b[38;2;%d;%d;%dm", &br, &bg, &bb)
	if errA != nil || errB != nil {
		if t < .5 {
			return a
		}
		return b
	}
	l := func(x, y int) int { return int(math.Round(float64(x) + (float64(y)-float64(x))*t)) }
	return rgb(l(ar, br), l(ag, bg), l(ab, bb))
}

// clkHash scatters n so drops and sparks land somewhere new each time but
// the same frame always draws the same.
func clkHash(n int) int {
	x := uint32(n) * 2654435761
	x ^= x >> 15
	x *= 2246822519
	x ^= x >> 13
	return int(x % 100003)
}

// fxKind is something clanker is reacting to, weakest first: a stronger
// one takes over a weaker one already running, never the other way.
type fxKind int

const (
	fxNone     fxKind = iota
	fxShimmer         // a light passes over him, now and then
	fxMorphIn         // he fades into the monogram
	fxMorphOut        // and back
	fxDone            // an agent finished: a glint and a drop
	fxAnswered        // one you kept waiting moved on: he greens, confetti
	fxAsk             // one started waiting on you: streaks fly off him
	fxError           // one hit an error, or agtop did: he reddens, sparks
)

// clkFX is a reaction and how far into it he is, in frames of fxEvery.
type clkFX struct {
	kind  fxKind
	frame int
}

const fxEvery = 70 * time.Millisecond

var fxLen = map[fxKind]int{
	fxShimmer: clkShine, fxMorphIn: 12 + clkShine, fxMorphOut: 12,
	fxDone: 14, fxAnswered: 22, fxAsk: 18, fxError: 16,
}

type fxTickMsg struct{}

func fxTick() tea.Cmd {
	return tea.Tick(fxEvery, func(time.Time) tea.Msg { return fxTickMsg{} })
}

// dress is how a reaction colours him: a tint that eases back, and his
// eyes lit for a moment in its colour.
func (fx clkFX) dress(b string, eye clkEye) (string, clkEye) {
	f := float64(fx.frame)
	switch fx.kind {
	case fxAsk:
		b = clkMix(cText, cYellow, f/6)
		if f < 10 {
			eye = clkEye{lit: cText}
		}
	case fxAnswered:
		b = clkMix(cGreen, b, (f-4)/14)
		if f < 12 {
			eye = clkEye{lit: clkMix(cGreen, cText, .4)}
		}
	case fxDone:
		if f < 6 {
			eye = clkEye{lit: clkMix(cText, b, f/6)}
		}
	case fxError:
		b = clkMix(cRed, b, (f-6)/10)
		if f < 10 {
			eye = clkEye{lit: clkMix(cRed, cText, .25)}
		}
	}
	return b, eye
}

// draw is what comes off him.
func (fx clkFX) draw(g *clkGrid) {
	f := fx.frame
	l, r := clkX-1, clkX+clkBodyW // the columns just off each side of him
	switch fx.kind {
	case fxShimmer:
		g.shimmer(f, clkShine, cText, .6)
	case fxAsk:
		// Streaks out from him, a tip with a short tail, fading as they
		// go; then a yellow light passes over him.
		type ray struct {
			y, x, dy, dx int
			r            rune
		}
		rays := []ray{
			{2, l, 0, -1, '─'}, {2, r, 0, 1, '─'},
			{1, l, -1, -1, '╲'}, {1, r, -1, 1, '╱'},
			{3, l, 1, -1, '╱'}, {3, r, 1, 1, '╲'},
		}
		if f < 8 {
			step := f/2 + 1
			c := clkMix(cYellow, cFaint, float64(f)/8)
			for _, ry := range rays {
				for s := max(0, step-2); s < step && s < 2; s++ {
					ch, cc := '·', clkMix(c, cFaint, .5)
					if s == step-1 {
						ch, cc = ry.r, c
					}
					g.dot(ry.y+ry.dy*s, ry.x+ry.dx*s, ch, cc)
				}
			}
		}
		if f >= 4 {
			g.shimmer(f-4, clkShine, cYellow, .8)
		}
	case fxAnswered:
		// Confetti drifting down past him, dimming as it falls.
		paints := append(clkPaints(), cText)
		for i := 0; i < 7; i++ {
			h := clkHash(1000 + i)
			y := f/2 - h%5
			x := []int{0, 1, clkW - 2, clkW - 1, clkX + 1, clkX + 5, clkX + 9}[i]
			if y < 0 || y >= clkH {
				continue
			}
			g.dot(y, x, []rune("·•°+")[h/9%4], clkMix(paints[h/13%len(paints)], cFaint, float64(y)*.25))
		}
		if f >= 6 {
			g.shimmer(f-6, clkShine, cGreen, .5)
		}
	case fxDone:
		// A glint across him, and a drop off each side.
		g.shimmer(f, fxLen[fxDone], cText, .5)
		for i, x := range []int{l, r} {
			y := (f+2*i)/4 + 1
			g.dot(y, x, '·', clkMix(cOrange, cFaint, float64(f)/14))
		}
	case fxError:
		// A few red sparks, then nothing.
		if f < 9 && f%3 == 0 {
			for i := 0; i < 2; i++ {
				h := clkHash(5000 + f*3 + i)
				g.dot(h%clkH, []int{0, 1, clkW - 2, clkW - 1}[h/5%4], []rune("×·")[h/7%2], clkMix(cRed, cFaint, float64(f)/10))
			}
		}
	}
}

// react starts a reaction, unless a stronger one is running.
func (m *Model) react(k fxKind) {
	if k < m.fx.kind {
		return
	}
	if k != fxMorphIn {
		m.clkMark = false
	}
	m.fx = clkFX{kind: k}
	if !m.fxOn {
		m.fxOn, m.fxKick = true, true
	}
}

// onFXTick moves the reaction on a frame, and stops ticking when it's over.
func (m *Model) onFXTick() tea.Cmd {
	m.fx.frame++
	if m.fx.frame >= fxLen[m.fx.kind] {
		if m.fx.kind == fxMorphIn {
			m.clkMark, m.clkMarkAt = true, m.tick
		}
		m.fx, m.fxOn = clkFX{}, false
		return nil
	}
	return fxTick()
}

// clkBeat is clanker's second: now and then a light passes over him, and
// every clkCycle seconds he turns into the monogram for a while.
func (m *Model) clkBeat(md mood) {
	if m.fx.kind != fxNone || m.zen {
		return
	}
	if md != moodIdle && md != moodWorking {
		m.clkMark = false
		return
	}
	switch {
	case m.clkMark && m.tick-m.clkMarkAt >= clkShown:
		m.react(fxMorphOut)
	case m.clkMark:
	case m.tick%clkCycle == 0:
		m.react(fxMorphIn)
	case md == moodWorking && m.tick%clkCycle%15 == 8, md == moodIdle && m.tick%clkCycle == 25:
		m.react(fxShimmer)
	}
}

func (m *Model) clkState(md mood, t tally) clkState {
	return clkState{md: md, tick: m.tick, fx: m.fx, mark: m.clkMark, rich: t.today >= 500}
}
