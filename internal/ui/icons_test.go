package ui

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestIcons draws the menu bar app's icons from clanker's own sprite, so
// they stay him: AGTOP_ICONS=1 go test ./internal/ui -run TestIcons writes
// them to internal/menubar/icons.
func TestIcons(t *testing.T) {
	if os.Getenv("AGTOP_ICONS") == "" {
		t.Skip("set AGTOP_ICONS=1 to redraw the menu bar app's icons")
	}
	out := filepath.Join("..", "menubar", "icons")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	save := func(name string, img image.Image) {
		f, err := os.Create(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
	}

	// The menu bar: black on clear, which macOS tints to fit the bar. At
	// rest he's crab-footed, working he steps into his other pose, and
	// needing you he holds his arms up by a !.
	crab, arms := iconPixels(clkCrab), iconPixels(clkArms)
	for _, v := range []struct {
		name string
		px   [][]bool
	}{
		{"clanker-idle", crab},
		{"clanker-working", arms},
		{"clanker-needs", iconBang(arms)},
	} {
		for _, s := range []int{1, 2} {
			name := v.name + "Template.png"
			if s == 2 {
				name = v.name + "Template@2x.png"
			}
			save(name, iconTemplate(v.px, 2*s))
		}
	}

	// The app, and so every notification it posts: him in orange on a dark
	// tile, at every size an .icns holds. macOS 26 on draws its own tile
	// and puts any other in a grey one, so it gets him on a full square.
	for _, v := range []struct {
		name  string
		bleed bool
	}{{"AppIcon", false}, {"AppIcon-full", true}} {
		set := filepath.Join(t.TempDir(), v.name+".iconset")
		if err := os.MkdirAll(set, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, s := range []int{16, 32, 128, 256, 512} {
			for _, k := range []int{1, 2} {
				name := fmt.Sprintf("icon_%dx%d.png", s, s)
				if k == 2 {
					name = fmt.Sprintf("icon_%dx%d@2x.png", s, s)
				}
				f, err := os.Create(filepath.Join(set, name))
				if err != nil {
					t.Fatal(err)
				}
				png.Encode(f, iconApp(crab, s*k, v.bleed))
				f.Close()
			}
		}
		if b, err := exec.Command("/usr/bin/iconutil", "-c", "icns", "-o", filepath.Join(out, v.name+".icns"), set).CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, b)
		}
	}
	save("AppIcon.png", iconApp(crab, 1024, false))
}

// iconPixels undoes the half blocks: each row of the sprite is two rows of
// pixels.
func iconPixels(rows []string) [][]bool {
	px := make([][]bool, 2*len(rows))
	for y, l := range rows {
		for _, r := range l {
			px[2*y] = append(px[2*y], r == '▀' || r == '█')
			px[2*y+1] = append(px[2*y+1], r == '▄' || r == '█')
		}
	}
	return px
}

// iconBang puts a ! a pixel off his right side.
func iconBang(px [][]bool) [][]bool {
	out := make([][]bool, len(px))
	for y, row := range px {
		bang := y < len(px)-3 || y == len(px)-2
		out[y] = append(append(append([]bool{}, row...), false), bang)
	}
	return out
}

// iconTemplate draws px with n image pixels to each, on a canvas 16n/2
// tall so he sits in the menu bar at its icons' height.
func iconTemplate(px [][]bool, n int) image.Image {
	w, h := len(px[0])*n, len(px)*n
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y, row := range px {
		for x, on := range row {
			if !on {
				continue
			}
			for dy := 0; dy < n; dy++ {
				for dx := 0; dx < n; dx++ {
					img.SetNRGBA(x*n+dx, y*n+dy, color.NRGBA{0, 0, 0, 255})
				}
			}
		}
	}
	return img
}

// iconApp is the app icon at size s: a tile on Apple's grid (824 of 1024,
// a superellipse's corners), or with bleed the whole square, lit from the
// top, and him on it at a whole number of pixels to each of his so he
// stays crisp.
func iconApp(px [][]bool, s int, bleed bool) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, s, s))
	f := float64(s) / 1024
	half, c := 412*f, 512*f
	size := 2 * half * .62 // how wide he is
	if bleed {
		half = c
	}
	top, bottom := [3]float64{50, 47, 43}, [3]float64{26, 24, 22}
	orange := [3]float64{217, 119, 87}

	pw, ph := len(px[0]), len(px)
	n := max(1, int(math.Round(size/float64(pw))))
	ox := int(math.Round(c - float64(pw*n)/2))
	oy := int(math.Round(c - float64(ph*n)/2 + float64(n)*.25))
	on := func(x, y int) bool {
		gx, gy := x-ox, y-oy
		if gx < 0 || gy < 0 || gx >= pw*n || gy >= ph*n {
			return false
		}
		return px[gy/n][gx/n]
	}

	const ss = 4 // samples a side, for the tile's edge
	for y := 0; y < s; y++ {
		for x := 0; x < s; x++ {
			cover := 0.
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					u := math.Abs(float64(x)+(float64(sx)+.5)/ss-c) / half
					v := math.Abs(float64(y)+(float64(sy)+.5)/ss-c) / half
					if bleed || math.Pow(u, 5)+math.Pow(v, 5) <= 1 {
						cover++
					}
				}
			}
			if cover == 0 {
				continue
			}
			t := min(1, max(0, (float64(y)-(c-half))/(2*half)))
			var col [3]float64
			for i := range col {
				col[i] = top[i] + (bottom[i]-top[i])*t
			}
			if on(x, y) {
				// A little lighter up top, like the tile.
				for i := range col {
					col[i] = orange[i] * (1.08 - .16*t)
				}
			} else if on(x, y-max(1, n/6)) {
				for i := range col { // his shadow
					col[i] *= .7
				}
			}
			a := cover / (ss * ss)
			img.SetNRGBA(x, y, color.NRGBA{
				uint8(min(255, col[0])), uint8(min(255, col[1])), uint8(min(255, col[2])), uint8(math.Round(255 * a)),
			})
		}
	}
	return img
}
