package claude

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	osuser "os/user"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
)

// Login is one Claude account agtop can sign ~/.claude in as. Every
// session shares ~/.claude (settings, transcripts, history); what differs
// between accounts is only the sign-in, which agtop keeps in its Vault and
// puts in place when you switch.
type Login struct {
	Name  string `json:"name"`
	ID    string `json:"id"` // the account's uuid
	Email string `json:"email,omitempty"`
	Org   string `json:"org,omitempty"`
	// Profile is the oauthAccount block Claude Code keeps beside the
	// sign-in in its state file: who the account is, not a secret.
	Profile json.RawMessage `json:"profile,omitempty"`
}

// UsageKey is where the login's usage readings are kept.
func (l Login) UsageKey() string { return "login:" + l.ID }

// Vault keeps each login's sign-in while it isn't the one in ~/.claude:
// in the login keychain on macOS, in files only you can read elsewhere.
type Vault struct{ Dir string }

const vaultService = "agtop-login"

func (v Vault) Get(id string) ([]byte, error) {
	if runtime.GOOS == "darwin" {
		return keychainRead(vaultService, id)
	}
	return os.ReadFile(filepath.Join(v.Dir, id+".json"))
}

func (v Vault) Put(id string, cred []byte) error {
	if runtime.GOOS == "darwin" {
		return keychainWrite(vaultService, id, cred)
	}
	if err := os.MkdirAll(v.Dir, 0o700); err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(v.Dir, id+".json"), cred, 0o600)
}

func (v Vault) Forget(id string) error {
	if runtime.GOOS == "darwin" {
		return keychainDelete(vaultService, id)
	}
	return os.Remove(filepath.Join(v.Dir, id+".json"))
}

// lock keeps two agtops from switching at once.
func (v Vault) lock() (func(), error) {
	if err := os.MkdirAll(v.Dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(v.Dir, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil
}

// Signed reports who a config folder is signed in as, from its state file,
// with the sign-in itself; ok is false when it isn't signed in.
func Signed(a Account) (l Login, cred []byte, ok bool) {
	prof := readProfile(a.StatePath())
	cred, err := readCreds(a)
	if prof == nil || err != nil || !usable(cred) {
		return Login{}, nil, false
	}
	var p struct {
		UUID  string `json:"accountUuid"`
		Email string `json:"emailAddress"`
		Org   string `json:"organizationName"`
	}
	if json.Unmarshal(prof, &p) != nil || p.UUID == "" {
		return Login{}, nil, false
	}
	return Login{ID: p.UUID, Email: p.Email, Org: p.Org, Profile: prof}, cred, true
}

// SignedInAs is the uuid of the account a config folder is signed in as,
// from its state file alone.
func SignedInAs(a Account) string {
	var p struct {
		UUID string `json:"accountUuid"`
	}
	if prof := readProfile(a.StatePath()); prof != nil {
		_ = json.Unmarshal(prof, &p)
	}
	return p.UUID
}

// Keep saves the sign-in a folder holds now into the vault, when it has
// changed: Claude Code replaces its tokens as it refreshes them, and only
// the newest works.
func (v Vault) Keep(a Account) (Login, bool, error) {
	l, cred, ok := Signed(a)
	if !ok {
		return Login{}, false, nil
	}
	if old, err := v.Get(l.ID); err == nil && bytes.Equal(old, cred) {
		return l, true, nil
	}
	return l, true, v.Put(l.ID, cred)
}

// Use signs root in as to: whatever root holds now is kept first, so
// switching back finds it as it was. A Claude Code already running keeps
// the account it started with until it restarts; when it next refreshes
// its sign-in it sees the stored one changed and takes that instead of
// writing its own back.
func (v Vault) Use(root Account, to Login) error {
	unlock, err := v.lock()
	if err != nil {
		return err
	}
	defer unlock()
	cred, err := v.Get(to.ID)
	if err != nil || !usable(cred) {
		return fmt.Errorf("agtop has no sign-in for %s; sign in to it again", to.Name)
	}
	if len(to.Profile) == 0 {
		return fmt.Errorf("agtop doesn't know who %s is; sign in to it again", to.Name)
	}
	if _, _, err := v.Keep(root); err != nil {
		return fmt.Errorf("couldn't keep the account in use: %w", err)
	}
	if err := writeCreds(root, cred); err != nil {
		return err
	}
	return writeProfile(root.StatePath(), to.Profile)
}

// Adopt takes the sign-in a fresh folder was just signed in with (the one
// Login ran `claude auth login` in) into the vault, then removes the
// folder and its sign-in: the vault's copy is the only one.
func (v Vault) Adopt(scratch Account) (Login, error) {
	l, cred, ok := Signed(scratch)
	if !ok {
		return Login{}, errors.New("not signed in")
	}
	if err := v.Put(l.ID, cred); err != nil {
		return Login{}, err
	}
	_ = deleteCreds(scratch)
	_ = os.RemoveAll(scratch.ConfigDir)
	return l, nil
}

// usable is whether a stored sign-in has what Claude Code needs to carry on
// with it: a refresh token.
func usable(cred []byte) bool {
	var c struct {
		OAuth struct {
			Refresh string `json:"refreshToken"`
		} `json:"claudeAiOauth"`
	}
	return json.Unmarshal(cred, &c) == nil && c.OAuth.Refresh != ""
}

func readProfile(statePath string) json.RawMessage {
	b, err := os.ReadFile(statePath)
	if err != nil {
		return nil
	}
	var f struct {
		OAuth json.RawMessage `json:"oauthAccount"`
	}
	if json.Unmarshal(b, &f) != nil || len(f.OAuth) == 0 || string(f.OAuth) == "null" {
		return nil
	}
	return f.OAuth
}

// writeProfile puts prof in the state file as who it's signed in as,
// leaving everything else. The usage Claude Code cached was the other
// account's, so it goes.
func writeProfile(statePath string, prof json.RawMessage) error {
	all := map[string]json.RawMessage{}
	b, err := os.ReadFile(statePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(b) > 0 {
		if err := json.Unmarshal(b, &all); err != nil {
			return fmt.Errorf("%s isn't readable: %w", statePath, err)
		}
	}
	all["oauthAccount"] = prof
	delete(all, "cachedUsageUtilization")
	out, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(statePath, out, 0o600)
}

// readCreds is the sign-in Claude Code keeps for a config folder: in the
// login keychain on macOS, in .credentials.json elsewhere.
func readCreds(a Account) ([]byte, error) {
	if runtime.GOOS == "darwin" {
		return keychainRead(a.keychainService(), "")
	}
	return os.ReadFile(filepath.Join(a.ConfigDir, ".credentials.json"))
}

func writeCreds(a Account, cred []byte) error {
	if runtime.GOOS == "darwin" {
		return keychainWrite(a.keychainService(), keychainAccount(a.keychainService()), cred)
	}
	return writeFileAtomic(filepath.Join(a.ConfigDir, ".credentials.json"), cred, 0o600)
}

func deleteCreds(a Account) error {
	if runtime.GOOS == "darwin" {
		return keychainDelete(a.keychainService(), "")
	}
	return os.Remove(filepath.Join(a.ConfigDir, ".credentials.json"))
}

func keychainRead(service, account string) ([]byte, error) {
	args := []string{"find-generic-password", "-s", service, "-w"}
	if account != "" {
		args = append(args, "-a", account)
	}
	out, err := exec.Command("/usr/bin/security", args...).Output()
	if err != nil {
		return nil, ErrNotSignedIn
	}
	return bytes.TrimRight(out, "\n"), nil
}

// keychainWrite goes through security's own prompt rather than its
// arguments where it fits, so the sign-in doesn't show in the process
// list; a longer one (a prompt line holds at most 4 KB, and a sign-in with
// MCP servers' logins in it is more) goes as an argument, which only your
// own user can read. security's output echoes the secret back, so it's
// never passed on.
func keychainWrite(service, account string, secret []byte) error {
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

func keychainDelete(service, account string) error {
	args := []string{"delete-generic-password", "-s", service}
	if account != "" {
		args = append(args, "-a", account)
	}
	return exec.Command("/usr/bin/security", args...).Run()
}

// keychainAccount is the account name Claude Code's item is stored under,
// so writing replaces it instead of adding a second; Claude Code uses
// yours.
func keychainAccount(service string) string {
	out, _ := exec.Command("/usr/bin/security", "find-generic-password", "-s", service).Output()
	for _, l := range strings.Split(string(out), "\n") {
		if _, v, ok := strings.Cut(strings.TrimSpace(l), `"acct"<blob>=`); ok {
			if v = strings.Trim(v, `"`); v != "" && v != "<NULL>" {
				return v
			}
		}
	}
	if u, err := osuser.Current(); err == nil {
		return u.Username
	}
	return os.Getenv("USER")
}

func writeFileAtomic(path string, b []byte, mode os.FileMode) error {
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	tmp.Close()
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}
