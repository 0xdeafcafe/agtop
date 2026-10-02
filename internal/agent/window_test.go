package agent

import (
	"fmt"
	"testing"
)

func TestFill(t *testing.T) {
	tok := func(n int64) string { return fmt.Sprintf("%dk", n/1000) }
	f := FillOf(520_000, 1_000_000, Compaction{})
	if f.Compacts() || f.Pct() != 52 || f.Of(tok) != "520k of 1000k" {
		t.Fatalf("no compaction: %+v %q", f, f.Of(tok))
	}
	f = FillOf(520_000, 1_000_000, Compaction{Window: 400_000, Headroom: 33_000})
	if !f.Compacts() || f.Pct() != 130 || f.At != 367_000 || f.Of(tok) != "520k of 400k · auto-compacts at 367k · model 1000k" {
		t.Fatalf("compaction: %+v %q", f, f.Of(tok))
	}
	// The model's own window caps it.
	if f := FillOf(100_000, 200_000, Compaction{Window: 400_000, Headroom: 33_000}); f.Compacts() || f.Window != 200_000 {
		t.Fatalf("capped: %+v", f)
	}
}
