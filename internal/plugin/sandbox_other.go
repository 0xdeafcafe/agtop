//go:build !darwin

package plugin

import "os/exec"

// Supported says whether plugins can run here: only where they can be
// sandboxed, which so far is macOS.
func Supported() error { return errNoSandbox }

// Profile has no meaning without a sandbox.
func (l Launch) Profile() (string, error) { return "", errNoSandbox }

// Command refuses: a plugin never runs unsandboxed.
func (l Launch) Command() (*exec.Cmd, error) { return nil, errNoSandbox }
