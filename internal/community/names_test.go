package community

import (
	"strings"
	"testing"
)

func TestNameIsFixedAndIsTheUsername(t *testing.T) {
	if Name("s1") != Name("s1") || !strings.Contains(Name("s1"), "-") {
		t.Fatalf("name: %q", Name("s1"))
	}
	if got := (Author{SessionID: "s1", Name: "Investigator", Handle: "inv"}).Username(); got != "@"+Name("s1") {
		t.Fatalf("a session's username is %q, not its animal name", got)
	}
}
