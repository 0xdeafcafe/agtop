package ui

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Charts for the Efficiency place: stacked bars in eighths of a cell, a
// braille area for small spaces, and a lane of markers under them.

var eighths = []string{" ", "▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}

// stackBars draws vals ([bar][part], parts bottom first) as h rows of bars
// barW cells wide with gap cells between. Each cell takes the colour of the
// part at its middle; the top cell of a bar is cut to the eighth.
func stackBars(vals [][]float64, colors []string, top float64, h, barW, gap int) []string {
	rows := make([]strings.Builder, h)
	for i, parts := range vals {
		total := 0.0
		for _, v := range parts {
			total += v
		}
		H := 0.0
		if top > 0 {
			H = total / top * float64(h*8)
		}
		for r := range h {
			lower := float64(r * 8)
			fill := int(math.Round(math.Min(8, math.Max(0, H-lower))))
			if total > 0 && r == 0 && fill == 0 {
				fill = 1 // something, however small, shows
			}
			cell := blanks(barW)
			if fill > 0 {
				mid := (lower + float64(fill)/2) / float64(h*8) * top
				c := colors[len(colors)-1]
				acc := 0.0
				for p, v := range parts {
					acc += v
					if mid <= acc {
						c = colors[p%len(colors)]
						break
					}
				}
				cell = paint(c, strings.Repeat(eighths[fill], barW))
			}
			row := &rows[h-1-r]
			row.WriteString(cell)
			if i < len(vals)-1 {
				row.WriteString(blanks(gap))
			}
		}
	}
	out := make([]string, h)
	for i := range rows {
		out[i] = rows[i].String()
	}
	return out
}

// braille draws vals as a filled area w cells wide and h rows high, two
// values to a cell, four dots high.
func braille(vals []float64, w, h int, c string) []string {
	cols := w * 2
	top := 0.0
	for _, v := range vals {
		top = math.Max(top, v)
	}
	// Resample to the columns: each column is the most of what it covers,
	// so a spike never disappears.
	at := make([]float64, cols)
	for x := range cols {
		lo := x * len(vals) / cols
		hi := max(lo+1, (x+1)*len(vals)/cols)
		for i := lo; i < hi && i < len(vals); i++ {
			at[x] = math.Max(at[x], vals[i])
		}
	}
	dots := func(x int) int {
		if top <= 0 || at[x] <= 0 {
			return 0
		}
		return max(1, int(math.Round(at[x]/top*float64(h*4))))
	}
	// Dot bits by column (left, right) and row from the top of the cell.
	bits := [2][4]rune{{0x01, 0x02, 0x04, 0x40}, {0x08, 0x10, 0x20, 0x80}}
	out := make([]string, h)
	for row := range h {
		var b strings.Builder
		for cx := range w {
			r := rune(0x2800)
			for side := range 2 {
				d := dots(cx*2 + side)
				for y := range 4 {
					fromBottom := (h-1-row)*4 + (3 - y)
					if fromBottom < d {
						r |= bits[side][y]
					}
				}
			}
			b.WriteRune(r)
		}
		out[row] = paint(c, b.String())
	}
	return out
}

// effMark is a marker under a chart: an event at a time.
type effMark struct {
	at    time.Time
	glyph string
	color string
}

// markLane places marks in a lane width cells wide: bucket says which bar
// a time falls in, at where that bar starts. Marks sharing a cell show as
// the first one's glyph with a + after it.
func markLane(marks []effMark, bucket func(time.Time) int, width int, at func(int) int) string {
	cells := make([]string, width)
	for _, mk := range marks {
		i := bucket(mk.at)
		if i < 0 {
			continue
		}
		x := at(i)
		if x < 0 || x >= width {
			continue
		}
		if cells[x] == "" {
			cells[x] = paint(mk.color, mk.glyph)
		} else if x+1 < width && cells[x+1] == "" {
			cells[x+1] = faint("+")
		}
	}
	var b strings.Builder
	for _, c := range cells {
		if c == "" {
			c = " "
		}
		b.WriteString(c)
	}
	return b.String()
}

// axisLabel is a chart's value in its unit, short.
func axisLabel(v float64, unit string) string {
	switch unit {
	case "$":
		return money(v)
	case "%":
		return fmt.Sprintf("%.0f%%", v)
	case "B":
		return bytesShort(int64(v))
	}
	return tokens(int64(v))
}

func bytesShort(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%dB", n)
}

// niceTop rounds a chart's top up to 1, 2 or 5 times a power of ten, so the
// axis reads cleanly.
func niceTop(v float64) float64 {
	if v <= 0 {
		return 1
	}
	p := math.Pow(10, math.Floor(math.Log10(v)))
	for _, m := range []float64{1, 1.5, 2, 2.5, 3, 4, 5, 6, 8, 10} {
		if v <= m*p {
			return m * p
		}
	}
	return 10 * p
}
