package deepseek

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/event"
)

// fakeProgram puts a program called name in dir that checks it was run
// with args, then answers each request it's sent with the next line of
// the fixture, keeping the requests in log.
func fakeProgram(t *testing.T, dir, name, args, fixture, log string) {
	t.Helper()
	abs, err := filepath.Abs(fixture)
	if err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
[ "$*" = "` + args + `" ] || { echo "run with: $*" >&2; exit 2; }
while IFS= read -r resp <&3; do
	IFS= read -r req || exit 0
	printf '%s\n' "$req" >> '` + log + `'
	printf '%s\n' "$resp"
done 3< '` + abs + `'
cat > /dev/null
`
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// DeepSeek is nowhere until dsh is installed, and there once it is.
func TestDormantUntilInstalled(t *testing.T) {
	home, bin := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin)
	t.Setenv("DSH_HOME", "")
	defer agent.Recheck()
	agent.Recheck()
	if _, ok := agent.Get(Kind); !ok || agent.Label(Kind) != "DeepSeek (dsh)" {
		t.Fatal("DeepSeek isn't registered")
	}
	if p := agent.Path(Kind); p != "" {
		t.Skipf("dsh is installed where every machine looks: %s", p)
	}
	if agent.Installed(Kind) || len(Adapter{}.Profiles()) != 0 {
		t.Fatal("DeepSeek shows without dsh")
	}
	for _, a := range agent.InstalledAll() {
		if a.Kind() == Kind {
			t.Fatal("DeepSeek is among the installed agents without dsh")
		}
	}
	fakeProgram(t, bin, "dsh", "--profile acp", "testdata/dsh-acp.jsonl", filepath.Join(t.TempDir(), "log"))
	agent.Recheck()
	if !agent.Installed(Kind) {
		t.Fatal("dsh is on PATH but DeepSeek isn't installed")
	}
	ps := Adapter{}.Profiles()
	if len(ps) != 1 || ps[0].Dir != filepath.Join(home, ".dsh") {
		t.Fatalf("Profiles = %+v", ps)
	}
}

// A session starts over ACP with dsh's acp profile, in the model asked
// for; a mode, which dsh hasn't, is left alone.
func TestStart(t *testing.T) {
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "requests")
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	fakeProgram(t, bin, "dsh", "--profile acp", "testdata/dsh-acp.jsonl", log)
	defer agent.Recheck()
	agent.Recheck()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := Adapter{}.Start(ctx, agent.StartOptions{Dir: t.TempDir(), Model: "deepseek-official/deepseek-v4-pro", Mode: "default"})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var last event.Init
	for last.Model != "deepseek-official/deepseek-v4-pro" {
		select {
		case ev := <-conn.Events():
			if i, ok := ev.(event.Init); ok {
				last = i
			}
		case <-ctx.Done():
			t.Fatalf("no Init in the model asked for; last %+v", last)
		}
	}
	if last.SessionID != "ses_01JQ7ZK4M3" || last.Version != "0.1.7-rc.2" {
		t.Errorf("Init = %+v", last)
	}
	b, _ := os.ReadFile(log)
	reqs := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(reqs) != 3 || !strings.Contains(reqs[1], `"session/new"`) ||
		!strings.Contains(reqs[2], `"session/set_config_option"`) || !strings.Contains(reqs[2], `"configId":"model"`) {
		t.Errorf("requests = %q", reqs)
	}
}

// The balance is read with the key dsh keeps, and says when it's used up.
func TestQuota(t *testing.T) {
	dir := t.TempDir()
	cred, _ := os.ReadFile("testdata/credentials.yaml")
	if err := os.WriteFile(filepath.Join(dir, ".credentials.yaml"), cred, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEEPSEEK_API_KEY", "")
	fixture := "testdata/balance.json"
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		b, _ := os.ReadFile(fixture)
		w.Write(b)
	}))
	defer srv.Close()
	old := balanceURL
	balanceURL = srv.URL
	defer func() { balanceURL = old }()

	p := agent.Profile{Kind: Kind, Dir: dir}
	q, err := Adapter{}.Quota(context.Background(), p, agent.Account{})
	if err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer sk-from-store" || q.Balance != "¥110.00 left" || q.Problem != "" || len(q.Windows) != 0 {
		t.Errorf("auth %q, quota %+v", auth, q)
	}

	fixture = "testdata/balance-empty.json"
	t.Setenv("DEEPSEEK_API_KEY", "sk-from-env")
	q, err = Adapter{}.Quota(context.Background(), p, agent.Account{})
	if err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer sk-from-env" || q.Balance != "$0.00 left" || q.Problem == "" {
		t.Errorf("auth %q, quota %+v", auth, q)
	}

	t.Setenv("DEEPSEEK_API_KEY", "")
	if _, err := (Adapter{}).Quota(context.Background(), agent.Profile{Dir: t.TempDir()}, agent.Account{}); err != errNoKey {
		t.Errorf("no key: %v", err)
	}
}

func TestDotenvKey(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("OTHER=1\nexport DEEPSEEK_API_KEY='sk-dotenv' # home\n"), 0o600)
	t.Setenv("DEEPSEEK_API_KEY", "")
	if k := apiKey(dir); k != "sk-dotenv" {
		t.Errorf("apiKey = %q", k)
	}
}
