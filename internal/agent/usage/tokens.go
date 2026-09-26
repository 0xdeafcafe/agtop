// Package usage is what agents spend: tokens, and the quota windows an
// account has left. It knows no provider; adapters fill it in.
package usage

// TokenUsage counts tokens by kind. Providers without a prompt cache leave
// the cache fields zero.
type TokenUsage struct {
	Input, Output, CacheRead, CacheWrite5m, CacheWrite1h int64
	// Reasoning is the part of Output spent thinking, when the provider
	// says. It is counted in Output, not beside it.
	Reasoning int64
}

// Add adds o to u.
func (u *TokenUsage) Add(o TokenUsage) {
	u.Input += o.Input
	u.Output += o.Output
	u.CacheRead += o.CacheRead
	u.CacheWrite5m += o.CacheWrite5m
	u.CacheWrite1h += o.CacheWrite1h
	u.Reasoning += o.Reasoning
}

// Total is every token counted.
func (u TokenUsage) Total() int64 {
	return u.Input + u.Output + u.CacheRead + u.CacheWrite5m + u.CacheWrite1h
}
