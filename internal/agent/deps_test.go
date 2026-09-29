package agent

import (
	"os/exec"
	"strings"
	"testing"
)

// The core knows no agent: nothing under internal/agent may import an
// adapter, or the host.
func TestCoreImportsNoAgent(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "./...").Output()
	if err != nil {
		t.Skipf("go list: %v", err)
	}
	for _, dep := range strings.Fields(string(out)) {
		for _, banned := range []string{"/internal/adapters", "/internal/host"} {
			if strings.Contains(dep, "rush"+banned) {
				t.Errorf("internal/agent depends on %s", dep)
			}
		}
	}
}
