package host

import "testing"

func TestCleanTitle(t *testing.T) {
	for in, want := range map[string]string{
		"Kimi, Pi and Vibe Adapters":                                         "Kimi, Pi and Vibe Adapters",
		"\"Fix Queue Delivery.\"\n\nBecause...":                              "Fix Queue Delivery",
		"Title: Session Header Redesign":                                     "Session Header Redesign",
		"I can't help name this session because it has no task in it at all": "",
		"":                                       "",
		"Describe the task you need a title for": "",
		"What's the task?":                       "",
		"Why Queues Stall":                       "Why Queues Stall",
	} {
		if got := cleanTitle(in); got != want {
			t.Errorf("cleanTitle(%q) = %q, want %q", in, got, want)
		}
	}
}
