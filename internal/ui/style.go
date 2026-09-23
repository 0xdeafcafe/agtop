package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

const (
	reset = "\x1b[0m"
	bold  = "\x1b[1m"
)

func rgb(r, g, b int) string { return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b) }

var (
	cOrange = rgb(217, 119, 87)
	cDim    = rgb(139, 133, 123)
	cFaint  = rgb(90, 85, 78)
	cGreen  = rgb(127, 191, 138)
	cYellow = rgb(224, 178, 92)
	cRed    = rgb(224, 104, 92)
	cBlue   = rgb(143, 179, 217)
	cWhite  = rgb(240, 236, 228)
)

func paint(c, s string) string {
	if s == "" {
		return ""
	}
	return c + s + reset
}

func dim(s string) string   { return paint(cDim, s) }
func faint(s string) string { return paint(cFaint, s) }

// fit pads or truncates to exactly w cells.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	sw := ansi.StringWidth(s)
	if sw > w {
		return ansi.Truncate(s, w, "…")
	}
	return s + strings.Repeat(" ", w-sw)
}

// right aligns to exactly w cells.
func right(s string, w int) string {
	sw := ansi.StringWidth(s)
	if sw >= w {
		return ansi.Truncate(s, w, "…")
	}
	return strings.Repeat(" ", w-sw) + s
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	return strings.Join(strings.Fields(s), " ")
}

// age matches the native view: 3s, 12m, 4h, 2d.
func age(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func dur(d time.Duration) string {
	switch {
	case d <= 0:
		return "–"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

func mem(b uint64) string {
	switch {
	case b == 0:
		return "–"
	case b >= 1<<30:
		return fmt.Sprintf("%.1fG", float64(b)/(1<<30))
	default:
		return fmt.Sprintf("%dM", b>>20)
	}
}

func money(c float64) string {
	switch {
	case c <= 0:
		return "–"
	case c < 10:
		return fmt.Sprintf("$%.2f", c)
	case c < 1000:
		return fmt.Sprintf("$%.1f", c)
	default:
		return fmt.Sprintf("$%.0f", c)
	}
}

func tokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1000:
		return fmt.Sprintf("%dk", n/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func cpuColor(p float64, s string) string {
	switch {
	case p >= 50:
		return paint(cYellow, s)
	case p >= 1:
		return paint(cGreen, s)
	default:
		return s
	}
}

func memColor(b uint64, s string) string {
	if b >= 1<<30 {
		return paint(cYellow, s)
	}
	return s
}

// bar draws a 10-cell usage meter.
func bar(pct float64) string {
	n := int(pct/10 + 0.5)
	if n > 10 {
		n = 10
	}
	if n < 0 {
		n = 0
	}
	c := cGreen
	switch {
	case pct >= 80:
		c = cRed
	case pct >= 50:
		c = cYellow
	}
	return paint(c, strings.Repeat("▰", n)) + faint(strings.Repeat("▱", 10-n))
}

var spinner = []string{"·", "✢", "✳", "✶", "✻", "✽", "✻", "✶", "✳", "✢"}

func wrap(s string, w int) []string {
	if w < 8 {
		w = 8
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		para = strings.TrimRight(para, " ")
		if para == "" {
			out = append(out, "")
			continue
		}
		out = append(out, strings.Split(ansi.Wrap(para, w, " -/"), "\n")...)
	}
	return out
}
