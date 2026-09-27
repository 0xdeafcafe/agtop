package agent

import (
	"os"
	"path/filepath"
	"testing"
)

type progAdapter struct {
	kind Kind
	name string
	dirs []string
}

func (p progAdapter) Kind() Kind                  { return p.kind }
func (p progAdapter) Name() string                { return string(p.kind) }
func (progAdapter) Caps() Caps                    { return 0 }
func (progAdapter) Profiles() []Profile           { return nil }
func (p progAdapter) Program() (string, []string) { return p.name, p.dirs }

// An agent shows as installed when its program is on PATH or where its
// installer puts it, and a program installed later shows after Recheck.
func TestInstalled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	Register(progAdapter{kind: "test-here", name: "agtop-test-here", dirs: []string{".here/bin"}})
	Register(progAdapter{kind: "test-later", name: "agtop-test-later"})
	defer func() {
		mu.Lock()
		delete(adapters, "test-here")
		delete(adapters, "test-later")
		mu.Unlock()
		Recheck()
	}()
	install := func(dir, name string) string {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	here := install(filepath.Join(home, ".here", "bin"), "agtop-test-here")
	Recheck()
	if !Installed("test-here") || Path("test-here") != here {
		t.Fatalf("test-here: installed %v at %q, want %q", Installed("test-here"), Path("test-here"), here)
	}
	if Installed("test-later") {
		t.Fatal("test-later is installed before it is")
	}
	install(filepath.Join(home, ".local", "bin"), "agtop-test-later")
	if Installed("test-later") {
		t.Fatal("a look is kept for a while, not made on every ask")
	}
	Recheck()
	if !Installed("test-later") {
		t.Fatal("test-later isn't found after Recheck")
	}
	for _, a := range InstalledAll() {
		if a.Kind() == "test-nothing" {
			t.Fatal("an unregistered agent is installed")
		}
	}
}
