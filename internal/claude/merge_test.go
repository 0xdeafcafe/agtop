package claude

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// An older folder's past sessions are copied into ~/.claude, keeping their
// age; a transcript ~/.claude has already is left as it is.
func TestMergeHistory(t *testing.T) {
	dir := t.TempDir()
	from, to := Account{ConfigDir: filepath.Join(dir, "old")}, Account{ConfigDir: filepath.Join(dir, "home")}
	write := func(a Account, rel, body string) {
		t.Helper()
		p := filepath.Join(a.ConfigDir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(from, "projects/-work/s1.jsonl", "old s1")
	write(from, "projects/-work/s2.jsonl", "old s2")
	write(from, "file-history/s1/abc@v1", "v1")
	write(from, "settings.json", "{}")
	write(to, "projects/-work/s2.jsonl", "home s2")
	then := time.Now().Add(-72 * time.Hour).Truncate(time.Second)
	_ = os.Chtimes(filepath.Join(from.ConfigDir, "projects/-work/s1.jsonl"), then, then)

	if err := MergeHistory(from, to); err != nil {
		t.Fatal(err)
	}
	read := func(rel string) string {
		b, _ := os.ReadFile(filepath.Join(to.ConfigDir, rel))
		return string(b)
	}
	if read("projects/-work/s1.jsonl") != "old s1" || read("file-history/s1/abc@v1") != "v1" {
		t.Fatal("the older folder's past sessions weren't copied")
	}
	if read("projects/-work/s2.jsonl") != "home s2" {
		t.Fatal("a transcript ~/.claude had was replaced")
	}
	if read("settings.json") != "" {
		t.Fatal("more than past sessions was copied")
	}
	if st, _ := os.Stat(filepath.Join(to.ConfigDir, "projects/-work/s1.jsonl")); !st.ModTime().Equal(then) {
		t.Fatalf("copied transcript's age = %v, want %v", st.ModTime(), then)
	}
	if err := MergeHistory(Account{ConfigDir: filepath.Join(dir, "missing")}, to); err != nil {
		t.Fatalf("a folder with no history: %v", err)
	}
}
