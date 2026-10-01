package gemini_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/adapters/gemini"
	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/convo"
)

func TestSavedGeminiTurnsRenderSeparatelyAndFinish(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.json")
	text := `{"sessionId":"s","messages":[{"id":"u1","type":"user","content":"first"},{"id":"a1","type":"gemini","content":"answer one"},{"id":"u2","type":"user","content":"second"},{"id":"a2","type":"gemini","content":"answer two"}]}`
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	events, err := (gemini.Adapter{}).History(agent.Session{Transcript: path}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	session := convo.New()
	for _, ev := range events {
		session.Apply(ev, time.Now())
	}
	if len(session.Turns) != 2 || session.Turns[0].Prompt != "first" || session.Turns[1].Prompt != "second" {
		t.Fatalf("turns: %+v", session.Turns)
	}
	if session.Live() != nil {
		t.Fatal("saved Gemini history is still working")
	}
}
