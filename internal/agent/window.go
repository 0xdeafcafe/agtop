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

// Compaction is where an agent compacts a session's context on its own,
// when its settings or environment set a window smaller than the model's.
// The zero Compaction is none: the model's window is the one that counts.
type Compaction struct {
	// Window is the window it compacts within; the model's own caps it.
	Window int64
	// Headroom is how far short of Window compaction starts.
	Headroom int64
}

// Compacter is an agent whose sessions compact within a window their
// settings and environment can make smaller than the model's.
type Compacter interface {
	// Compaction is where a session on p in cwd, whose process has env
	// (NAME=value), compacts.
	Compaction(p Profile, cwd string, env []string) Compaction
}

// CompactionOf is Compaction as agent k tells it; none for an agent that
// doesn't.
func CompactionOf(k Kind, p Profile, cwd string, env []string) Compaction {
	if k == "" {
		k = LegacyKind
	}
	if c, ok := As[Compacter](k); ok {
		return c.Compaction(p, cwd, env)
	}
	return Compaction{}
}

// Fill is how full a session's context is: Used tokens of Window, the
// window it compacts within, which is Model's unless a Compaction makes it
// smaller; At is where it compacts, zero when only the model's window
// stops it.
type Fill struct {
	Used, Window, Model, At int64
}

// FillOf is used tokens against model's window of model tokens, and c.
func FillOf(used, model int64, c Compaction) Fill {
	f := Fill{Used: used, Window: model, Model: model}
	if c.Window > 0 && c.Window < model {
		f.Window = c.Window
		f.At = max(0, c.Window-c.Headroom)
	}
	return f
}

// Pct is Used as a percentage of Window.
func (f Fill) Pct() float64 {
	if f.Window <= 0 {
		return 0
	}
	return float64(f.Used) / float64(f.Window) * 100
}

// Compacts is whether a window of its own, smaller than the model's,
// counts.
func (f Fill) Compacts() bool { return f.Window < f.Model }

// Of is what's filled, for after its percentage, tok writing a count of
// tokens: "520k of 1M"; with a window of its own, "520k of 400k ·
// auto-compacts at 367k · model 1M".
func (f Fill) Of(tok func(int64) string) string {
	s := tok(f.Used) + " of " + tok(f.Window)
	if !f.Compacts() {
		return s
	}
	if f.At > 0 {
		s += " · auto-compacts at " + tok(f.At)
	}
	return s + " · model " + tok(f.Model)
}
