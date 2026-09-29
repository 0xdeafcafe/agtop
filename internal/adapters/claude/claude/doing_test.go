package claude

import "testing"

func TestDoing(t *testing.T) {
	for _, c := range []struct{ tool, in, want string }{
		{"Bash", `{"command":"cd /Users/lw/Source/github.com/langwatch/langwatch && pnpm test"}`, "running pnpm test"},
		{"Bash", `{"command":"grep -rn \"agtools\\.\" internal","description":"Find agtools uses"}`, "find agtools uses"},
		{"Bash", `{"command":"GOFLAGS=-mod=mod go vet ./..."}`, "running go vet"},
		{"Bash", `{"command":"grep -n \"func PrettyModel\" -A12 internal/ui"}`, "searching for func PrettyModel"},
		{"Bash", `{"description":"PR checks"}`, "PR checks"},
		{"Bash", `{"command":"grep -rn \"agtools\\.\\|mcp\" internal"}`, "searching for agtools\\.\\|mcp"},
		{"Bash", `{"command":"cd x"}`, "running a command"},
		{"Read", `{"file_path":"/a/b/view.go"}`, "reading view.go"},
		{"Grep", `{"pattern":"func PrettyModel"}`, "searching for func PrettyModel"},
		{"WebFetch", `{"url":"https://www.example.com/x"}`, "reading example.com"},
		{"Agent", `{"description":"Explore the repo"}`, "subagent: explore the repo"},
		{"mcp__claude_ai_Notion__notion-search", `{}`, "notion-search · Notion"},
		{"Frobnicate", `{"query":"x"}`, "Frobnicate x"},
	} {
		if got := Doing(c.tool, []byte(c.in)); got != c.want {
			t.Errorf("Doing(%s, %s) = %q, want %q", c.tool, c.in, got, c.want)
		}
	}
}
