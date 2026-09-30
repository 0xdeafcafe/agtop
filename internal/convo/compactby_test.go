package convo

import (
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/adapters/claude/headless"
	"github.com/0xdeafcafe/rush/internal/host"
)

func TestPlainText(t *testing.T) {
	s := New()
	now := time.Now()
	s.Apply(host.Sent{Text: "fix the upload"}, now)
	s.Apply(headless.Message{Role: "assistant", Blocks: []headless.Block{{Type: "text", Text: "Looking at upload.go"}}}, now)
	got := s.PlainText()
	for _, w := range []string{"## User\nfix the upload", "## Assistant\nLooking at upload.go"} {
		if !strings.Contains(got, w) {
			t.Fatalf("no %q in\n%s", w, got)
		}
	}
}
