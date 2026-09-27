package host

import "testing"

func TestTurnCostTakesTheRiseInARunningTotal(t *testing.T) {
	var spent, sum float64
	// Claude Code's results carry the process's total so far.
	for _, total := range []float64{1, 3, 6} {
		sum += TurnCost(&spent, total)
	}
	// A restarted process counts from zero again.
	sum += TurnCost(&spent, 2)
	if sum != 8 {
		t.Fatalf("summed %v, want 8 (1+2+3, then 2)", sum)
	}
}
