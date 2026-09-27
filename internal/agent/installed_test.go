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

type lesserAdapter struct{ progAdapter }

func (lesserAdapter) Lesser() (string, []string, string) { return "agtop-test-gh", nil, "install it" }

// An agent whose own program is missing but whose lesser one is there is
// installed, can't run sessions, and says what installing it adds.
func TestLesser(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", bin)
	Register(lesserAdapter{progAdapter{kind: "test-lesser", name: "agtop-test-cli"}})
	defer func() {
		mu.Lock()
		delete(adapters, "test-lesser")
		mu.Unlock()
		Recheck()
	}()
	Recheck()
	if Installed("test-lesser") {
		t.Fatal("installed with neither program")
	}
	if err := os.WriteFile(filepath.Join(bin, "agtop-test-gh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	Recheck()
	if !Installed("test-lesser") || Runs("test-lesser") || Hint("test-lesser") != "install it" || Path("test-lesser") != "" {
		t.Fatalf("with only the lesser program: installed %v, runs %v, hint %q, path %q", Installed("test-lesser"), Runs("test-lesser"), Hint("test-lesser"), Path("test-lesser"))
	}
	if err := os.WriteFile(filepath.Join(bin, "agtop-test-cli"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	Recheck()
	if !Runs("test-lesser") || Hint("test-lesser") != "" {
		t.Fatal("its own program doesn't make it run")
	}
}
