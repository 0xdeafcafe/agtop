package fleet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// However the platform walks a folder, it counts what statUsage does: every
// file's and folder's blocks, links not followed.
func TestDirUsageMatchesStat(t *testing.T) {
	dir := t.TempDir()
	for i, p := range []string{"a.txt", "b/c.bin", "b/d/e.txt", "b/d/f/g.txt", "h/i.txt", "long-" + strings.Repeat("n", 200)} {
		p = filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, (i+1)*9000), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 300 { // more than one call's worth of entries
		os.WriteFile(filepath.Join(dir, "h", "many-"+strings.Repeat("x", i%50)+string(rune('a'+i%26))+"-"+filepath.Base(t.Name())+string(rune('0'+i%10))+string(rune('A'+i/26))), []byte("x"), 0o644)
	}
	os.Symlink(filepath.Join(dir, "b"), filepath.Join(dir, "link"))
	os.Symlink("/", filepath.Join(dir, "root"))
	want, got := statUsage(dir), dirUsage(dir)
	if want == 0 || got != want {
		t.Fatalf("dirUsage %d, statUsage %d", got, want)
	}
}

func BenchmarkDirUsage(b *testing.B) {
	dir := os.Getenv("AGTOP_DU_DIR")
	if dir == "" {
		b.Skip("AGTOP_DU_DIR names a big folder to walk")
	}
	b.Run("bulk", func(b *testing.B) {
		for b.Loop() {
			dirUsage(dir)
		}
	})
	b.Run("stat", func(b *testing.B) {
		for b.Loop() {
			statUsage(dir)
		}
	})
}
