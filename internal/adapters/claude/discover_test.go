package claude

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/agtop/internal/agent"
	"github.com/0xdeafcafe/agtop/internal/agent/event"
	"github.com/0xdeafcafe/agtop/internal/claude"
)

// A profile's running sessions and past conversations are found on disk,
// and a past one reads back as agtop's own events.
func TestDiscoverAndHistory(t *testing.T) {
	dir := t.TempDir()
	p := agent.Profile{Kind: Kind, Name: "claude", Dir: dir}
	sid := "0123456789abcdef-past"
	proj := filepath.Join(dir, "projects", "-r")
	must(t, os.MkdirAll(proj, 0o755))
	must(t, os.MkdirAll(filepath.Join(dir, "sessions"), 0o755))
	lines := []string{
		`{"type":"user","cwd":"/r","timestamp":"2026-09-29T10:00:00Z","uuid":"u1","message":{"role":"user","content":"fix the build"}}`,
		`{"type":"assistant","cwd":"/r","timestamp":"2026-09-29T10:00:05Z","uuid":"a1","message":{"id":"m1","role":"assistant","content":[{"type":"text","text":"Fixed."}]}}`,
		`{"type":"user","cwd":"/r","timestamp":"2026-09-29T11:00:00Z","uuid":"u2","message":{"role":"user","content":"later"}}`,
	}
	must(t, os.WriteFile(filepath.Join(proj, sid+".jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644))
	live := `{"pid":` + strconv.Itoa(os.Getpid()) + `,"sessionId":"fedcba9876543210-live","cwd":"/r","kind":"interactive","status":"busy"}`
	must(t, os.WriteFile(filepath.Join(dir, "sessions", "1.json"), []byte(live), 0o644))

	var a Adapter
	past := a.Past(p)
	if len(past) != 1 || past[0].ID != sid || past[0].Cwd != "/r" {
		t.Fatalf("past = %+v", past)
	}
	got := a.Live(p)
	if len(got) != 1 || got[0].ID != "fedcba9876543210-live" {
		t.Fatalf("live = %+v", got)
	}
	if ss, ok := got[0].Extra.(claude.Session); !ok || ss.Status != "busy" {
		t.Errorf("live session's own record = %#v", got[0].Extra)
	}

	evs, err := a.History(past[0], time.Date(2026, 9, 29, 10, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, ev := range evs {
		if m, ok := ev.(event.Message); ok {
			for _, part := range m.Parts {
				texts = append(texts, m.Role+":"+part.Text)
			}
		}
	}
	if strings.Join(texts, "|") != "user:fix the build|assistant:Fixed." {
		t.Errorf("history = %q", texts)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
