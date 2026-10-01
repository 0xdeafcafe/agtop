// Package termimg draws pictures in the terminal at the best it can: Kitty
// graphics through Unicode placeholders where the terminal speaks them
// (kitty, Ghostty), else rush's own blocks. A placeholder is a character
// like any other, so a line holding one can be cached, scrolled and drawn
// again by a cell renderer and the picture follows it.
package termimg

import (
	"bytes"
	"hash/fnv"
	"image"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"unicode"

	"github.com/charmbracelet/x/ansi/kitty"
)

// Protocol is how a terminal takes pictures.
type Protocol int

const (
	Blocks Protocol = iota // half and quarter blocks, coloured: any terminal
	Kitty                  // Kitty graphics with Unicode placeholders
	ITerm2                 // iTerm2 inline images: detected, drawn as Blocks
	Sixel                  // sixel: detected, drawn as Blocks
)

func (p Protocol) String() string {
	return [...]string{"blocks", "kitty", "iterm2", "sixel"}[p]
}

// Detect names the terminal's picture protocol from its environment.
// RUSH_GRAPHICS (kitty, iterm2, sixel, blocks) overrides it. Inside tmux
// it's Blocks: tmux passes none of them through on its own.
func Detect(env func(string) string) Protocol {
	switch strings.ToLower(env("RUSH_GRAPHICS")) {
	case "kitty":
		return Kitty
	case "iterm2":
		return ITerm2
	case "sixel":
		return Sixel
	case "blocks":
		return Blocks
	}
	term, prog := env("TERM"), env("TERM_PROGRAM")
	switch {
	case env("TMUX") != "" || strings.HasPrefix(term, "screen") || strings.HasPrefix(term, "tmux"):
		return Blocks
	case prog == "ghostty" || term == "xterm-ghostty" || term == "xterm-kitty" || env("KITTY_WINDOW_ID") != "":
		return Kitty
	case prog == "iTerm.app" || env("LC_TERMINAL") == "iTerm2" || prog == "WezTerm":
		return ITerm2 // WezTerm has Kitty graphics but no placeholders
	case strings.Contains(term, "sixel") || term == "foot" || term == "foot-extra" || term == "mlterm" || prog == "mintty":
		return Sixel
	}
	return Blocks
}

// Current is this terminal's protocol.
var Current = Detect(os.Getenv)

// Drawn is whether pictures go out as Kitty graphics here. iTerm2 and
// sixel draw at the cursor, outside the cells, so a cell renderer can't
// keep them in place: they're Blocks until rush draws outside its frame.
func Drawn() bool { return Current == Kitty && !blocks.Load() }

// blocks is set to draw pictures in blocks even where Kitty graphics
// would show them sharp: some like them pixelated.
var blocks atomic.Bool

// SetBlocks sets whether pictures are drawn in blocks whatever the terminal.
func SetBlocks(on bool) { blocks.Store(on) }

var cellW, cellH atomic.Int64

// SetCell records a cell's size in pixels, as CSI 16t answers it.
func SetCell(w, h int) {
	if w > 0 && h > 0 {
		cellW.Store(int64(w))
		cellH.Store(int64(h))
	}
}

// Cell is a cell's size in pixels: as the terminal said, else a guess with
// a usual cell's shape, twice as tall as it's wide.
func Cell() (w, h int) {
	if w, h := cellW.Load(), cellH.Load(); w > 0 && h > 0 {
		return int(w), int(h)
	}
	return 10, 20
}

// Measured is whether the terminal has said its cell size.
func Measured() bool { return cellW.Load() > 0 }

// QueryCell asks the terminal for its cell size (CSI 16t), and its window
// in pixels (CSI 14t) for one that answers only that.
const QueryCell = "\x1b[16t\x1b[14t"

// Fit is how many cells a picture pw by ph pixels takes to fit maxCols by
// maxRows with its shape kept, cells cw by ch pixels: never more cells
// than its own pixels fill, so a small one isn't blown up.
func Fit(pw, ph, maxCols, maxRows, cw, ch int) (cols, rows int) {
	if pw <= 0 || ph <= 0 || maxCols <= 0 || maxRows <= 0 || cw <= 0 || ch <= 0 {
		return 0, 0
	}
	maxCols, maxRows = min(maxCols, 297), min(maxRows, 297) // kitty's diacritics number 297
	// Each in pixels: the most room, and the picture scaled into it.
	w, h := min(pw, maxCols*cw), min(ph, maxRows*ch)
	if w*ph > h*pw {
		w = h * pw / ph
	} else {
		h = w * ph / pw
	}
	return max(1, min(maxCols, (w+cw/2)/cw)), max(1, min(maxRows, (h+ch/2)/ch))
}

// ID is a picture's Kitty image id, from its bytes and size: the same
// picture is the same id every time, so a line drawn with it once stays
// right. 24 bits, the most a colour holds; never 0.
func ID(data []byte, cols, rows int) uint32 {
	h := fnv.New32a()
	h.Write(data)
	h.Write([]byte(strconv.Itoa(cols) + "x" + strconv.Itoa(rows)))
	return max(1, h.Sum32()&0xffffff)
}

// Placeholders are cols by rows cells showing Kitty image id: each cell the
// placeholder with its row and column as diacritics, the id its colour.
func Placeholders(id uint32, cols, rows int) []string {
	fg := "\x1b[38;2;" + strconv.Itoa(int(id>>16&0xff)) + ";" + strconv.Itoa(int(id>>8&0xff)) + ";" + strconv.Itoa(int(id&0xff)) + "m"
	out := make([]string, rows)
	for r := range rows {
		var b strings.Builder
		b.WriteString(fg)
		for c := range cols {
			b.WriteRune(kitty.Placeholder)
			b.WriteRune(kitty.Diacritic(r))
			b.WriteRune(kitty.Diacritic(c))
		}
		b.WriteString("\x1b[39m")
		out[r] = b.String()
	}
	return out
}

// Blank is s with each placeholder cell a space: for a screen faded behind
// a box, whose colours, and so its pictures' ids, are gone.
func Blank(s string) string {
	if !strings.ContainsRune(s, kitty.Placeholder) {
		return s
	}
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == kitty.Placeholder:
			b.WriteByte(' ')
			in = true
		case in && unicode.Is(unicode.Mn, r):
		default:
			in = false
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Transmit is the escape sequence that sends img to the terminal as image
// id, placed for placeholders cols by rows cells: PNG, quiet, in chunks.
// Transmission is named: x/ansi sends nothing when it's left to default.
func Transmit(id uint32, img image.Image, cols, rows int) (string, error) {
	var b bytes.Buffer
	err := kitty.EncodeGraphics(&b, img, &kitty.Options{
		Action: kitty.TransmitAndPut, Transmission: kitty.Direct, Format: kitty.PNG, ID: int(id), Quiet: 2,
		VirtualPlacement: true, DoNotMoveCursor: true, Columns: cols, Rows: rows, Chunk: true,
	})
	return b.String(), err
}
