package state

import "testing"

// A split provider's key in its own harness keeps its own defaults,
// starting from the plan's; every other route keeps them by its agent.
func TestStartOn(t *testing.T) {
	var d Dispatch
	d.SetStartOn("ps", "ps", Start{Model: "big"})
	if d.StartOn("ps-key", "ps").Model != "big" {
		t.Fatal("the key starts from the plan's defaults")
	}
	d.SetStartOn("ps-key", "ps", Start{Model: "small"})
	if d.StartOn("ps-key", "ps").Model != "small" || d.StartOn("ps", "ps").Model != "big" {
		t.Fatalf("plan and key share defaults: %+v", d.Starts)
	}
	d.SetStartOn("ps-key", "ps-pb", Start{Effort: "high"})
	if d.StartFor("ps-pb").Effort != "high" {
		t.Fatalf("a rider's defaults are its agent's: %+v", d.Starts)
	}
}
