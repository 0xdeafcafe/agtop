package ui

import (
	"strings"

	"github.com/0xdeafcafe/agtop/internal/cellw"
)

type mood int

const (
	moodIdle mood = iota
	moodWorking
	moodNeedsYou
	moodSpendy
	moodSleepy
)

// clanker draws the header robot: antenna, lid, face with ears and arms, legs.
// Slots right of the lid carry the mood's extras (sweat, z's) at fixed width.
func clanker(md mood, tick int) []string {
	b := clkBody(md)
	face := paint(cText, clkEyes(md, tick))
	tip, l, r := dim("○"), "╶", "╴"
	sky, cheek := "   ", " "
	switch md {
	case moodWorking:
		tip = paint(cOrange+bold, "●")
		if tick%2 == 1 {
			tip = paint(cOrange, "○")
		}
	case moodNeedsYou:
		tip = paint(cYellow+bold, "!")
		if tick%2 == 1 {
			tip = paint(cYellow, "!")
		}
		l, r = "╲", "╱"
		face = paint(cYellow+bold, clkEyes(md, tick))
		cheek = paint(cBlue, "'")
	case moodSpendy:
		tip = paint(cYellow+bold, "$")
		face = paint(cYellow+bold, clkEyes(md, tick))
	case moodSleepy:
		tip = faint("○")
		switch tick % 3 {
		case 0:
			cheek = faint("z")
		case 1:
			sky, cheek = dim("  Z"), faint("z")
		default:
			sky = dim("  Z")
		}
	}
	return clkPad([]string{
		"    " + tip + " " + sky,
		" " + paint(b, "╭──┴──╮") + cheek,
		paint(b, l+"┤ ") + face + paint(b, " ├"+r),
		" " + paint(b, "╰┬───┬╯"),
	})
}

func clkPad(lines []string) []string {
	w := 0
	for _, l := range lines {
		w = max(w, cellw.String(l))
	}
	for i, l := range lines {
		lines[i] = l + strings.Repeat(" ", w-cellw.String(l))
	}
	return lines
}

func clkBody(md mood) string {
	if md == moodIdle || md == moodSleepy {
		return cDim
	}
	return cOrange
}

func clkEyes(md mood, tick int) string {
	switch md {
	case moodWorking:
		if tick%7 == 6 {
			return "-_-"
		}
		return "◉_◉"
	case moodNeedsYou:
		return "°□°"
	case moodSpendy:
		return "$_$"
	case moodSleepy:
		return "-_-"
	}
	return "•_•"
}
