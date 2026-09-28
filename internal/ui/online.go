package ui

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/actions"
	"github.com/0xdeafcafe/agtop/internal/fleet"
	"github.com/0xdeafcafe/agtop/internal/host"
	"github.com/0xdeafcafe/agtop/internal/state"
)

// A Claude Code session agtop isn't hosting stops for good when its turn
// dies on an API error: the network down, a connection dropped or stalled,
// the API overloaded. While one is stopped like that, agtop checks every
// few seconds whether the API can be reached, and once it can tells each to
// continue, as its own sessions do by themselves.

// onlineEvery is how often agtop checks the network while a session waits
// on it.
var onlineEvery = 5 * time.Second

// onlineMost is how many times one session is told to continue before
// agtop leaves it to you: an API that keeps failing gives up in the end.
const onlineMost = 8

// onlineBackoff is how long after a halt a session is told to continue,
// once it has been told tries times since it last answered: at once the
// first time, then 15s, doubling each time, so an API still failing isn't
// asked again and again.
func onlineBackoff(tries int) time.Duration {
	if tries == 0 {
		return 0
	}
	return 15 * time.Second << (tries - 1)
}

type onlineWatch struct {
	checking bool
	checked  time.Time
	sent     map[string]time.Time // agent key → the halt it was told to continue from
	tries    map[string]int       // agent key → continues sent since it last answered
}

type onlineMsg struct{ up bool }

// continueWaiting is the agents an API error stopped that are due to be
// told to continue.
func (m *Model) continueWaiting() []*fleet.Agent {
	w := &m.online
	var out []*fleet.Agent
	for _, a := range m.snap.Agents {
		if !a.Continues(m.snap.At) {
			if a.Spend.Halt == nil {
				delete(w.tries, a.Key) // it answered: it gets its tries back
			}
			continue
		}
		if w.sent[a.Key].Equal(a.Spend.Halt.At) || w.tries[a.Key] >= onlineMost {
			continue
		}
		if m.snap.At.Before(a.Spend.Halt.At.Add(onlineBackoff(w.tries[a.Key]))) {
			continue
		}
		if q := m.localQ[a.Key]; q != nil && len(q.items) > 0 {
			continue // its queue goes to it instead
		}
		out = append(out, a)
	}
	return out
}

// watchOnline checks the network when a session waits on it. Offline
// (--soak) never does.
func (m *Model) watchOnline() tea.Cmd {
	w := &m.online
	if m.offline || w.checking || time.Since(w.checked) < onlineEvery || len(m.continueWaiting()) == 0 {
		return nil
	}
	w.checking = true
	return func() tea.Msg { return onlineMsg{up: host.Reachable()} }
}

// onOnline tells each session an API error stopped to continue, once the
// API can be reached: a few seconds apart, so they don't all send in the same moment.
func (m *Model) onOnline(msg onlineMsg) tea.Cmd {
	w := &m.online
	w.checking, w.checked = false, time.Now()
	if !msg.up {
		return nil
	}
	if w.sent == nil {
		w.sent, w.tries = map[string]time.Time{}, map[string]int{}
	}
	var cmds []tea.Cmd
	for i, a := range m.continueWaiting() {
		w.sent[a.Key] = a.Spend.Halt.At
		w.tries[a.Key]++
		m.loader.Nudge(a.Key)
		key, acct, id, name, wait := a.Key, a.Acct, a.ID, a.DisplayName, time.Duration(i)*3*time.Second
		at := a.Spend.Halt.At
		cmds = append(cmds, func() tea.Msg {
			if !claimContinue(key, at) {
				return nil // another agtop told it
			}
			time.Sleep(wait)
			if err := actions.Reply(acct, id, "continue"); err != nil {
				return doneMsg{err: err}
			}
			return doneMsg{text: "the API is back · " + name + " continues"}
		})
	}
	return tea.Batch(cmds...)
}

// claimContinue claims telling one session to continue from one halt for
// this process, so two agtops open at once don't both send it. Claims a
// few days old are cleared as it goes.
func claimContinue(key string, at time.Time) bool {
	dir := filepath.Join(state.Dir(), "online")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return true
	}
	if old, err := os.ReadDir(dir); err == nil {
		for _, e := range old {
			if st, err := e.Info(); err == nil && time.Since(st.ModTime()) > 72*time.Hour {
				_ = os.Remove(filepath.Join(dir, e.Name()))
			}
		}
	}
	sum := sha256.Sum256([]byte(key + "\x00" + at.UTC().Format(time.RFC3339Nano)))
	f, err := os.OpenFile(filepath.Join(dir, hex.EncodeToString(sum[:8])), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return !os.IsExist(err)
	}
	_ = f.Close()
	return true
}
