package fleet

import (
	"testing"
	"time"
)

// A running agent's temp work is measured again after a minute, or after
// fifty times as long as the last walk took when that was long.
func TestTempDueBacksOff(t *testing.T) {
	now := time.Now()
	quick := &Agent{Key: "quick", PID: 1}
	slow := &Agent{Key: "slow", PID: 2}
	ts := &TempSizes{Sizes: map[string]TempSize{
		"quick": {At: now.Add(-2 * time.Minute), Took: 100 * time.Millisecond},
		"slow":  {At: now.Add(-2 * time.Minute), Took: 30 * time.Second},
	}}
	due := ts.Due([]*Agent{quick, slow}, now)
	if len(due) != 1 || due[0] != quick {
		t.Fatalf("only the quick one is due: %v", due)
	}
	if due := ts.Due([]*Agent{slow}, now.Add(24*time.Minute)); len(due) != 1 {
		t.Fatal("the slow one is due once 25 minutes have passed")
	}
}
