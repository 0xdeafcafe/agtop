package tool

import "testing"

func TestDoing(t *testing.T) {
	for _, c := range []struct {
		call Call
		want string
	}{
		{Call{Kind: Shell, Input: Input{Command: "cd x && pnpm test"}}, "running pnpm test"},
		{Call{Kind: Shell, Input: Input{Command: "rg -n foo", Description: "Find foo"}}, "find foo"},
		{Call{Kind: Delete, Input: Input{Path: "/a/b.go"}}, "deleting b.go"},
		{Call{Kind: Question, Title: "Which one?\nmore"}, "Which one?"},
		{Call{Kind: MCP, Input: Input{Server: "linear", Tool: "get_issue"}}, "get issue · linear"},
		{Call{Kind: Other, Name: "update_plan", Title: "Planning"}, "Planning"},
		{Call{Kind: Other, Name: "frob"}, "frob"},
	} {
		if got := Doing(c.call); got != c.want {
			t.Errorf("Doing(%+v) = %q, want %q", c.call, got, c.want)
		}
	}
}
