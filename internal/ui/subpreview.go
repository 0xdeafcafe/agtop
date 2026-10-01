package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/0xdeafcafe/rush/internal/convo"
)

// The list uses the pane's ordinary viewport; this sibling viewport owns
// only the preview. Neither is nested inside the other's scrolling content.
type subPreview struct {
	id, selected, hover        string
	scroll, total, rows, width int
	x, y, w, h                 int
	visible                    bool
}

func wideSubagents(c *hostConn, w int) bool {
	return c.subOpen == "" && w >= 150 && len(c.subs) > 0
}

func subListWidth(w int) int { return min(72, w*2/5) }

func (m *Model) subPreviewAt(c *hostConn, x, y int) bool {
	p := &c.subPreview
	return p.visible && m.viewName(c) == "subagents" && c.subOpen == "" &&
		!m.zen && !m.embedded && m.picker == nil && !m.cardModal(c) &&
		x >= p.x && x < p.x+p.w && y >= p.y && y < p.y+p.h
}

func (m *Model) subPreviewLines(c *hostConn, o convo.Options, h int) []string {
	p := &c.subPreview
	if p.selected != c.sel {
		p.selected, p.hover = c.sel, ""
	}
	id, picked := strings.CutPrefix(c.sel, "sub:")
	if !picked {
		id = c.subs[len(c.subs)-1].ID
		if run := c.runningSubs(); len(run) > 0 {
			id = run[0].ID
		}
	}
	if hover, ok := strings.CutPrefix(c.subHover, "sub:"); ok && time.Since(c.subHoverAt) >= subPeekAfter {
		p.hover = hover
	}
	// Keep a hovered preview when the pointer crosses into it to read/scroll.
	if p.hover != "" {
		id = p.hover
	}
	if p.id != id {
		p.id, p.scroll, p.total = id, 0, 0
	}
	o.Selected, o.Focused, o.HideActivity = "", false, true
	var detail []convo.Line
	if tail := c.subDetail(id); tail != nil {
		detail = tail.Sess.Render(o)
	}
	rows := max(0, h-2)
	if p.scroll > 0 && p.width == o.Width {
		// Streaming appends below the portion being read; follow only at the tail.
		p.scroll += len(detail) - p.total
	}
	p.total, p.rows, p.width = len(detail), rows, o.Width
	p.scroll = max(0, min(p.scroll, max(0, len(detail)-rows)))
	out := make([]string, h)
	if h == 0 {
		return out
	}
	name := id
	for _, sa := range c.subs {
		if sa.ID == id && sa.Type != "" {
			name = sa.Type
			break
		}
	}
	out[0] = dim(" " + name + " · scroll here · click to open")
	end := len(detail) - p.scroll
	start := max(0, end-rows)
	for i, l := range detail[start:end] {
		out[i+1] = l.Text
	}
	if h > 1 && len(detail) > rows {
		position := "latest"
		if p.scroll > 0 {
			position = fmt.Sprintf("↓ %d more", p.scroll)
		}
		out[h-1] = dim(fmt.Sprintf(" %d–%d / %d · %s", start+1, end, len(detail), position))
	}
	return out
}

// Compose after list clipping and selection, so the preview never becomes
// part of the list's row references, text selection, height or scroll range.
func (m *Model) drawSubPreview(c *hostConn, o convo.Options, out []string, top, h int) {
	lw := subListWidth(o.Width)
	o.Width -= lw + 3
	right := m.subPreviewLines(c, o, h)
	p := &c.subPreview
	p.x, p.y, p.w, p.h = m.paneX()+lw+3, m.paneTop+top, o.Width, h
	p.visible = true
	for i := range h {
		out[top+i] = fit(out[top+i], lw) + reset + " " + faint("│") + " " + fit(right[i], o.Width)
	}
}
