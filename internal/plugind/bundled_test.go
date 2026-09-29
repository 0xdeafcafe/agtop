package plugind

import (
	"context"
	"encoding/json/jsontext"
	"io"
	"os"
	"testing"
	"time"

	"github.com/0xdeafcafe/agtop/internal/plugin"
)

// The test binary stands in for agtop: a bundled plugin runs as
// `<exe> plugin run <name>`.
func TestMain(m *testing.M) {
	if len(os.Args) == 4 && os.Args[1] == "plugin" && os.Args[2] == "run" {
		registerE2E()
		if err := plugin.RunBundled(os.Args[3]); err != nil {
			os.Stderr.WriteString(err.Error() + "\n")
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func registerE2E() (forget func()) {
	return plugin.RegisterBundle(plugin.Bundle{
		Manifest: plugin.Manifest{Name: "bundle-e2e", Command: []string{"agtop"}, UI: []string{plugin.UINotify}},
		Run: func(rw io.ReadWriteCloser) error {
			conn := plugin.NewConn(rw, func(context.Context, string, jsontext.Value) (any, error) {
				return map[string]any{}, nil
			})
			<-conn.Done()
			return nil
		},
	})
}

// A bundled plugin runs without an approval or a sandbox, as agtop itself,
// and stops when it's turned off.
func TestBundledPluginRuns(t *testing.T) {
	defer registerE2E()()
	b := testBroker(t)
	if _, ok := plugin.Enabled()["bundle-e2e"]; !ok {
		t.Fatal("bundled plugin not enabled")
	}
	b.reload()
	r := b.runner("bundle-e2e")
	if r == nil {
		t.Fatal("the broker didn't take it on")
	}
	defer r.shutdown()
	for deadline := time.Now().Add(10 * time.Second); r.status().State != "running"; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("not running: %+v", r.status())
		}
	}
	if err := plugin.SetBundled("bundle-e2e", false); err != nil {
		t.Fatal(err)
	}
	b.reload()
	if b.runner("bundle-e2e") != nil {
		t.Fatal("turned off, it should stop")
	}
}
