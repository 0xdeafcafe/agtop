package efficiency

import (
	"testing"

	_ "github.com/0xdeafcafe/agtop/internal/adapters/claude"
)

// Cache reads are priced at the agent's own prices, and at its default
// model's when the model isn't one it prices.
func TestCacheReadPrice(t *testing.T) {
	if got := cacheReadPrice("claude-opus-5-5"); got != 0.20 {
		t.Fatalf("Opus 5.5 reads at %v a million, not 0.20", got)
	}
	if got := cacheReadPrice(""); got != cacheReadPrice("claude-opus-5-5") {
		t.Fatalf("an unknown model reads at %v, not the default model's", got)
	}
	if got := cacheReadPrice("claude-haiku-4-5"); got <= 0 || got >= 0.20 {
		t.Fatalf("Haiku reads at %v", got)
	}
}
