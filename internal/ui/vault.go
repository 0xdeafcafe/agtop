package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/rush/internal/secrets"
)

// vaultGate is what the vault check decided for the message in a box, kept
// until that message is sent: the secrets saved to the Kanban vault, each
// replaced by {{vault:NAME}}, and the ones you said to send as they are.
type vaultGate struct {
	swaps  []vaultSwap
	skip   []string
	asking bool // a vault call is out for this box
}

type vaultSwap struct{ value, name string }

// apply puts each saved secret's reference in place of its value, with
// the note on using it.
func (g *vaultGate) apply(text string) string {
	for _, s := range g.swaps {
		if strings.Contains(text, s.value) || strings.Contains(text, secrets.Ref(s.name)) {
			text = secrets.Replace(text, s.value, s.name)
		}
	}
	return text
}

// vaultBox is the message box a secret is checked in: its text and the
// pastes its chips stand for.
type vaultBox struct {
	input  *[]rune
	pastes *pastes
}

func (b vaultBox) text() string { return b.pastes.out(*b.input, false) }

// swap puts ref in place of value in the box, so the box as drafts keep it
// no longer holds the secret.
func (b vaultBox) swap(value, ref string) {
	*b.input = []rune(strings.ReplaceAll(string(*b.input), value, ref))
	for k, v := range b.pastes.text {
		b.pastes.text[k] = strings.ReplaceAll(v, value, ref)
	}
}

// next is the first secret in text not yet saved or let through.
func (g *vaultGate) next(text string) *secrets.Detected {
	for _, d := range secrets.Find(text) {
		if !slices.Contains(g.skip, d.Value) {
			return &d
		}
	}
	return nil
}

// vaultStore is the Kanban vault, through its kv CLI.
type vaultStore interface {
	names() ([]string, error)
	add(name, value string) error
}

var vault vaultStore = kvCLI{}

// vaultRules is what the vault tells an agent about a secret pasted into a
// prompt.
const vaultRules = "Pasted into a chat prompt by the user; use it only for the task that prompt asks for, never print or copy it."

type kvCLI struct{}

// path finds kv on PATH, then where Kanban Code installs it.
func (kvCLI) path() (string, error) {
	if p, err := exec.LookPath("kv"); err == nil {
		return p, nil
	}
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".local", "bin", "kv")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", errors.New("kv isn't installed: Kanban Code installs it in ~/.local/bin")
}

// run runs kv with stdin, its output going through files: node cuts a
// piped stdout short when kv exits.
func (k kvCLI) run(stdin string, args ...string) ([]byte, error) {
	bin, err := k.path()
	if err != nil {
		return nil, err
	}
	out, err := os.CreateTemp("", "rush-kv-out-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(out.Name())
	defer out.Close()
	errf, err := os.CreateTemp("", "rush-kv-err-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(errf.Name())
	defer errf.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = strings.NewReader(stdin), out, errf
	runErr := cmd.Run()
	stdout, _ := os.ReadFile(out.Name())
	if runErr != nil {
		msg, _ := os.ReadFile(errf.Name())
		if s := strings.TrimSpace(string(msg)); s != "" {
			return stdout, errors.New(strings.TrimPrefix(s, "kv: "))
		}
		return stdout, fmt.Errorf("kv %s: %w", args[0], runErr)
	}
	return stdout, nil
}

func (k kvCLI) names() ([]string, error) {
	out, err := k.run("", "ls", "--json")
	if err != nil {
		return nil, err
	}
	var list []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, fmt.Errorf("kv ls: %w", err)
	}
	names := make([]string, len(list))
	for i, s := range list {
		names[i] = s.Name
	}
	return names, nil
}

func (k kvCLI) add(name, value string) error {
	_, err := k.run(value, "add", name, "--tier", "judged", "--rules", vaultRules)
	return err
}

// secretHint names a secret without showing it: its first 4 characters
// and its length.
func secretHint(v string) string {
	r := []rune(v)
	return fmt.Sprintf("%s… %d chars", string(r[:min(4, len(r))]), len(r))
}

// askVault checks text for a secret before it goes. When it finds one it
// looks up the vault's names, then asks to save it under a free name: y
// saves it and sends with {{vault:NAME}} in its place, n or esc sends it as
// it is. It says whether it asked, and the command that goes on asking;
// send is the send that asked, run again once you've answered.
func (m *Model) askVault(g *vaultGate, box vaultBox, send func() tea.Cmd) (tea.Cmd, bool) {
	d := g.next(box.text())
	if d == nil {
		return nil, false
	}
	if g.asking {
		m.flash("still checking the vault", false)
		return nil, true
	}
	g.asking = true
	m.flash("a secret in your message · checking the vault", false)
	found := *d
	return func() tea.Msg {
		names, err := vault.names()
		return applyMsg(func(m *Model) tea.Cmd {
			g.asking = false
			if err != nil {
				m.vaultFailed(g, found, err, send)
				return nil
			}
			m.offerVault(g, box, found, secrets.UniqueName(found.SuggestedName, names), send)
			return nil
		})
	}, true
}

func (m *Model) offerVault(g *vaultGate, box vaultBox, d secrets.Detected, name string, send func() tea.Cmd) {
	sendAsIs := func() tea.Cmd {
		g.skip = append(g.skip, d.Value)
		return send()
	}
	m.confirm = &confirmation{
		question: "Save as vault secret " + name + "?",
		detail: fmt.Sprintf("your message has a secret (%s, %s) · saved, it goes as %s and agents use it through kv run",
			strings.ReplaceAll(d.Kind, "_", " "), secretHint(d.Value), secrets.Ref(name)),
		yesText: "save it",
		noText:  "send as is",
		escIsNo: true,
		onNo:    sendAsIs,
		onYes: func() tea.Cmd {
			g.asking = true
			m.flash("saving "+name+" to the vault", false)
			return func() tea.Msg {
				err := vault.add(name, d.Value)
				return applyMsg(func(m *Model) tea.Cmd {
					g.asking = false
					if err != nil {
						m.vaultFailed(g, d, err, send)
						return nil
					}
					g.swaps = append(g.swaps, vaultSwap{d.Value, name})
					box.swap(d.Value, secrets.Ref(name))
					m.emitInput()
					m.flash("saved "+name+" to the vault", false)
					return send()
				})
			}
		},
	}
}

// vaultFailed says why the secret couldn't be saved, and lets you send the
// message as it is or go back to it.
func (m *Model) vaultFailed(g *vaultGate, d secrets.Detected, err error, send func() tea.Cmd) {
	m.confirm = &confirmation{
		question: "Couldn't save the secret to the vault",
		detail:   err.Error(),
		yesText:  "send as is",
		noEnter:  true,
		onYes: func() tea.Cmd {
			g.skip = append(g.skip, d.Value)
			return send()
		},
	}
}
