package agent

// Published is where an agent's program is published, so its newest
// version can be looked up: the npm or PyPI package it is (either may be
// empty), and its own update command, run as the program with these
// arguments, when it has one.
type Published struct {
	NPM, PyPI string
	Update    []string
}

// Publisher is an adapter whose program says where it's published.
type Publisher interface {
	Published() Published
}
