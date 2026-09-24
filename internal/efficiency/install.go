package efficiency

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Plan is what installing or removing a saver will do, shown before it's
// done: the commands in order and the settings it changes.
type Plan struct {
	Saver   *Saver
	Remove  bool
	Cmds    [][]string
	Changes []string
	Undo    string
	// Blocked says why it can't be done here: a package manager missing, or
	// a saver that's only ever set up by hand.
	Blocked string
}

// NewPlan works out what installing (or removing) s would do in env.
func NewPlan(env *Env, s *Saver, remove bool) Plan {
	p := Plan{Saver: s, Remove: remove}
	if st := s.Setting; st != nil {
		where := tilde(env.Settings.Path)
		name := st.Key
		if st.Env {
			name = "env." + st.Key
		}
		if remove {
			p.Changes = []string{fmt.Sprintf("%s   − %s   (back to %s)", where, name, st.Default)}
			p.Undo = fmt.Sprintf("set %s again here", name)
		} else {
			p.Changes = []string{fmt.Sprintf("%s   + %s: %v", where, name, st.Value)}
			p.Undo = fmt.Sprintf("x here removes it; Claude Code then uses %s", st.Default)
		}
		return p
	}
	if len(s.Manual) > 0 && len(s.Install) == 0 {
		p.Blocked = "set up by hand: " + strings.Join(s.Manual, "  ·  ")
		return p
	}
	rs := s.Install
	if remove {
		rs = s.Remove
	}
	r, why := Pick(rs)
	if r == nil {
		p.Blocked = why
		return p
	}
	p.Cmds = r.Plan()
	if !remove {
		if u, _ := Pick(s.Remove); u != nil {
			var parts []string
			for _, st := range u.Steps {
				parts = append(parts, strings.Join(st.Argv, " "))
			}
			p.Undo = strings.Join(parts, " && ")
		}
	}
	return p
}

// backupFiles are what an install may change, copied first.
func backupFiles(env *Env) []string {
	return []string{
		filepath.Join(env.Acct.ConfigDir, "settings.json"),
		filepath.Join(env.Acct.ConfigDir, "CLAUDE.md"),
		env.Acct.StatePath(),
	}
}

// Backup copies the files an install may change into a folder of its own,
// and returns it.
func Backup(env *Env) (string, error) {
	dir := filepath.Join(Dir(), "backups", time.Now().Format("2006-01-02T150405"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	for _, p := range backupFiles(env) {
		src, err := os.Open(p)
		if err != nil {
			continue
		}
		dst, err := os.OpenFile(filepath.Join(dir, filepath.Base(p)), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err == nil {
			_, err = io.Copy(dst, src)
			dst.Close()
		}
		src.Close()
		if err != nil {
			return dir, err
		}
	}
	return dir, nil
}

// Run carries the plan out, logging it as agtop's doing. It returns the
// commands' output, last lines last.
func (p Plan) Run(env *Env) ([]string, error) {
	if p.Blocked != "" {
		return nil, fmt.Errorf("%s", p.Blocked)
	}
	var log []string
	dir, err := Backup(env)
	if err != nil {
		return nil, fmt.Errorf("backing up first: %w", err)
	}
	log = append(log, "backed up to "+tilde(dir))
	if st := p.Saver.Setting; st != nil {
		err = applySetting(env, st, p.Remove)
	} else {
		for _, argv := range p.Cmds {
			log = append(log, "$ "+strings.Join(argv, " "))
			out, rerr := run(env, argv)
			log = append(log, out...)
			if rerr != nil {
				err = fmt.Errorf("%s: %w", argv[0], rerr)
				break
			}
		}
	}
	if err != nil {
		return log, err
	}
	fresh := LoadEnv(env.Acct)
	f := fresh.Detect(p.Saver)
	kind, detail := "install", joinParts(f.Parts)
	switch {
	case p.Saver.Setting != nil:
		kind, detail = "setting", p.Saver.Setting.Key+" = "+f.Value
		if p.Remove {
			detail = p.Saver.Setting.Key + " unset"
		}
	case p.Remove:
		kind, detail = "remove", ""
	}
	_ = AddEvent(Event{Kind: kind, Saver: p.Saver.ID, Account: env.Acct.ConfigDir, Source: "agtop", Detail: detail})
	Remember(fresh, p.Saver.ID, f)
	return log, nil
}

func applySetting(env *Env, st *Setting, remove bool) error {
	var v any = st.Value
	if remove {
		v = nil
	}
	var err error
	if st.Env {
		s := ""
		if !remove {
			s = fmt.Sprint(st.Value)
		}
		err = env.Settings.SetEnv(st.Key, s)
	} else {
		err = env.Settings.Set(st.Key, v)
	}
	if err != nil {
		return err
	}
	return env.Settings.Save()
}

// run runs one command as the account, with package managers' folders on
// the PATH, and keeps the last lines it printed.
func run(env *Env, argv []string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	bin := argv[0]
	if p := LookPath(bin); p != "" {
		bin = p
	}
	c := exec.CommandContext(ctx, bin, argv[1:]...)
	c.Env = append(env.Acct.Env(), "PATH="+strings.Join(searchPath(), string(os.PathListSeparator)), "HOMEBREW_NO_AUTO_UPDATE=1", "CI=1")
	c.Dir, _ = os.UserHomeDir()
	c.Stdin = nil
	var out bytes.Buffer
	c.Stdout, c.Stderr = &out, &out
	err := c.Run()
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) > 12 {
		lines = lines[len(lines)-12:]
	}
	var keep []string
	for _, l := range lines {
		if l = strings.TrimSpace(l); l != "" {
			keep = append(keep, "  "+l)
		}
	}
	return keep, err
}

func tilde(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}
