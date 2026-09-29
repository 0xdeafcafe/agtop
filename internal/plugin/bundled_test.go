package plugin

import (
	"io"
	"testing"
)

func TestBundledOnUntilTurnedOff(t *testing.T) {
	t.Setenv("AGTOP_HOME", t.TempDir())
	defer RegisterBundle(Bundle{Manifest: Manifest{Name: "bundle-test", Command: []string{"agtop"}, UI: []string{UINotify}},
		Run: func(io.ReadWriteCloser) error { return nil }})()
	if _, ok := Enabled()["bundle-test"]; !ok || !BundledOn("bundle-test") {
		t.Fatal("a bundled plugin should be on without approval")
	}
	p, err := Verify("bundle-test")
	if err != nil || !p.Bundled || p.Dir != "" {
		t.Fatalf("verify: %+v %v", p, err)
	}
	if err := SetBundled("bundle-test", false); err != nil {
		t.Fatal(err)
	}
	if _, ok := Enabled()["bundle-test"]; ok || BundledOn("bundle-test") {
		t.Fatal("turned off, it shouldn't run")
	}
	if _, err := Verify("bundle-test"); err == nil {
		t.Fatal("a bundled plugin turned off shouldn't verify")
	}
	if err := SetBundled("bundle-test", true); err != nil || !BundledOn("bundle-test") {
		t.Fatalf("on again: %v", err)
	}
	if err := SetBundled("not-bundled", false); err == nil {
		t.Fatal("only a bundled plugin can be turned off")
	}
}

func TestBundledManifestIsChecked(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("a bundled plugin asking for the network should be refused")
		}
	}()
	RegisterBundle(Bundle{Manifest: Manifest{Name: "bundle-net", Command: []string{"agtop"}, Network: []string{"example.com:443"}}})
}
