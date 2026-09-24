package host

import (
	"image"
	"image/png"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestReadImageWebP(t *testing.T) {
	p := filepath.Join(t.TempDir(), "shot.png")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 400, 300))
	r := rand.New(rand.NewPCG(1, 2))
	for i := range img.Pix {
		img.Pix[i] = byte(r.Uint32()) // noise: PNG can't squeeze it
	}
	png.Encode(f, img)
	f.Close()
	im, err := readImage(p)
	if err != nil {
		t.Fatal(err)
	}
	want := "image/png"
	if _, err := exec.LookPath("cwebp"); err == nil {
		want = "image/webp"
	}
	if im.MediaType != want {
		t.Fatalf("media type %s, want %s", im.MediaType, want)
	}
}
