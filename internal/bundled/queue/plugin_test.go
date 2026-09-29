package queue

import (
	"context"
	"encoding/json/jsontext"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/0xdeafcafe/rush/internal/plugin"
)

func TestParse(t *testing.T) {
	for _, args := range [][]string{{"a1", "2", "--was", "hi there"}, {"--was", "hi there", "a1", "2"}, {"a1", "--was=hi there", "2"}} {
		id, n, was, err := parse(args)
		if err != nil || id != "a1" || n != 2 || was != "hi there" {
			t.Errorf("%q: %q %d %q %v", args, id, n, was, err)
		}
	}
	for _, args := range [][]string{{"a1"}, {"a1", "x"}, {"a1", "-1"}, {"a1", "1", "2"}, {"a1", "1", "--nope"}} {
		if _, _, _, err := parse(args); err == nil {
			t.Errorf("%q should be refused", args)
		}
	}
}

// A CLI command goes to the broker as a queued op, and says what it did.
func TestCLIRun(t *testing.T) {
	mine, theirs := net.Pipe()
	calls := make(chan string, 4)
	broker := plugin.NewConn(mine, func(_ context.Context, method string, params jsontext.Value) (any, error) {
		calls <- method + " " + string(params)
		if strings.Contains(string(params), `"gone"`) {
			return nil, &plugin.Error{Code: plugin.CodeInvalidParams, Message: "no session gone"}
		}
		return map[string]any{}, nil
	})
	defer broker.Close()
	go func() { _ = Run(theirs) }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cli := func(cmd string, args ...string) plugin.CLIResult {
		var out plugin.CLIResult
		if err := broker.Call(ctx, "cli.run", plugin.CLIRun{Command: cmd, Args: args}, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if out := cli("send", "a1", "0"); out.Exit != 0 || out.Stdout != "sent queued message 0 to a1\n" {
		t.Fatalf("send: %+v", out)
	}
	if c := <-calls; !strings.HasPrefix(c, "sessions.queued.send ") || !strings.Contains(c, `"index":0`) {
		t.Fatalf("broker got %s", c)
	}
	if out := cli("remove", "a1", "1", "--was", "hi"); out.Exit != 0 || !strings.HasPrefix(out.Stdout, "removed queued message 1") {
		t.Fatalf("remove: %+v", out)
	}
	if c := <-calls; !strings.HasPrefix(c, "sessions.queued.remove ") || !strings.Contains(c, `"was":"hi"`) {
		t.Fatalf("broker got %s", c)
	}
	if out := cli("send", "gone", "0"); out.Exit != 1 || !strings.Contains(out.Stderr, "no session gone") {
		t.Fatalf("a failed send: %+v", out)
	}
	if out := cli("send", "a1"); out.Exit != 2 || !strings.Contains(out.Stderr, "usage: rush queue send") {
		t.Fatalf("bad args: %+v", out)
	}
}
