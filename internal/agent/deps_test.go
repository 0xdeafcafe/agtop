package agent

import (
	"os/exec"
	"strings"
	"testing"
)

// The core knows no agent: nothing under internal/agent may import an
// adapter, or the Claude Code packages the Claude adapter wraps.
func TestCoreImportsNoAgent(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "./...").Output()
	if err != nil {
		t.Skipf("go list: %v", err)
	}
	for _, dep := range strings.Fields(string(out)) {
		for _, banned := range []string{"/internal/adapters", "/internal/claude", "/internal/headless", "/internal/host", "/internal/daemon"} {
			if strings.Contains(dep, "agtop"+banned) {
				t.Errorf("internal/agent depends on %s", dep)
			}
		}
	}
}
