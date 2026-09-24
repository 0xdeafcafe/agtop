package plugin

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const (
	sandboxExec = "/usr/bin/sandbox-exec"
	taskPolicy  = "/usr/sbin/taskpolicy"
)

// Supported says whether plugins can run here: only where they can be
// sandboxed.
func Supported() error {
	if _, err := os.Stat(sandboxExec); err != nil {
		return errNoSandbox
	}
	return nil
}

// Profile is the sandbox the plugin runs in. It denies everything, then
// allows only what the plugin needs to load and run: its program, its own
// folder and the system's libraries to read, its data folder to write, and
// one localhost port, its proxy's, to connect to. It may not start another
// program, open a socket anywhere else, or look up system services beyond
// the one libSystem needs at start.
func (l Launch) Profile() (string, error) {
	prog, err := l.Program()
	if err != nil {
		return "", err
	}
	name := l.Plugin.Name
	var b strings.Builder
	b.WriteString("(version 1)\n(deny default)\n")
	b.WriteString("(allow process-exec (literal " + quote(prog) + "))\n")
	if app := pythonApp(prog); app != "" {
		// A framework Python's bin/python3 only starts the real one, and
		// replaces itself with it, so this is still one program.
		b.WriteString("(allow process-exec (literal " + quote(app) + "))\n")
	}

	reads := []string{
		"/usr/lib", "/usr/share", "/System", "/Library/Apple/System",
		"/private/var/db/dyld", "/private/var/db/timezone", "/private/etc/ssl",
		resolved(l.Plugin.Dir), resolved(DataDir(name)),
	}
	reads = append(reads, runtimeDirs(prog)...)
	for _, r := range l.Plugin.Read {
		reads = append(reads, resolved(expandHome(r)))
	}
	writes := []string{resolved(DataDir(name))}
	for _, w := range l.Plugin.Write {
		writes = append(writes, resolved(expandHome(w)))
	}
	reads = append(reads, writes[1:]...)
	b.WriteString("(allow file-read*\n  (literal \"/\") (literal \"/dev/null\") (literal \"/dev/random\") (literal \"/dev/urandom\")\n  (literal \"/private/etc/localtime\")")
	for _, r := range reads {
		b.WriteString("\n  (subpath " + quote(r) + ")")
	}
	b.WriteString(")\n")
	// Finding its libraries means looking at paths on the way to them.
	b.WriteString("(allow file-read-metadata)\n")
	b.WriteString("(allow file-write*")
	for _, w := range writes {
		b.WriteString(" (subpath " + quote(w) + ")")
	}
	b.WriteString(" (literal \"/dev/null\"))\n")
	b.WriteString("(allow file-ioctl (literal \"/dev/null\"))\n")
	b.WriteString("(allow sysctl-read)\n")
	b.WriteString("(allow mach-lookup (global-name \"com.apple.system.notification_center\"))\n")
	if l.ProxyPort > 0 {
		b.WriteString("(allow network-outbound (remote ip " + quote("localhost:"+strconv.Itoa(l.ProxyPort)) + "))\n")
	}
	return b.String(), nil
}

// runtimeDirs are what an interpreter from Homebrew needs to read: its
// keg, the libraries it links, and OpenSSL's configuration.
func runtimeDirs(prog string) []string {
	for _, prefix := range []string{"/opt/homebrew", "/usr/local"} {
		if strings.HasPrefix(prog, prefix+"/") {
			return []string{
				prefix + "/Cellar", prefix + "/opt", prefix + "/lib",
				prefix + "/etc/openssl@3", prefix + "/etc/ca-certificates",
			}
		}
	}
	return nil
}

// pythonApp is the interpreter a framework build of Python's bin/python3
// hands over to (Homebrew's and python.org's are built this way), or "".
func pythonApp(prog string) string {
	i := strings.Index(prog, "/Python.framework/Versions/")
	if i < 0 {
		return ""
	}
	rest := prog[i+len("/Python.framework/Versions/"):]
	v, _, ok := strings.Cut(rest, "/")
	if !ok {
		return ""
	}
	app := prog[:i] + "/Python.framework/Versions/" + v + "/Resources/Python.app/Contents/MacOS/Python"
	if _, err := os.Stat(app); err != nil {
		return ""
	}
	return app
}

// quote writes s as a sandbox profile string.
func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// Command is the plugin's process, sandboxed and clamped to utility QoS so
// it never competes with you or your agents for the CPU. The caller wires
// up its stdio and extra files.
func (l Launch) Command() (*exec.Cmd, error) {
	if err := Supported(); err != nil {
		return nil, err
	}
	data := DataDir(l.Plugin.Name)
	if err := os.MkdirAll(filepath.Join(data, "tmp"), 0o700); err != nil {
		return nil, err
	}
	profile, err := l.Profile()
	if err != nil {
		return nil, err
	}
	prog, _ := l.Program()
	args := append([]string{sandboxExec, "-p", profile, prog}, l.Plugin.Command[1:]...)
	if _, err := os.Stat(taskPolicy); err == nil {
		args = append([]string{taskPolicy, "-c", "utility"}, args...)
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = resolved(l.Plugin.Dir)
	cmd.Env = l.Env()
	// Its own process group, so ending it ends anything it managed to leave.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd, nil
}
