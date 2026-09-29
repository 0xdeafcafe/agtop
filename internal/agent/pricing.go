package agent

import "github.com/0xdeafcafe/agtop/internal/agent/usage"

// DefaultModeler is an agent that says which model its sessions run when
// none is picked, to price tokens whose model isn't known.
type DefaultModeler interface {
	DefaultModel() string
}

// Price is what agent k's tokens u cost on model, at its default model's
// prices when model has none; false when k can't say.
func Price(k Kind, model string, u usage.TokenUsage) (float64, bool) {
	pr, ok := As[Pricer](k)
	if !ok {
		return 0, false
	}
	if c, ok := pr.Cost(model, u); ok {
		return c, true
	}
	if d, ok := As[DefaultModeler](k); ok {
		return pr.Cost(d.DefaultModel(), u)
	}
	return 0, false
}
