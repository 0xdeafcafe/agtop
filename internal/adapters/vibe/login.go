package vibe

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/0xdeafcafe/rush/internal/agent"
)

// SignInCommand runs Vibe's own onboarding in the user's terminal. Vibe owns
// its API key and keychain; Rush does not pretend it supports account swapping.
func (Adapter) SignInCommand(p agent.Profile) (*exec.Cmd, error) {
	bin := agent.Path(Kind)
	if bin == "" {
		return nil, fmt.Errorf("Vibe is not installed")
	}
	cmd := exec.Command(bin, "--setup")
	if p.Dir != "" {
		cmd.Env = append(os.Environ(), "VIBE_HOME="+p.Dir)
	}
	return cmd, nil
}

var _ agent.Authenticator = Adapter{}
