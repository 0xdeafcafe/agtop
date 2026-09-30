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
