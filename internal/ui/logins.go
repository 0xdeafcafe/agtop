package ui

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/actions"
	"github.com/0xdeafcafe/agtop/internal/claude"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/host"
	"github.com/0xdeafcafe/agtop/internal/state"
)

// loginsMsg is the logins signed in where agtop looks, and why ~/.claude's
// couldn't be kept, if it couldn't.
type loginsMsg struct {
	found []fleet.Found
	err   error
}

// switchedMsg is ~/.claude signed in as another login, or why it isn't.
type switchedMsg struct {
	to      claude.Login
	why     string
	resumed int // agtop sessions that carried on at once
	err     error
}

// addedLoginMsg is a login just signed in to from Accounts.
type addedLoginMsg struct {
	name string
	l    claude.Login
	err  error
}

// switchGap is how long after a switch agtop waits before switching on its
// own again: the readings of both logins need time to catch up.
const switchGap = 10 * time.Minute

// findLogins keeps ~/.claude's sign-in in the vault and reports the logins
// found; offline (--soak) leaves the keychain alone.
func (m *Model) findLogins() tea.Cmd {
	if m.offline {
		return nil
	}
	cfg := m.store.Config
	return func() tea.Msg {
		found, err := fleet.FindLogins(cfg)
		return loginsMsg{found, err}
	}
}

// fetchLoginUsage refreshes every saved login's plan usage.
func (m *Model) fetchLoginUsage() []tea.Cmd {
	path := filepath.Join(state.Dir(), "usage.json")
	cfg, offline := m.store.Config, m.offline
	var cmds []tea.Cmd
	for _, lg := range cfg.Logins {
		cmds = append(cmds, func() tea.Msg {
			return usageMsg{dir: lg.UsageKey(), u: fleet.RefreshLogin(path, cfg, lg, offline)}
		})
	}
	return cmds
}

// onLogins saves logins not saved yet, and who each saved one is now. A
// new one's usage is read at once, so a switch it makes possible needn't
// wait for the next minute.
func (m *Model) onLogins(msg loginsMsg) tea.Cmd {
	found := msg.found
	if msg.err != nil && !m.keepFailed {
		m.flash("agtop can't switch account: "+msg.err.Error(), true)
	}
	m.keepFailed = msg.err != nil
	cfg := &m.store.Config
	changed, added := false, false
	for _, f := range found {
		i := m.loginIndex(f.Login.ID)
		if i < 0 {
			f.Login.Name = m.loginName(f)
			cfg.Logins = append(cfg.Logins, f.Login)
			changed, added = true, true
			continue
		}
		l := &cfg.Logins[i]
		if l.Email != f.Login.Email || l.Org != f.Login.Org || string(l.Profile) != string(f.Login.Profile) {
			l.Email, l.Org, l.Profile = f.Login.Email, f.Login.Org, f.Login.Profile
			changed = true
		}
	}
	if cfg.Active != "" {
		// Which folder new sessions started in is agtop's choice now.
		cfg.Active, changed = "", true
	}
	if changed {
		_ = m.store.SaveConfig()
		if m.dialog != nil {
			m.loadDialog()
		}
	}
	if added {
		return tea.Batch(m.fetchLoginUsage()...)
	}
	return nil
}

// inUse is the name of the login ~/.claude is signed in as, or the
// folder's name before agtop knows which it is.
func (m *Model) inUse() string {
	for _, l := range m.snap.Logins {
		if l.Current && l.Name != "" {
			return l.Name
		}
	}
	return m.store.Config.ActiveAccount().Name
}

func (m *Model) loginIndex(id string) int {
	for i, l := range m.store.Config.Logins {
		if l.ID == id {
			return i
		}
	}
	return -1
}

// loginName is what a newly found login is called: its folder's name, or
// its email's name for ~/.claude's, unless another login has it already.
func (m *Model) loginName(f fleet.Found) string {
	name := f.Name
	if name == "" || name == claude.DefaultAccount().Name {
		name, _, _ = strings.Cut(f.Login.Email, "@")
	}
	for _, l := range m.store.Config.Logins {
		if strings.EqualFold(l.Name, name) {
			return f.Login.Email
		}
	}
	if name == "" {
		return f.Login.ID[:8]
	}
	return name
}

// autoSwitch signs ~/.claude in as another login when the one in use is
// nearly out of its 5-hour or weekly usage, or an agtop session was
// stopped by a limit on it, unless you asked to stay.
func (m *Model) autoSwitch() tea.Cmd {
	cfg := m.store.Config
	if cfg.StayOnAccount || m.offline || m.switching || time.Since(m.switchedAt) < switchGap || len(m.snap.Logins) < 2 {
		return nil
	}
	root := cfg.ActiveAccount()
	stopped := false
	for _, a := range m.snap.Agents {
		if a.Agtop && a.Account == root.Name && strings.HasPrefix(a.Detail, "usage limit") {
			stopped = true
		}
	}
	to, ok := fleet.NextLogin(m.snap.Logins, stopped)
	if !ok {
		if stopped && m.hasRoom() && time.Since(m.resumedAt) > time.Minute {
			// Stopped under a login ~/.claude has been switched away from
			// since (by another agtop, or before this one could say).
			m.resumedAt = time.Now()
			return func() tea.Msg {
				n := reloginHosts(root)
				if n == 0 {
					return nil
				}
				return doneMsg{text: fmt.Sprintf("%d stopped by a usage limit carry on, on the account now in use", n)}
			}
		}
		return nil
	}
	why := "a session hit a usage limit"
	for _, l := range m.snap.Logins {
		if l.Current && !stopped {
			why = fmt.Sprintf("%s was at %.0f%%", l.Name, l.Usage.Used())
		}
	}
	return m.switchLogin(to.Login, why)
}

// switchLogin signs ~/.claude in as to. Idle agtop sessions rest so their
// next message starts on it, and those a limit stopped carry on now.
func (m *Model) switchLogin(to claude.Login, why string) tea.Cmd {
	if m.switching {
		return nil
	}
	m.switching = true
	root := m.store.Config.ActiveAccount()
	return func() tea.Msg {
		if err := state.Vault().Use(root, to); err != nil {
			return switchedMsg{to: to, err: err}
		}
		return switchedMsg{to: to, why: why, resumed: reloginHosts(root)}
	}
}

// hasRoom is whether the login in use has a recent reading below where
// agtop switches away from it.
func (m *Model) hasRoom() bool {
	for _, l := range m.snap.Logins {
		if l.Current {
			return time.Since(l.Usage.FetchedAt) < 3*claude.UsageEvery && l.Usage.Used() < state.SwitchAt
		}
	}
	return false
}

// reloginHosts tells every agtop session on root that it's signed in as
// another account now, and reports how many a usage limit had stopped. A
// host from before agtop could switch ignores the message: one of those a
// limit stopped is still stopped after it, so its Claude Code (which holds
// the old sign-in) is stopped, and it's told to continue, which starts a
// fresh one.
func reloginHosts(root claude.Account) int {
	n := 0
	for _, info := range host.List() {
		if (info.Account != root.Name && info.Account != "") || info.State == "stopped" {
			continue
		}
		c, err := host.Dial(info.ID)
		if err != nil {
			continue
		}
		err = c.Relogin()
		if err == nil && info.Limit != nil && stillLimited(info.ID) {
			err = restartClaude(c, info, "continue")
		}
		if err == nil && info.Limit != nil {
			n++
		}
		c.Close()
	}
	return n
}

func (m *Model) onSwitched(msg switchedMsg) tea.Cmd {
	m.switching = false
	if msg.err != nil {
		m.flash("couldn't switch to "+msg.to.Name+": "+msg.err.Error(), true)
		return nil
	}
	m.switchedAt = time.Now()
	text := "new sessions run on " + msg.to.Name
	if msg.why != "" {
		text = msg.why + " · " + text
	}
	if msg.resumed > 0 {
		text += fmt.Sprintf(" · %d stopped by the limit carry on", msg.resumed)
	}
	m.flash(text, false)
	if m.dialog != nil {
		m.loadDialog()
	}
	return tea.Batch(append(m.fetchLoginUsage(), m.fetchUsage())...)
}

// addLogin signs in to an account in a folder of its own, then keeps the
// sign-in in the vault and removes the folder: ~/.claude stays as it is
// until you switch to it.
func (m *Model) addLogin(name string) tea.Cmd {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	scratch := claude.Account{Name: name, ConfigDir: filepath.Join(state.Dir(), "signin-"+hex.EncodeToString(b))}
	return tea.ExecProcess(actions.Login(scratch), func(err error) tea.Msg {
		if err != nil {
			return addedLoginMsg{name: name, err: err}
		}
		l, err := state.Vault().Adopt(scratch)
		return addedLoginMsg{name: name, l: l, err: err}
	})
}

func (m *Model) onAddedLogin(msg addedLoginMsg) tea.Cmd {
	if msg.err != nil {
		m.flash("didn't sign in to "+msg.name+": "+msg.err.Error(), true)
		return nil
	}
	cfg := &m.store.Config
	msg.l.Name = msg.name
	if i := m.loginIndex(msg.l.ID); i >= 0 {
		msg.l.Name = cfg.Logins[i].Name
		cfg.Logins[i] = msg.l
		m.flash("signed in to "+msg.l.Name+" again ("+msg.l.Email+")", false)
	} else {
		cfg.Logins = append(cfg.Logins, msg.l)
		m.flash("added "+msg.l.Name+" ("+msg.l.Email+") · enter switches to it", false)
	}
	_ = m.store.SaveConfig()
	if m.dialog != nil {
		m.loadDialog()
	}
	return tea.Batch(m.fetchLoginUsage()...)
}

// useLogin switches to the login named, or with that email.
func (m *Model) useLogin(name string) tea.Cmd {
	for _, l := range m.store.Config.Logins {
		if strings.EqualFold(l.Name, name) || strings.EqualFold(l.Email, name) {
			if l.ID == claude.SignedInAs(m.store.Config.ActiveAccount()) {
				m.flash("already on "+l.Name, false)
				return nil
			}
			return m.switchLogin(l, "")
		}
	}
	m.flash("no account named "+name, true)
	return nil
}

// stillLimited waits up to 3s for a session a limit stopped to carry on.
func stillLimited(id string) bool {
	for range 30 {
		time.Sleep(100 * time.Millisecond)
		for _, info := range host.List() {
			if info.ID == id && info.Limit == nil {
				return false
			}
		}
	}
	return true
}

// restartClaude stops a session's Claude Code, and sends text if there is
// any, which starts a fresh one resuming the conversation: signed in as
// whoever ~/.claude is now, with the settings and MCP servers as they are
// now. Without text it starts again with your next message.
func restartClaude(c *host.Client, info host.Info, text string) error {
	if pid := info.ClaudePID; pid != 0 {
		_ = syscall.Kill(pid, syscall.SIGTERM)
		gone := false
		for range 50 {
			if syscall.Kill(pid, 0) != nil {
				gone = true
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if !gone {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			time.Sleep(200 * time.Millisecond)
		}
	}
	if text == "" {
		return nil
	}
	return c.Send(text)
}

// restart is #restart: the agent's Claude Code starts again, so it picks up
// the account in use and settings changed since. An agtop agent keeps its
// place; any other is relaunched on its account.
func (m *Model) restart(a *fleet.Agent, text string) tea.Cmd {
	if !a.Agtop {
		return m.relaunch(a, "", nil, a.Acct)
	}
	if text == "" && (strings.HasPrefix(a.Detail, "usage limit") || strings.HasPrefix(a.Detail, "API error")) {
		text = "continue"
	}
	id, name := a.ID, a.DisplayName
	run := func() tea.Cmd {
		return func() tea.Msg {
			var info host.Info
			for _, i := range host.List() {
				if i.ID == id {
					info = i
				}
			}
			if info.ID == "" || info.State == "stopped" {
				return doneMsg{err: fmt.Errorf("%s isn't running; a message starts it", name)}
			}
			c, err := host.Dial(id)
			if err != nil {
				return doneMsg{err: err}
			}
			defer c.Close()
			if err := restartClaude(c, info, text); err != nil {
				return doneMsg{err: err}
			}
			if text == "" {
				return doneMsg{text: "restarted " + name + " · it starts again with your next message"}
			}
			return doneMsg{text: "restarted " + name}
		}
	}
	if a.State == "working" || a.State == "blocked" && !strings.HasPrefix(a.Detail, "usage limit") {
		m.confirm = &confirmation{question: "Restart " + name + "?", detail: "it's in the middle of a turn · y cuts it off and resumes the conversation", onYes: run}
		return nil
	}
	return run()
}
