package gate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xdeafcafe/rush/internal/gate"
)

func TestShimsFollowTheRules(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	t.Setenv(RulesEnv, "tsc=1, go")
	WriteShims()
	b, err := os.ReadFile(filepath.Join(gate.BinDir(), "tsc"))
	if err != nil || !strings.Contains(string(b), " gate shim 'tsc' \"$@\"") {
		t.Fatalf("tsc shim %q, %v", b, err)
	}
	t.Setenv(RulesEnv, "go")
	WriteShims()
	if _, err := os.Stat(filepath.Join(gate.BinDir(), "tsc")); err == nil {
		t.Fatal("tsc's shim stayed after it left the rules")
	}
}

func TestOffHasNoRules(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	if r := Rules(); r != nil {
		t.Fatalf("rules while off: %v", r)
	}
}

// A test's own build runs the stand-ins it writes into a temp folder,
// but never the ones a real folder keeps for every agent.
func TestScratchBuildsKeepToScratch(t *testing.T) {
	if got := ExeFor(t.TempDir()); got != Exe() {
		t.Errorf("into a temp folder: %q, want this build %q", got, Exe())
	}
	home, _ := os.UserHomeDir()
	if got := ExeFor(filepath.Join(home, "Library", "Caches", "agtop", "shims")); got == Exe() || scratch(got) {
		t.Errorf("into a real folder: %q, a scratch build", got)
	}
}
