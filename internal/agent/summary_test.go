package agent

import (
	"strings"
	"testing"
)

func TestFitTokens(t *testing.T) {
	if FitTokens("short", 100) != "short" {
		t.Fatal("cut what fits")
	}
	long := "START" + strings.Repeat("x", 10_000) + "END"
	got := FitTokens(long, 1000)
	if len(got) > 3200 || !strings.HasPrefix(got, "START") || !strings.HasSuffix(got, "END") || !strings.Contains(got, "left out") {
		t.Fatalf("fit to %d: %q…", len(got), got[:40])
	}
}
