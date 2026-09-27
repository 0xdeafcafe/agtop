package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Drafts from before there were kinds become sent or cleared, and the
// file as it was is kept as drafts.json.bak.
func TestDraftsMigrate(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGTOP_HOME", dir)
	old := `[{"text":"sent it","at":"2026-01-01T00:00:00Z","sent":true},{"text":"wiped it","at":"2026-01-01T00:00:00Z"}]`
	path := filepath.Join(dir, "drafts.json")
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	ds := Drafts()
	if len(ds) != 2 || ds[0].Kind != KindSent || ds[1].Kind != KindCleared {
		t.Fatalf("migrated: %+v", ds)
	}
	if b, err := os.ReadFile(path + ".bak"); err != nil || string(b) != old {
		t.Fatalf("the old file should be kept as .bak: %q %v", b, err)
	}
	if DraftCount(KindCleared) != 1 || DraftCount(KindDraft) != 0 {
		t.Fatal("counts")
	}
}

// Each kind keeps a text once. Sending one takes it out of the drafts
// and the cleared; clearing a draft leaves it a draft.
func TestDraftKinds(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	add := func(text, kind string) {
		t.Helper()
		if err := AddDraft(Draft{Text: text, Kind: kind, At: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	add("a", KindDraft)
	add("a", KindCleared)
	if DraftCount(KindDraft) != 1 || DraftCount(KindCleared) != 0 {
		t.Fatalf("clearing a draft should leave it one: %+v", Drafts())
	}
	add("b", KindCleared)
	add("b", KindDraft)
	if DraftCount(KindDraft) != 2 || DraftCount(KindCleared) != 0 {
		t.Fatalf("keeping a cleared one as a draft moves it: %+v", Drafts())
	}
	add("a", KindSent)
	add("a", KindSent)
	if DraftCount(KindDraft) != 1 || DraftCount(KindSent) != 1 {
		t.Fatalf("sending a draft moves it to sent, once: %+v", Drafts())
	}
	if ds := Drafts(); !ds[0].Sent {
		t.Fatalf("sent drafts say so to older agtops: %+v", ds[0])
	}
	if err := RemoveDraft(KindSent, "a"); err != nil || DraftCount(KindSent) != 0 {
		t.Fatalf("remove: %v %+v", err, Drafts())
	}
}
