package antigravity

import (
	"errors"
	"github.com/0xdeafcafe/rush/internal/state"
	"testing"
	"time"
)

func TestRunnerSnapshotNeedsFreshSuccessfulDiscovery(t *testing.T) {
	t.Setenv("RUSH_HOME", t.TempDir())
	if _, ready := (Adapter{}).RunnerSnapshot(); ready {
		t.Fatal("unverified catalog marked ready")
	}
	writeRunnerSnapshot([]byte("gemini-test Available model"), true)
	models, ready := (Adapter{}).RunnerSnapshot()
	if !ready || len(models) != 1 || models[0].ID != "gemini-test" {
		t.Fatalf("snapshot: %v %v", models, ready)
	}
	writeRunnerSnapshot([]byte(errors.New("private provider error").Error()), false)
	if models, ready = (Adapter{}).RunnerSnapshot(); ready || len(models) != 0 {
		t.Fatal("failed refresh kept stale readiness")
	}
	if err := state.WriteJSON(runnerSnapshotPath(), runnerSnapshot{Ready: true, At: time.Now().Add(-25 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, ready = (Adapter{}).RunnerSnapshot(); ready {
		t.Fatal("expired snapshot accepted")
	}
}
