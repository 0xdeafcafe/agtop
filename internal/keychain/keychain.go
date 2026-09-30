// Package keychain reads and writes generic passwords in the macOS login
// keychain, through security(1).
package keychain

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	osuser "os/user"
	"slices"
	"strings"
)

// ErrNotFound is an item the keychain doesn't have, or won't give.
var ErrNotFound = errors.New("keychain: no such item")

// Read is service's secret for account; an empty account is any.
func Read(service, account string) ([]byte, error) {
	args := []string{"find-generic-password", "-s", service, "-w"}
	if account != "" {
		args = append(args, "-a", account)
	}
	out, err := exec.Command("/usr/bin/security", args...).Output()
	if err != nil {
		return nil, ErrNotFound
	}
	out = bytes.TrimRight(out, "\n")
	// A secret that isn't one line of text (a pretty-printed auth.json)
	// is printed as hex; -g says whether it was.
	if b, err := hex.DecodeString(string(out)); err == nil && len(out) > 0 && printedHex(args) {
		return b, nil
	}
	return out, nil
}

// printedHex is whether security prints the secret args find as hex:
// -g writes it to stderr as password: 0x… when it does.
func printedHex(args []string) bool {
	var stderr bytes.Buffer
	cmd := exec.Command("/usr/bin/security", append(slices.DeleteFunc(slices.Clone(args), func(a string) bool { return a == "-w" }), "-g")...)
	cmd.Stderr = &stderr
	return cmd.Run() == nil && bytes.HasPrefix(stderr.Bytes(), []byte("password: 0x"))
}

// Has is whether service keeps an item for account, found without
// reading its secret.
func Has(service, account string) bool {
	return exec.Command("/usr/bin/security", "find-generic-password", "-s", service, "-a", account).Run() == nil
}

// Write goes through security's own prompt rather than its arguments
// where it fits, so the secret doesn't show in the process list; a longer
// one (a prompt line holds at most 4 KB, and a sign-in with MCP servers'
// logins in it is more) goes as an argument, which only your own user can
// read. security's output echoes the secret back, so it's never passed on.
func Write(service, account string, secret []byte) error {
	var cmd *exec.Cmd
	if line := fmt.Sprintf("add-generic-password -U -a %q -s %q -X %q\n", account, service, hex.EncodeToString(secret)); len(line) < 4000 {
		cmd = exec.Command("/usr/bin/security", "-i")
		cmd.Stdin = strings.NewReader(line)
	} else {
		cmd = exec.Command("/usr/bin/security", "add-generic-password", "-U", "-a", account, "-s", service, "-X", hex.EncodeToString(secret))
	}
	out, err := cmd.CombinedOutput()
	if err == nil && bytes.Contains(out, []byte("unknown command")) {
		err = errors.New("rejected")
	}
	if err != nil {
		return fmt.Errorf("couldn't save a sign-in to the keychain (%s)", service)
	}
	return nil
}

// Delete removes service's item for account; an empty account is any.
func Delete(service, account string) error {
	args := []string{"delete-generic-password", "-s", service}
	if account != "" {
		args = append(args, "-a", account)
	}
	return exec.Command("/usr/bin/security", args...).Run()
}

// User is your user name, the account name programs keep their own
// items under.
func User() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	if u, err := osuser.Current(); err == nil {
		return u.Username
	}
	return ""
}
