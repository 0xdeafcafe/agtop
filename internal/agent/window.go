package agent

// ContextWindower is an agent that knows its models' context windows.
type ContextWindower interface {
	ContextWindow(model string) int64
}

// DefaultWindow is the window assumed for a model no agent says the size
// of.
const DefaultWindow = 200_000

// ContextWindow is model's context window as agent k knows it, else
// DefaultWindow. An empty k is the one an older record meant.
func ContextWindow(k Kind, model string) int64 {
	if k == "" {
		k = LegacyKind
	}
	if w, ok := As[ContextWindower](k); ok {
		if n := w.ContextWindow(model); n > 0 {
			return n
		}
	}
	return DefaultWindow
}
