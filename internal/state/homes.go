package state

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/0xdeafcafe/agtop/internal/claude"
)

// homesDir is where agtop keeps each Claude login's home (claude.LinkHome).
func homesDir() string { return filepath.Join(Dir(), "claude") }

// ClaudeHome is the config folder agtop keeps for the login with id.
func ClaudeHome(id string) claude.Account {
	return claude.Account{ConfigDir: filepath.Join(homesDir(), id)}
}

// ClaudeUsing is the login new Claude Code sessions run as, in its home;
// "" runs them in ~/.claude as whoever it's signed in as. It's a file of
// its own, written as the switch is made, so a session that rests for a
// switch finds it when it starts again.
func ClaudeUsing() string {
	b, err := os.ReadFile(filepath.Join(homesDir(), "using"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// SetClaudeUsing makes id the login new sessions run as; "" is ~/.claude.
func SetClaudeUsing(id string) error {
	path := filepath.Join(homesDir(), "using")
	if id == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(homesDir(), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(id+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// RunAccount is the config folder new Claude Code sessions run in: the
// home of the login in use, or ~/.claude. It reads the disk.
func (c Config) RunAccount() claude.Account {
	root := c.ActiveAccount()
	id := ClaudeUsing()
	l, ok := c.Login(id)
	if id == "" || !ok {
		return root
	}
	h := ClaudeHome(id)
	h.Name = l.Name
	return h
}

// UseLogin readies l's home and makes it the one new sessions run in; the
// login ~/.claude is signed in as runs there instead, so its sign-in is
// never in two places.
func UseLogin(root claude.Account, l claude.Login) error {
	if claude.SignedInAs(root) == l.ID {
		return SetClaudeUsing("")
	}
	h := ClaudeHome(l.ID)
	if err := claude.LinkHome(h, root); err != nil {
		return err
	}
	if !claude.HasHome(h) {
		cred, err := Vault().Get(l.ID)
		if err != nil {
			return fmt.Errorf("agtop has no sign-in for %s; sign in to it again", l.Name)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if id, err := claude.Owner(ctx, cred); err == nil && id != l.ID {
			return fmt.Errorf("agtop's sign-in for %s is another account's; sign in to it again", l.Name)
		}
		if err := claude.SeedHome(h, root, l, cred); err != nil {
			return err
		}
	} else if err := claude.SeedHome(h, root, l, nil); err != nil {
		return err
	}
	return SetClaudeUsing(l.ID)
}

// AdoptLogin keeps the sign-in just made in scratch as its login's, in
// the vault and, when the login has a home already, there too: signing in
// to it again.
func AdoptLogin(scratch claude.Account) (claude.Login, error) {
	l, err := Vault().Adopt(scratch)
	if err != nil {
		return l, err
	}
	if cred, err := Vault().Get(l.ID); err == nil {
		_ = claude.SignHome(ClaudeHome(l.ID), l, cred)
	}
	return l, nil
}

// ForgetLogin drops what agtop keeps of a login: its vault sign-in and
// its home. Sessions it's the login in use for go back to ~/.claude.
func ForgetLogin(id string) error {
	if ClaudeUsing() == id {
		_ = SetClaudeUsing("")
	}
	_ = claude.RemoveHome(ClaudeHome(id))
	return Vault().Forget(id)
}
