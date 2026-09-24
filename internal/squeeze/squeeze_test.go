package squeeze

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestFile(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS only")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "s.jsonl")
	var b strings.Builder
	for i := 0; b.Len() < 3<<20+12345; i++ {
		b.WriteString(`{"type":"assistant","message":{"content":[{"type":"text","text":"line `)
		b.WriteString(strings.Repeat("words ", i%40))
		b.WriteString(`"}]},"cwd":"/work"}` + "\n")
	}
	want := []byte(b.String())
	_ = os.WriteFile(p, want, 0o600)
	old := time.Now().Add(-72 * time.Hour)
	_ = os.Chtimes(p, old, old)
	before, _ := os.Stat(p)

	r := Transcripts([]string{dir}, 48*time.Hour)
	if r.Files != 1 || r.Failed != 0 || r.After >= r.Before/3 {
		t.Fatalf("result %+v", r)
	}
	after, _ := os.Stat(p)
	if !Compressed(after) || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) || after.Mode() != before.Mode() {
		t.Fatalf("after: compressed=%v size %d/%d mtime %v/%v mode %v", Compressed(after), after.Size(), before.Size(), after.ModTime(), before.ModTime(), after.Mode())
	}
	got, _ := os.ReadFile(p)
	if !bytes.Equal(got, want) {
		t.Fatal("reads back different")
	}
	if out, err := exec.Command("grep", "-c", "line words words", p).Output(); err != nil || strings.TrimSpace(string(out)) == "0" {
		t.Fatalf("grep: %s %v", out, err)
	}
	// Appending, as Claude Code does, works and keeps everything.
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("{\"new\":1}\n")
	f.Close()
	got, _ = os.ReadFile(p)
	if !bytes.Equal(got, append(want, "{\"new\":1}\n"...)) {
		t.Fatal("append lost data")
	}
	// A second pass leaves compressed and recent files alone.
	if r := Transcripts([]string{dir}, 48*time.Hour); r.Files != 0 {
		t.Fatalf("second pass %+v", r)
	}
	if n, _ := filepath.Glob(filepath.Join(dir, ".*.squeeze")); len(n) != 0 {
		t.Fatalf("left a temp file: %v", n)
	}
}
