package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/host"
	"github.com/0xdeafcafe/agtop/internal/state"
	"github.com/0xdeafcafe/agtop/internal/ui"
)

const sessionUsage = `agtop session: run agtop-mode sessions without the view

  agtop session start --cwd DIR [--session-id UUID] [--resume] [--name N]
        [--prompt-file F] [--image PATH]... [--env K=V]... [--meta k=v]...
        [--binary PATH] [--model M] [--effort E] [--permission-mode M] [--json]
  agtop session send <id> [--now] [--image PATH]...   message text on stdin
  agtop session interrupt <id>
  agtop session stop <id>
  agtop session info <id> [--json]
  agtop session list [--json] [--meta k=v]...
  agtop session queue <id> send|remove <n> [--was TEXT]
        send the queued message at position n (from 0, as info's queue
        lists it) now, or drop it; --was names it by its text, so it is
        still found if the queue moved
`

// sessionView is a session as the session commands print it: its info,
// and whether its host is running.
type sessionView struct {
	host.Info
	Alive bool `json:"alive"`
}

func viewOf(i host.Info) sessionView { return sessionView{Info: i, Alive: host.Alive(i.HostPID)} }

// errNotFound is a session id with no session behind it.
var errNotFound = errors.New("not found")

// sessionCmd runs agtop session <sub> and returns the exit code.
func sessionCmd(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(stdout, sessionUsage)
		return 0
	}
	var (
		asJSON bool
		err    error
	)
	sub, rest := args[0], args[1:]
	switch sub {
	case "start":
		asJSON, err = sessionStart(rest, stdout)
	case "send":
		err = sessionSend(rest, stdin, stdout)
	case "interrupt":
		err = sessionControl(rest, stdout, false)
	case "stop":
		err = sessionControl(rest, stdout, true)
	case "info":
		asJSON, err = sessionInfo(rest, stdout)
	case "list":
		asJSON, err = sessionList(rest, stdout)
	case "queue":
		err = sessionQueue(rest, stdout)
	default:
		err = fmt.Errorf("unknown session command %q\n\n%s", sub, sessionUsage)
	}
	if err == nil {
		return 0
	}
	if asJSON {
		writeJSON(stdout, map[string]string{"error": err.Error()})
	} else {
		fmt.Fprintln(stderr, "agtop:", err)
	}
	return 1
}

func writeJSON(w io.Writer, v any) {
	e := json.NewEncoder(w)
	e.SetEscapeHTML(false)
	_ = e.Encode(v)
}

// multi is a flag given any number of times.
type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

// idAndFlags takes the session id, before or after the flags.
func idAndFlags(fs *flag.FlagSet, args []string) (string, error) {
	var id string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		id, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	if id == "" && fs.NArg() > 0 {
		id = fs.Arg(0)
	}
	if id == "" {
		return "", fmt.Errorf("usage: agtop session %s <id>", fs.Name())
	}
	return id, nil
}

func newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func pairs(kvs []string, what string) (map[string]string, error) {
	if len(kvs) == 0 {
		return nil, nil
	}
	m := map[string]string{}
	for _, kv := range kvs {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("--%s wants key=value, not %q", what, kv)
		}
		m[k] = v
	}
	return m, nil
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// sessionExists is whether id has a session folder.
func sessionExists(id string) bool {
	if id == "" || strings.ContainsAny(id, "/\\") || id == "." || id == ".." {
		return false
	}
	st, err := os.Stat(filepath.Join(host.Root(), id))
	return err == nil && st.IsDir()
}

// waitInfo waits for a host just started to publish its info.
func waitInfo(id string) (host.Info, error) {
	var (
		info host.Info
		err  error
	)
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if info, err = host.ReadInfo(id); err == nil && host.Alive(info.HostPID) {
			return info, nil
		}
	}
	if err == nil {
		err = fmt.Errorf("the host for %s stopped right after starting", id)
	}
	return info, err
}

func sessionStart(args []string, stdout io.Writer) (bool, error) {
	fs := newFlags("start")
	var (
		cwd, sessionID, name, promptFile, binary, model, effort, mode string
		resume, asJSON                                                bool
		images, env, meta                                             multi
	)
	fs.StringVar(&cwd, "cwd", "", "")
	fs.StringVar(&sessionID, "session-id", "", "")
	fs.BoolVar(&resume, "resume", false, "")
	fs.StringVar(&name, "name", "", "")
	fs.StringVar(&promptFile, "prompt-file", "", "")
	fs.Var(&images, "image", "")
	fs.Var(&env, "env", "")
	fs.Var(&meta, "meta", "")
	fs.StringVar(&binary, "binary", "", "")
	fs.StringVar(&model, "model", "", "")
	fs.StringVar(&effort, "effort", "", "")
	fs.StringVar(&mode, "permission-mode", "", "")
	fs.BoolVar(&asJSON, "json", false, "")
	if err := fs.Parse(args); err != nil {
		return hasFlag(args, "--json"), err
	}
	if fs.NArg() > 0 {
		return asJSON, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if resume && sessionID == "" {
		return asJSON, errors.New("--resume needs --session-id")
	}
	if sessionID != "" && !uuidRe.MatchString(sessionID) {
		return asJSON, fmt.Errorf("--session-id %q is not a UUID", sessionID)
	}
	for _, kv := range env {
		if k, _, ok := strings.Cut(kv, "="); !ok || k == "" {
			return asJSON, fmt.Errorf("--env wants KEY=value, not %q", kv)
		}
	}
	metaMap, err := pairs(meta, "meta")
	if err != nil {
		return asJSON, err
	}
	var prompt string
	if promptFile != "" {
		var b []byte
		if promptFile == "-" {
			b, err = io.ReadAll(os.Stdin)
		} else {
			b, err = os.ReadFile(promptFile)
		}
		if err != nil {
			return asJSON, err
		}
		prompt = strings.TrimRight(string(b), "\n")
	}
	for i, p := range images {
		if abs, err := filepath.Abs(p); err == nil {
			images[i] = abs
		}
	}

	st := state.Load()
	d := st.Config.Dispatch
	show := func(info host.Info) (bool, error) {
		v := viewOf(info)
		if asJSON {
			writeJSON(stdout, v)
		} else {
			fmt.Fprintf(stdout, "%s %s %s\n", v.ID, v.State, v.Name)
		}
		return asJSON, nil
	}

	var cfg host.Config
	if sessionID != "" {
		id := host.ShortID(sessionID)
		if info, err := host.ReadInfo(id); err == nil {
			if host.Alive(info.HostPID) {
				return show(info) // already running: nothing to start
			}
			if !resume {
				return asJSON, fmt.Errorf("session %s exists and is stopped; pass --resume to bring it back", id)
			}
			// Back from its saved config, as the view's resume does.
			if saved, err := host.ReadConfig(id); err == nil {
				cfg = saved
			}
		}
		if cfg.ID == "" {
			cfg = host.Config{ID: id, SessionID: sessionID}
		}
		cfg.Resume = resume
	}
	if cwd != "" {
		abs, err := filepath.Abs(cwd)
		if err != nil {
			return asJSON, err
		}
		cfg.Cwd = abs
	}
	if cfg.Cwd == "" {
		return asJSON, errors.New("--cwd is required")
	}
	if st, err := os.Stat(cfg.Cwd); err != nil || !st.IsDir() {
		return asJSON, fmt.Errorf("--cwd %s is not a folder", cfg.Cwd)
	}
	if cfg.Account.ConfigDir == "" {
		cfg.Account = st.Config.ActiveAccount()
	}
	cfg.Prompt, cfg.Images = prompt, images
	cfg.Lean, cfg.IdleStop = d.Lean, host.Duration(d.Rest())
	if cfg.LimitMode == "" {
		cfg.LimitMode = d.OnLimit
	}
	cfg.Model = or(model, or(cfg.Model, d.Model))
	cfg.Effort = or(effort, or(cfg.Effort, d.Effort))
	cfg.PermissionMode = or(mode, or(cfg.PermissionMode, d.Permission))
	cfg.Binary = or(binary, cfg.Binary)
	if len(env) > 0 {
		cfg.Env = env
	}
	if metaMap != nil {
		if cfg.Meta == nil {
			cfg.Meta = map[string]string{}
		}
		for k, v := range metaMap {
			cfg.Meta[k] = v
		}
	}
	cfg.Name = or(name, cfg.Name)
	if cfg.Name == "" {
		cfg.Name = firstWordsOf(prompt)
	}
	if cfg.Name == "" && len(images) > 0 {
		cfg.Name = "about " + filepath.Base(images[0])
	}
	if cfg.Name == "" {
		cfg.Name = "fresh session in " + filepath.Base(cfg.Cwd)
	}
	started, err := host.Spawn(cfg)
	if err != nil {
		return asJSON, err
	}
	info, err := waitInfo(started.ID)
	if err != nil {
		return asJSON, err
	}
	return show(info)
}

func hasFlag(args []string, f string) bool {
	for _, a := range args {
		if a == f || a == f+"=true" {
			return true
		}
	}
	return false
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// firstWordsOf is the first few words of a prompt, the name a session
// goes by until it names itself.
func firstWordsOf(text string) string {
	words := strings.Fields(text)
	if len(words) > 6 {
		words = words[:6]
	}
	n := strings.Join(words, " ")
	if r := []rune(n); len(r) > 48 {
		n = string(r[:47]) + "…"
	}
	return n
}

// sessionSend sends stdin to a session, resuming it first if its host is
// not running.
func sessionSend(args []string, stdin io.Reader, stdout io.Writer) error {
	fs := newFlags("send")
	var (
		now    bool
		images multi
	)
	fs.BoolVar(&now, "now", false, "")
	fs.Var(&images, "image", "")
	id, err := idAndFlags(fs, args)
	if err != nil {
		return err
	}
	if !sessionExists(id) {
		return fmt.Errorf("session %s %w", id, errNotFound)
	}
	b, err := io.ReadAll(stdin)
	if err != nil {
		return err
	}
	text := strings.TrimRight(string(b), " \t\r\n")
	if text == "" && len(images) == 0 {
		return errors.New("nothing to send: write the message on stdin")
	}
	for i, p := range images {
		if abs, err := filepath.Abs(p); err == nil {
			images[i] = abs
		}
	}
	if info, err := host.ReadInfo(id); err != nil || !host.Alive(info.HostPID) {
		// Asleep: the message resumes it, as sending from the view does.
		cfg, err := host.ReadConfig(id)
		if err != nil {
			return fmt.Errorf("session %s has no saved config to resume from: %w", id, err)
		}
		d := state.Load().Config.Dispatch
		cfg.Resume, cfg.Prompt, cfg.Images = true, text, images
		cfg.Lean, cfg.IdleStop = d.Lean, host.Duration(d.Rest())
		if _, err := host.Spawn(cfg); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "resumed %s with the message\n", id)
		return nil
	}
	c, err := host.Dial(id)
	if err != nil {
		return err
	}
	defer c.Close()
	// The host replays the conversation first and ends the replay with its
	// info: what comes after that answers this send.
	if err := awaitLine(c, 10*time.Second, func(ev any) bool { _, ok := ev.(host.InfoEvent); return ok }); err != nil {
		return err
	}
	switch {
	case len(images) > 0:
		err = c.SendImages(text, images)
	case now:
		err = c.SendNow(text)
	default:
		err = c.Send(text)
	}
	if err != nil {
		return err
	}
	var failed error
	_ = awaitLine(c, 5*time.Second, func(ev any) bool {
		switch e := ev.(type) {
		case host.ErrorEvent:
			failed = errors.New(e.Error)
			return true
		case host.Sent:
			return true
		case host.InfoEvent:
			return len(e.Info.Queue) > 0 && e.Info.Queue[len(e.Info.Queue)-1] == text
		}
		return false
	})
	if failed != nil {
		return failed
	}
	fmt.Fprintf(stdout, "sent to %s\n", id)
	return nil
}

// sessionQueue sends one queued message now, or removes it.
func sessionQueue(args []string, stdout io.Writer) error {
	fs := newFlags("queue")
	var was string
	fs.StringVar(&was, "was", "", "")
	usage := errors.New("usage: agtop session queue <id> send|remove <n> [--was TEXT]")
	if len(args) < 3 || strings.HasPrefix(args[0], "-") {
		return usage
	}
	id, action, n := args[0], args[1], args[2]
	if err := fs.Parse(args[3:]); err != nil {
		return err
	}
	if action != "send" && action != "remove" {
		return usage
	}
	index, err := strconv.Atoi(n)
	if err != nil || index < 0 {
		return fmt.Errorf("queue position %q is not a number from 0", n)
	}
	if !sessionExists(id) {
		return fmt.Errorf("session %s %w", id, errNotFound)
	}
	info, err := host.ReadInfo(id)
	if err != nil || !host.Alive(info.HostPID) {
		return fmt.Errorf("session %s is not running", id)
	}
	c, err := host.Dial(id)
	if err != nil {
		return err
	}
	defer c.Close()
	var queue []string
	if err := awaitLine(c, 10*time.Second, func(ev any) bool {
		e, ok := ev.(host.InfoEvent)
		if ok {
			queue = e.Info.Queue
		}
		return ok
	}); err != nil {
		return err
	}
	if was == "" {
		if index >= len(queue) {
			return fmt.Errorf("no queued message %d: the queue has %d", index, len(queue))
		}
		was = queue[index]
	}
	before := count(queue, was)
	if action == "send" {
		err = c.SendQueued(index, was)
	} else {
		err = c.RemoveQueued(index, was)
	}
	if err != nil {
		return err
	}
	var failed error
	if err := awaitLine(c, 5*time.Second, func(ev any) bool {
		switch e := ev.(type) {
		case host.ErrorEvent:
			failed = errors.New(e.Error)
			return true
		case host.InfoEvent:
			return count(e.Info.Queue, was) < before
		}
		return false
	}); err != nil {
		return err
	}
	if failed != nil {
		return failed
	}
	if action == "send" {
		fmt.Fprintf(stdout, "sent queued message %d to %s\n", index, id)
	} else {
		fmt.Fprintf(stdout, "removed queued message %d from %s\n", index, id)
	}
	return nil
}

func count(list []string, s string) int {
	n := 0
	for _, x := range list {
		if x == s {
			n++
		}
	}
	return n
}

// awaitLine reads host lines until want accepts one.
func awaitLine(c *host.Client, d time.Duration, want func(any) bool) error {
	timeout := time.After(d)
	for {
		select {
		case line, ok := <-c.Lines:
			if !ok {
				return errors.New("the host closed the connection")
			}
			if ev, err := host.Decode(line); err == nil && want(ev) {
				return nil
			}
		case <-timeout:
			return errors.New("the host did not answer in time")
		}
	}
}

// sessionControl interrupts a session's turn, or stops the session.
func sessionControl(args []string, stdout io.Writer, stop bool) error {
	name := "interrupt"
	if stop {
		name = "stop"
	}
	id, err := idAndFlags(newFlags(name), args)
	if err != nil {
		return err
	}
	if !sessionExists(id) {
		return fmt.Errorf("session %s %w", id, errNotFound)
	}
	info, err := host.ReadInfo(id)
	if err != nil || !host.Alive(info.HostPID) {
		if stop {
			fmt.Fprintf(stdout, "%s is already stopped\n", id)
			return nil
		}
		return fmt.Errorf("session %s is not running", id)
	}
	c, err := host.Dial(id)
	if err != nil {
		return err
	}
	defer c.Close()
	if !stop {
		if err := c.Interrupt(); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "interrupted %s\n", id)
		return nil
	}
	if err := c.Stop(); err != nil {
		return err
	}
	for deadline := time.Now().Add(15 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		if !host.Alive(info.HostPID) {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the host for %s didn't stop", id)
		}
	}
	fmt.Fprintf(stdout, "stopped %s\n", id)
	return nil
}

func sessionInfo(args []string, stdout io.Writer) (bool, error) {
	fs := newFlags("info")
	var asJSON bool
	fs.BoolVar(&asJSON, "json", false, "")
	id, err := idAndFlags(fs, args)
	if err != nil {
		return asJSON || hasFlag(args, "--json"), err
	}
	if !sessionExists(id) {
		return asJSON, errNotFound
	}
	info, err := host.ReadInfo(id)
	if err != nil {
		return asJSON, errNotFound
	}
	v := viewOf(info)
	if asJSON {
		writeJSON(stdout, v)
		return true, nil
	}
	fmt.Fprintf(stdout, "%s %s %s\n  cwd %s\n  session %s\n", v.ID, v.State, v.Name, v.Cwd, v.SessionID)
	return false, nil
}

func sessionList(args []string, stdout io.Writer) (bool, error) {
	fs := newFlags("list")
	var (
		asJSON bool
		meta   multi
	)
	fs.BoolVar(&asJSON, "json", false, "")
	fs.Var(&meta, "meta", "")
	if err := fs.Parse(args); err != nil {
		return hasFlag(args, "--json"), err
	}
	want, err := pairs(meta, "meta")
	if err != nil {
		return asJSON, err
	}
	out := []sessionView{}
	for _, i := range host.List() {
		if matches(i.Meta, want) {
			out = append(out, viewOf(i))
		}
	}
	if asJSON {
		writeJSON(stdout, out)
		return true, nil
	}
	for _, v := range out {
		fmt.Fprintf(stdout, "%s  %-8s %s\n", v.ID, v.State, v.Name)
	}
	return false, nil
}

func matches(have, want map[string]string) bool {
	for k, v := range want {
		if got, ok := have[k]; !ok || got != v {
			return false
		}
	}
	return true
}

// openSolo is agtop open <id> [--solo]: the view of that one session
// alone. Without --solo it is the same view.
func openSolo(args []string) error {
	fs := newFlags("open")
	fs.Bool("solo", true, "")
	id, err := idAndFlags(fs, args)
	if err != nil {
		return err
	}
	if !sessionExists(id) {
		return fmt.Errorf("session %s %w", id, errNotFound)
	}
	if _, err := host.ReadInfo(id); err != nil {
		return fmt.Errorf("session %s %w", id, errNotFound)
	}
	viewGC()
	p := tea.NewProgram(ui.NewSolo(state.Load(), version, id), tea.WithFPS(120))
	// The app it's embedded in may end it at any moment, closing the view:
	// what's in the box is kept first.
	stop := ui.EndOnSignals(p.Kill)
	_, err = p.Run()
	stop()
	ui.FlushDrafts()
	if errors.Is(err, tea.ErrProgramKilled) {
		return nil
	}
	return err
}
