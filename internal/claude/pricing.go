package claude

import "strings"

// Price is dollars per million tokens. CacheRead of zero means 0.1x input.
type Price struct {
	Input, Output, CacheRead float64
}

// List prices, first-party API. Longest prefix wins.
var prices = []struct {
	prefix string
	p      Price
}{
	{"claude-fable-5-1", Price{10, 50, 0.25}},
	{"claude-mythos-5-1", Price{10, 50, 0.25}},
	{"claude-fable-5", Price{10, 50, 0}},
	{"claude-mythos-5", Price{10, 50, 0}},
	{"claude-opus-5-5", Price{4, 20, 0.20}},
	{"claude-opus-5", Price{5, 25, 0}},
	{"claude-opus-4-1", Price{15, 75, 0}},
	{"claude-opus-4-0", Price{15, 75, 0}},
	{"claude-opus-4-2025", Price{15, 75, 0}},
	{"claude-opus-4", Price{5, 25, 0}},
	{"claude-sonnet-5", Price{2, 10, 0}},
	{"claude-sonnet-4", Price{3, 15, 0}},
	{"claude-3-7-sonnet", Price{3, 15, 0}},
	{"claude-haiku-4", Price{1, 5, 0}},
	{"claude-3-5-haiku", Price{0.8, 4, 0}},
}

func PriceFor(model string) (Price, bool) {
	best, bestLen := Price{}, -1
	for _, e := range prices {
		if strings.HasPrefix(model, e.prefix) && len(e.prefix) > bestLen {
			best, bestLen = e.p, len(e.prefix)
		}
	}
	return best, bestLen >= 0
}

type TokenUsage struct {
	Input, Output, CacheRead, CacheWrite5m, CacheWrite1h int64
}

func (u *TokenUsage) Add(o TokenUsage) {
	u.Input += o.Input
	u.Output += o.Output
	u.CacheRead += o.CacheRead
	u.CacheWrite5m += o.CacheWrite5m
	u.CacheWrite1h += o.CacheWrite1h
}

func (u TokenUsage) Total() int64 {
	return u.Input + u.Output + u.CacheRead + u.CacheWrite5m + u.CacheWrite1h
}

// Cost estimates dollars; fast mode is billed at twice the standard rate.
func Cost(model string, u TokenUsage, fast bool) float64 {
	p, ok := PriceFor(model)
	if !ok {
		return 0
	}
	read := p.CacheRead
	if read == 0 {
		read = p.Input * 0.1
	}
	c := float64(u.Input)*p.Input +
		float64(u.Output)*p.Output +
		float64(u.CacheRead)*read +
		float64(u.CacheWrite5m)*p.Input*1.25 +
		float64(u.CacheWrite1h)*p.Input*2
	if fast {
		c *= 2
	}
	return c / 1e6
}
