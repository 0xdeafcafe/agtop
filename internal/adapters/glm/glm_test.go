package glm

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

// fakeProgram puts a program called name in dir that answers each request
// it's sent with the next line of the fixture, keeping the requests in log.
func fakeProgram(t *testing.T, dir, name, fixture, log string) {
	t.Helper()
	abs, err := filepath.Abs(fixture)
	if err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
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

// GLM is nowhere until the bridge is installed and has a ZCode to drive.
func TestDormantUntilInstalled(t *testing.T) {
	home, bin := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin)
	t.Setenv("ZCODE_HOME", "")
	t.Setenv("ZCODE_BIN", "")
	old := appBundle
	appBundle = filepath.Join(home, "no-ZCode.app")
	defer func() { appBundle = old }()
	defer agent.Recheck()
	agent.Recheck()
	if _, ok := agent.Get(Kind); !ok || agent.Label(Kind) != "GLM (ZCode)" {
		t.Fatal("GLM isn't registered")
	}
	if p := agent.Path(Kind); p != "" {
		t.Skipf("zcode-acp-server is installed where every machine looks: %s", p)
	}
	if agent.Installed(Kind) || len(Adapter{}.Profiles()) != 0 {
		t.Fatal("GLM shows without the bridge")
	}
	fakeProgram(t, bin, "zcode-acp-server", "testdata/zcode-acp.jsonl", filepath.Join(t.TempDir(), "log"))
	agent.Recheck()
	if !agent.Installed(Kind) {
		t.Fatal("the bridge is on PATH but GLM isn't installed")
	}
	if _, ok := agent.Find("zcode"); !ok && len(Adapter{}.Profiles()) != 0 {
		t.Fatal("GLM shows with the bridge but no ZCode")
	}
	fakeProgram(t, bin, "zcode", "testdata/zcode-acp.jsonl", filepath.Join(t.TempDir(), "log"))
	ps := Adapter{}.Profiles()
	if len(ps) != 1 || ps[0].Dir != filepath.Join(home, ".zcode") {
		t.Fatalf("Profiles = %+v", ps)
	}
}

// A session starts over ACP through the bridge, in the mode asked for.
func TestStart(t *testing.T) {
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "requests")
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	fakeProgram(t, bin, "zcode-acp-server", "testdata/zcode-acp.jsonl", log)
	defer agent.Recheck()
	agent.Recheck()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := Adapter{}.Start(ctx, agent.StartOptions{Dir: t.TempDir(), Mode: "plan"})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var last event.Init
	for last.Mode != "plan" {
		select {
		case ev := <-conn.Events():
			if i, ok := ev.(event.Init); ok {
				last = i
			}
		case <-ctx.Done():
			t.Fatalf("no Init in plan mode; last %+v", last)
		}
	}
	if last.SessionID != "tsk_7f3a9c" || last.Model != "glm-5.3" {
		t.Errorf("Init = %+v", last)
	}
	b, _ := os.ReadFile(log)
	reqs := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(reqs) != 3 || !strings.Contains(reqs[2], `"session/set_mode"`) {
		t.Errorf("requests = %q", reqs)
	}
}

// The Coding Plan's limits are read with ZCode's key, from Z.ai for a
// Z.ai key.
func TestQuota(t *testing.T) {
	dir := t.TempDir()
	cfg, _ := os.ReadFile("testdata/config.json")
	os.MkdirAll(filepath.Join(dir, "v2"), 0o700)
	if err := os.WriteFile(filepath.Join(dir, "v2", "config.json"), cfg, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZCODE_PROVIDER", "")
	fixture := "testdata/quota.json"
	var auth, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, path = r.Header.Get("Authorization"), r.URL.Path
		b, _ := os.ReadFile(fixture)
		w.Write(b)
	}))
	defer srv.Close()
	oldIntl, oldCN := hostIntl, hostCN
	hostIntl, hostCN = srv.URL, "http://cn.invalid"
	defer func() { hostIntl, hostCN = oldIntl, oldCN }()

	p := agent.Profile{Kind: Kind, Dir: dir}
	q, err := Adapter{}.Quota(context.Background(), p, agent.Account{})
	if err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer zai-key.abc" || path != quotaPath || q.Plan != "pro" || len(q.Windows) != 3 {
		t.Fatalf("auth %q path %q, quota %+v", auth, path, q)
	}
	five, _ := q.Window("token_5h")
	week, _ := q.Window("token_week")
	mcp, _ := q.Window("mcp")
	if five.Percent != 42 || five.Label != "5h" || five.ResetsAt.UnixMilli() != 1790524800000 {
		t.Errorf("5h = %+v", five)
	}
	if week.Percent != 11 || week.Label != "7d" {
		t.Errorf("week = %+v", week)
	}
	if mcp.Used != 250 || mcp.Limit != 1000 || mcp.Percent != 25 {
		t.Errorf("mcp = %+v", mcp)
	}
	if w, _ := q.Tightest(""); w.ID != "token_5h" {
		t.Errorf("tightest = %+v", w)
	}

	fixture = "testdata/quota-auth.json"
	if _, err := (Adapter{}).Quota(context.Background(), p, agent.Account{}); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Errorf("a refused key: %v", err)
	}
	if _, err := (Adapter{}).Quota(context.Background(), agent.Profile{Dir: t.TempDir()}, agent.Account{}); err != errNoKey {
		t.Errorf("no key: %v", err)
	}
}
