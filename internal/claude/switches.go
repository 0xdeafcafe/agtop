package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Who is the account a Claude Code signs in as: who it is, not a secret.
type Who struct {
	ID    string `json:"id"`
	Email string `json:"email,omitempty"`
	Org   string `json:"org,omitempty"`
}

// Label is how the account is named to you: its email, else its org.
func (w Who) Label() string {
	switch {
	case w.Email != "":
		return w.Email
	case w.Org != "":
		return w.Org
	}
	if len(w.ID) > 8 {
		return w.ID[:8]
	}
	return w.ID
}

// WhoIs is who a config folder is signed in as, from its state file alone:
// the account a Claude Code started there now runs on.
func WhoIs(a Account) Who {
	var p struct {
		UUID  string `json:"accountUuid"`
		Email string `json:"emailAddress"`
		Org   string `json:"organizationName"`
	}
	if prof := readProfile(a.StatePath()); prof != nil {
		_ = json.Unmarshal(prof, &p)
	}
	return Who{ID: p.UUID, Email: p.Email, Org: p.Org}
}

// Switch is a config folder signed in as another account.
type Switch struct {
	At   time.Time `json:"at"`
	Dir  string    `json:"dir"` // the config folder
	From Who       `json:"from"`
	To   Who       `json:"to"`
}

// keptSwitches is how many switches are remembered: enough to tell the
// account of any Claude Code still running.
const keptSwitches = 50

func (v Vault) SwitchesPath() string { return filepath.Join(v.Dir, "switches.json") }

// Switches are the switches agtop made, oldest first.
func (v Vault) Switches() []Switch {
	var sw []Switch
	if b, err := os.ReadFile(v.SwitchesPath()); err == nil {
		_ = json.Unmarshal(b, &sw)
	}
	return sw
}

// noteSwitch remembers a switch, so a Claude Code started before it can be
// told apart from one started after.
func (v Vault) noteSwitch(s Switch) error {
	sw := append(v.Switches(), s)
	if len(sw) > keptSwitches {
		sw = sw[len(sw)-keptSwitches:]
	}
	b, err := json.Marshal(sw)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(v.Dir, 0o700); err != nil {
		return err
	}
	return writeFileAtomic(v.SwitchesPath(), b, 0o600)
}

// StartedAs is the account a Claude Code started in dir at started signed
// in as, when a switch made since tells it; ok is false when none did, so
// it is on the account dir is signed in as now.
func StartedAs(sw []Switch, dir string, started time.Time) (Who, bool) {
	if started.IsZero() {
		return Who{}, false
	}
	for _, s := range sw {
		if s.Dir == dir && s.At.After(started) {
			return s.From, s.From.ID != ""
		}
	}
	return Who{}, false
}
