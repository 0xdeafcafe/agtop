package convo

import (
	"strings"
	"testing"

	"github.com/0xdeafcafe/rush/internal/host"
)

const autoModeDenial = "Permission for this action was denied by the Claude Code auto mode classifier. Reason: [Credential Exploration]. If you have other tasks that don't depend on this action, continue working on those. IMPORTANT: You *may* attempt to accomplish this action using other tools."

func TestClassified(t *testing.T) {
	for _, c := range []struct{ out, want string }{
		{autoModeDenial, "Credential Exploration"},
		{"Permission for this action was denied by the Claude Code auto mode classifier. Reason: Stage 2 classifier error - blocking based on stage 1 assessment (usually transient — retrying often succeeds). If you have other tasks", "Stage 2 classifier error - blocking based on stage 1 assessment (usually transient — retrying often succeeds)"},
		{"Permission for this action was denied by the Claude Code auto mode classifier. Reason: Blocked by classifier. IMPORTANT: x", "Blocked by classifier"},
	} {
		if got, ok := classified(&Step{Status: Denied, Output: c.out}); !ok || got != c.want {
			t.Errorf("classified(%.40q) = %q, %v; want %q", c.out, got, ok, c.want)
		}
	}
	if _, ok := classified(&Step{Status: Denied, Output: "The user doesn't want to proceed"}); ok {
		t.Error("a user's refusal isn't the classifier's")
	}
}

func TestAutoModeDenialCard(t *testing.T) {
	s := New()
	s.Apply(host.Sent{Text: "find the account"}, at(0))
	s.Apply(toolUse("b1", "Bash", map[string]any{"command": "strings $f | grep token"}), at(1))
	s.Apply(toolResult("b1", autoModeDenial, true, nil), at(2))
	s.Apply(toolUse("r1", "Read", map[string]any{"file_path": "/work/go.mod"}), at(2))
	s.Apply(toolResult("r1", "module x", false, nil), at(2))
	out := plain(s.Render(Options{Width: 110, Now: at(3)}))
	if !strings.Contains(out, "⊘ auto mode blocked this · Credential Exploration") || strings.Contains(out, "IMPORTANT") || strings.Contains(out, "▸") {
		t.Fatalf("denial card:\n%s", out)
	}
	if st := s.byID["b1"]; st.Status != Denied {
		t.Errorf("status %v, want Denied", st.Status)
	}
}
