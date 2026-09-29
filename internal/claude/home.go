package claude

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/0xdeafcafe/agtop/internal/jsonx"
)

// A login's home is a config folder of its own that agtop keeps for it,
// so a session runs as the login whatever ~/.claude is signed in as.
// Only the sign-in and the state file are its own: everything else in it
// links to ~/.claude, so transcripts, settings, plugins and memory are the
// same whichever login a session runs as, and a conversation carries on
// under another login by resuming it there.
//
// A sign-in lives in one place only. Claude Code replaces its tokens as it
// refreshes them and the old ones stop working, so a copy left elsewhere
// soon signs nothing in: the home takes the vault's sign-in once, and
// from then on the vault keeps a copy of the home's (Keep), never the
// other way.

// ownHome are the entries of a home that aren't ~/.claude's.
var ownHome = map[string]bool{".credentials.json": true, ".claude.json": true}

// sharedDirs are made in ~/.claude before it's linked to, so a session in
// a home writes them there rather than into a folder of the home's own.
var sharedDirs = []string{"projects", "todos", "plans", "shell-snapshots", "file-history", "session-env", "sessions"}

// LinkHome makes home a login's config folder: each of root's entries
// linked into it, new ones as root gains them. An entry the home has of
// its own already (a file Claude Code rewrote in place of its link) is
// left alone.
func LinkHome(home, root Account) error {
	if err := os.MkdirAll(home.ConfigDir, 0o700); err != nil {
		return err
	}
	for _, d := range sharedDirs {
		if err := os.MkdirAll(filepath.Join(root.ConfigDir, d), 0o700); err != nil {
			return err
		}
	}
	ents, err := os.ReadDir(root.ConfigDir)
	if err != nil {
		return err
	}
	for _, e := range ents {
		if ownHome[e.Name()] {
			continue
		}
		at := filepath.Join(home.ConfigDir, e.Name())
		if _, err := os.Lstat(at); err == nil {
			continue
		}
		if err := os.Symlink(filepath.Join(root.ConfigDir, e.Name()), at); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
	}
	return syncState(home, root)
}

// sharedState are the parts of ~/.claude.json a home keeps up with: the
// rest (who it's signed in as, its usage) is the home's own.
var sharedState = []string{"mcpServers"}

// syncState copies sharedState from root's state file into the home's,
// once the home has one.
func syncState(home, root Account) error {
	b, err := os.ReadFile(home.StatePath())
	if err != nil {
		return nil //nolint:nilerr // not seeded yet: SeedHome copies it whole
	}
	var own, theirs map[string]jsontext.Value
	if jsonx.Unmarshal(b, &own) != nil {
		return nil
	}
	if rb, err := os.ReadFile(root.StatePath()); err != nil || jsonx.Unmarshal(rb, &theirs) != nil {
		return nil //nolint:nilerr // nothing of root's to keep up with
	}
	changed := false
	for _, k := range sharedState {
		if v, ok := theirs[k]; ok && string(own[k]) != string(v) {
			own[k], changed = v, true
		}
	}
	if !changed {
		return nil
	}
	out, err := jsonx.MarshalIndent(own)
	if err != nil {
		return err
	}
	return writeFileAtomic(home.StatePath(), out, 0o600)
}

// SeedHome gives a home l's sign-in, when it has none that's l's yet,
// and a state file saying it's l: ~/.claude's, for everything else it
// holds (projects trusted, onboarding done). cred is the vault's.
func SeedHome(home, root Account, l Login, cred []byte) error {
	if _, err := os.Stat(home.StatePath()); os.IsNotExist(err) {
		b, err := os.ReadFile(root.StatePath())
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if len(b) == 0 {
			b = []byte("{}")
		}
		if err := writeFileAtomic(home.StatePath(), b, 0o600); err != nil {
			return err
		}
	}
	if SignedInAs(home) != l.ID {
		if err := writeProfile(home.StatePath(), l.Profile); err != nil {
			return err
		}
	}
	if have, err := readCreds(home); err == nil && usable(have) {
		return nil
	}
	if !usable(cred) {
		return fmt.Errorf("agtop has no sign-in for %s; sign in to it again", l.Name)
	}
	return writeCreds(home, cred)
}

// SignHome puts a sign-in just made for l in its home, over whatever it
// had: signing in to a login again.
func SignHome(home Account, l Login, cred []byte) error {
	if _, err := os.Stat(home.ConfigDir); err != nil {
		return nil // no home yet: it's made from the vault when it's used
	}
	if err := writeCreds(home, cred); err != nil {
		return err
	}
	return writeProfile(home.StatePath(), l.Profile)
}

// HasHome is whether a home has a sign-in of its own.
func HasHome(home Account) bool {
	cred, err := readCreds(home)
	return err == nil && usable(cred)
}

// RemoveHome drops a login's home and its sign-in; ~/.claude's entries it
// linked to stay.
func RemoveHome(home Account) error {
	_ = deleteCreds(home)
	return os.RemoveAll(home.ConfigDir)
}
