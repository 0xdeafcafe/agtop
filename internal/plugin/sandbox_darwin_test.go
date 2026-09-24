package plugin

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestSandboxHolds runs a real plugin in the real sandbox and has it try to
// get out: read your files, write outside its data folder, start a program,
// reach the internet, localhost or agtop's own sockets.
func TestSandboxHolds(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and sandboxes a program")
	}
	// The sandbox is keyed on resolved paths; so is everything here.
	home, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("AGTOP_HOME", home)
	t.Setenv("ANTHROPIC_API_KEY", "sk-secret")
	dir := install(t, Manifest{Name: "probe", Command: []string{"probe"}, Network: []string{"example.com:443"}}, nil)
	build := exec.Command("go", "build", "-o", filepath.Join(dir, "probe"), "./testdata/probe")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	p, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	secretDir := filepath.Join(home, "secrets")
	_ = os.MkdirAll(secretDir, 0o700)
	secret := filepath.Join(secretDir, "token")
	_ = os.WriteFile(secret, []byte("sk-secret"), 0o600)
	sock := filepath.Join(home, "agtop.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	proxy, err := Listen(p.Network, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()

	l := Launch{Plugin: p, ProxyPort: proxy.Port()}
	cmd, err := l.Command()
	if err != nil {
		t.Fatal(err)
	}
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	mine, theirs := os.NewFile(uintptr(fds[0]), "a"), os.NewFile(uintptr(fds[1]), "b")
	cmd.ExtraFiles = []*os.File{theirs}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	theirs.Close()
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	nc, err := net.FileConn(mine)
	if err != nil {
		t.Fatal(err)
	}
	conn := NewConn(nc, nil)
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var got map[string]string
	err = conn.Call(ctx, "probe", map[string]any{
		"secret": secret, "outside": filepath.Join(dir, "planted"), "sock": sock,
		"data": DataDir("probe"), "proxy": proxy.Port(),
	}, &got)
	if err != nil {
		t.Fatalf("probe: %v\nstderr: %s", err, stderr.String())
	}
	want := map[string]string{
		"read secret": "denied", "write outside": "denied", "list secret dir": "denied",
		"exec": "denied", "internet": "denied", "localhost": "denied", "agtop socket": "denied",
		"write data": "allowed", "proxy": "allowed",
	}
	for k, v := range got {
		t.Logf("%-16s %s", k, v)
	}
	for k, w := range want {
		if !strings.HasPrefix(got[k], w) {
			t.Errorf("%s: %s, want %s", k, got[k], w)
		}
	}
	if got["token"] != "" {
		t.Error("the plugin saw agtop's API key")
	}
	if got["home"] != DataDir("probe") {
		t.Errorf("HOME = %q", got["home"])
	}
}

func TestProfileQuotesPaths(t *testing.T) {
	if q := quote(`/a "b"\c`); q != `"/a \"b\"\\c"` {
		t.Fatalf("quote = %s", q)
	}
}

// TestProfileWrites gives write paths to the sandbox, and no others.
func TestProfileWrites(t *testing.T) {
	home, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("AGTOP_HOME", home)
	other := filepath.Join(home, "kanban")
	dir := install(t, Manifest{Name: "w", Command: []string{"/bin/sh"}, Write: []string{other}}, nil)
	p, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	prof, err := Launch{Plugin: p}.Profile()
	if err != nil {
		t.Fatal(err)
	}
	var writes string
	for _, l := range strings.Split(prof, "\n") {
		if strings.HasPrefix(l, "(allow file-write*") {
			writes = l
		}
	}
	if !strings.Contains(writes, quote(other)) || !strings.Contains(writes, quote(DataDir("w"))) {
		t.Fatalf("writes = %s", writes)
	}
}
