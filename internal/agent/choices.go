package agent

// Chooser is an adapter that says what a new session can be started with:
// the models, efforts and permission modes it takes (StartOptions' Model,
// Effort and Mode). Settings offers them for that agent; an adapter
// without one takes a typed model, and its own default for the rest.
type Chooser interface {
	Choices() Choices
}

// Choices are what a new session can start with. Each list is offered as
// it is, after the agent's own default, which is the empty ID.
type Choices struct {
	Models  []Choice
	Efforts []Choice
	Modes   []Choice
}

// Choice is one value and what it means: {"sonnet", "faster and cheaper,
// good for routine work"}.
type Choice struct {
	ID   string
	Note string
}

// ChoicesOf are what agent k says a new session can start with.
func ChoicesOf(k Kind) (Choices, bool) {
	a, ok := Get(k)
	if !ok {
		return Choices{}, false
	}
	c, ok := a.(Chooser)
	if !ok {
		return Choices{}, false
	}
	return c.Choices(), true
}
