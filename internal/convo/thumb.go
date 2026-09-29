package convo

import (
	"bytes"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/0xdeafcafe/agtop/internal/agent/event"
	"github.com/0xdeafcafe/agtop/internal/theme"
)

// An image a step read or a tool gave back is drawn under it, small, in
// half blocks: each cell's ▀ takes the upper pixel as its colour and the
// lower as its ground. Colours are 24-bit, as the rest of the palette is;
// the renderer brings them down for a terminal with fewer. A thumbnail is
// made off the UI goroutine: the first frame to want one starts it and
// draws the image's chip, and a frame after it's made has it.

// thumbW and thumbH are a thumbnail's most pixels: 32 cells by 12 rows.
const thumbW, thumbH = 32, 24

// thumbKey is a step's image, by its call and its place among them.
type thumbKey struct {
	id string
	n  int
}

type thumbnail struct {
	done bool
	rows []string // nil when it couldn't be read
}

var (
	thumbs = struct {
		sync.Mutex
		m map[thumbKey]*thumbnail
	}{m: map[thumbKey]*thumbnail{}}
	// Only a couple at once: a transcript full of screenshots asks for
	// them all when it's opened with everything shown.
	thumbSlots = make(chan struct{}, 2)
)

// thumbOf is image img of a step drawn as rows of half blocks, and whether
// they're ready. It never decodes while a frame is drawn.
func thumbOf(k thumbKey, img *event.ImageData) ([]string, bool) {
	thumbs.Lock()
	defer thumbs.Unlock()
	t := thumbs.m[k]
	if t == nil {
		if len(thumbs.m) >= 1024 {
			thumbs.m = map[thumbKey]*thumbnail{} // made again as they're drawn
		}
		t = &thumbnail{}
		thumbs.m[k] = t
		go func() {
			thumbSlots <- struct{}{}
			rows := halfBlocks(readThumb(img))
			<-thumbSlots
			thumbs.Lock()
			t.rows, t.done = rows, true
			thumbs.Unlock()
			lookupsGen.Add(1)
		}()
	}
	return t.rows, t.done && t.rows != nil
}

// thumbable is whether a file can be drawn as a thumbnail, by its name.
func thumbable(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif":
		return true
	}
	return false
}

// readThumb decodes an image, from its bytes or else its file, and
// shrinks it to fit a thumbnail; nil when it can't.
func readThumb(img *event.ImageData) *image.NRGBA {
	var r io.ReadSeeker
	switch {
	case len(img.Data) > 0:
		r = bytes.NewReader(img.Data)
	case img.Path != "":
		f, err := os.Open(img.Path)
		if err != nil {
			return nil
		}
		defer f.Close()
		r = f
	default:
		return nil
	}
	// Nothing huge is decoded to draw 768 pixels of it.
	cfg, _, err := image.DecodeConfig(r)
	if err != nil || cfg.Width*cfg.Height > 64<<20 {
		return nil
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil
	}
	src, _, err := image.Decode(r)
	if err != nil {
		return nil
	}
	return shrink(src)
}

// shrink scales src to fit thumbW by thumbH, keeping its shape, each pixel
// the average of those it covers.
func shrink(src image.Image) *image.NRGBA {
	b := src.Bounds()
	W, H := b.Dx(), b.Dy()
	if W <= 0 || H <= 0 {
		return nil
	}
	w := min(W, thumbW)
	h := max(1, (H*w+W/2)/W)
	if h > thumbH {
		h = thumbH
		w = max(1, (W*h+H/2)/H)
	}
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		y0, y1 := b.Min.Y+y*H/h, b.Min.Y+(y+1)*H/h
		for x := range w {
			x0, x1 := b.Min.X+x*W/w, b.Min.X+(x+1)*W/w
			var r, g, bl, a, n uint64
			for sy := y0; sy < max(y1, y0+1); sy++ {
				for sx := x0; sx < max(x1, x0+1); sx++ {
					cr, cg, cb, ca := src.At(sx, sy).RGBA()
					r, g, bl, a, n = r+uint64(cr), g+uint64(cg), bl+uint64(cb), a+uint64(ca), n+1
				}
			}
			if a == 0 {
				continue
			}
			// The sums are premultiplied: dividing by alpha's undoes it.
			out.SetNRGBA(x, y, color.NRGBA{R: uint8(r * 255 / a), G: uint8(g * 255 / a), B: uint8(bl * 255 / a), A: uint8(a / n / 257)})
		}
	}
	return out
}

// halfBlocks draws px two rows of pixels to a line. A pixel less than
// half opaque shows the terminal's ground.
func halfBlocks(px *image.NRGBA) []string {
	if px == nil {
		return nil
	}
	b := px.Bounds()
	rgb := func(c color.NRGBA) theme.RGB { return theme.RGB{R: c.R, G: c.G, B: c.B} }
	var rows []string
	for y := b.Min.Y; y < b.Max.Y; y += 2 {
		var s strings.Builder
		for x := b.Min.X; x < b.Max.X; x++ {
			top, bot := px.NRGBAAt(x, y), color.NRGBA{}
			if y+1 < b.Max.Y {
				bot = px.NRGBAAt(x, y+1)
			}
			switch up, down := top.A >= 128, bot.A >= 128; {
			case up && down:
				s.WriteString(rgb(top).FG() + rgb(bot).BG() + "▀")
			case up:
				s.WriteString(rgb(top).FG() + "\x1b[49m▀")
			case down:
				s.WriteString(rgb(bot).FG() + "\x1b[49m▄")
			default:
				s.WriteString("\x1b[49m ")
			}
		}
		s.WriteString(reset)
		rows = append(rows, s.String())
	}
	return rows
}
