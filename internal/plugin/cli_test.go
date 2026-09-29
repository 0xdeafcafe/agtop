package plugin

import (
	"strings"
	"testing"
)

func TestValidateCLI(t *testing.T) {
	ok := Manifest{Name: "q", Command: []string{"q"}, CLI: []CLISpec{{Name: "send", Usage: "<id> <n>", Description: "send it"}}}
	if err := ok.validateCLI(); err != nil {
		t.Fatal(err)
	}
	for why, m := range map[string]Manifest{
		"bad name":       {CLI: []CLISpec{{Name: "Send", Description: "x"}}},
		"no description": {CLI: []CLISpec{{Name: "send"}}},
		"twice":          {CLI: []CLISpec{{Name: "a", Description: "x"}, {Name: "a", Description: "y"}}},
		"two lines":      {CLI: []CLISpec{{Name: "a", Description: "x\ny"}}},
		"mcp":            {Protocol: ProtoMCP, CLI: []CLISpec{{Name: "a", Description: "x"}}},
	} {
		if m.validateCLI() == nil {
			t.Errorf("%s: should be refused", why)
		}
	}
	if err := ok.CheckCLIRun(CLIRun{Command: "nope"}); err == nil {
		t.Error("an unknown command should be refused")
	}
	if err := ok.CheckCLIRun(CLIRun{Command: "send", Args: []string{strings.Repeat("x", maxCLIArg+1)}}); err == nil {
		t.Error("a huge argument should be refused")
	}
	if got := (CLIResult{Stdout: strings.Repeat("x", MaxCLIOutput+10)}).Clip(); len(got.Stdout) > MaxCLIOutput+20 {
		t.Errorf("clipped to %d", len(got.Stdout))
	}
}
