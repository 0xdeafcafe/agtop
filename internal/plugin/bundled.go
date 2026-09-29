package plugin

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"

	"github.com/0xdeafcafe/agtop/internal/jsonx"
)

// A bundled plugin ships inside agtop. Its code is agtop's own, so it's
// trusted as agtop is: it runs without being approved, outside the sandbox,
// on every system, as `agtop plugin run <name>` speaking the same protocol
// on fd 3 as any other. What it may do in agtop's screen is still its
// manifest's, checked by the broker as for any plugin. Each is on until
// you turn it off.
type Bundle struct {
	Manifest Manifest
	// Run is its main, given its end of the connection to the broker.
	Run func(rw io.ReadWriteCloser) error
}

// bundledDigest stands for a bundled plugin's files: they're agtop's
// binary, which the broker runs from.
const bundledDigest = "bundled"

var (
	bundleMu sync.RWMutex
	bundles  = map[string]Bundle{}
)

// RegisterBundle makes a bundled plugin known. Its package calls it from
// init; a test may call the func it returns to forget it again.
func RegisterBundle(b Bundle) (forget func()) {
	if err := b.Manifest.validateBundled(); err != nil {
		panic("bundled plugin " + b.Manifest.Name + ": " + err.Error())
	}
	bundleMu.Lock()
	defer bundleMu.Unlock()
	bundles[b.Manifest.Name] = b
	return func() {
		bundleMu.Lock()
		defer bundleMu.Unlock()
		delete(bundles, b.Manifest.Name)
	}
}

func (m *Manifest) validateBundled() error {
	if !nameRE.MatchString(m.Name) {
		return errors.New("bad name")
	}
	if m.Proto() != ProtoAgtop || len(m.Network) > 0 || len(m.Exec) > 0 || len(m.Sessions) > 0 {
		return errors.New("a bundled plugin speaks agtop's protocol, with no network, exec or sessions")
	}
	if err := m.validateUI(); err != nil {
		return err
	}
	return nil
}

// Bundles are the bundled plugins, by name.
func Bundles() []Bundle {
	bundleMu.RLock()
	defer bundleMu.RUnlock()
	out := make([]Bundle, 0, len(bundles))
	for _, b := range bundles {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Manifest.Name < out[j].Manifest.Name })
	return out
}

// BundleNamed is the bundled plugin called name.
func BundleNamed(name string) (Bundle, bool) {
	bundleMu.RLock()
	defer bundleMu.RUnlock()
	b, ok := bundles[name]
	return b, ok
}

func offPath() string { return filepath.Join(Root(), "bundled-off.json") }

// BundledOff are the bundled plugins you turned off.
func BundledOff() []string {
	var off []string
	if b, err := os.ReadFile(offPath()); err == nil {
		_ = jsonx.Unmarshal(b, &off)
	}
	return off
}

// BundledOn says whether a bundled plugin runs.
func BundledOn(name string) bool {
	_, ok := BundleNamed(name)
	return ok && !slices.Contains(BundledOff(), name)
}

// SetBundled turns a bundled plugin on or off.
func SetBundled(name string, on bool) error {
	if _, ok := BundleNamed(name); !ok {
		return errors.New(name + " isn't bundled with agtop")
	}
	off := slices.DeleteFunc(BundledOff(), func(n string) bool { return n == name })
	if !on {
		off = append(off, name)
	}
	sort.Strings(off)
	if err := os.MkdirAll(Root(), 0o700); err != nil {
		return err
	}
	b, err := jsonx.Marshal(off)
	if err != nil {
		return err
	}
	tmp := offPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, offPath())
}

// Enabled is every plugin that runs, by name: the approved ones, and the
// bundled ones not turned off. A bundled plugin's name is its own: an
// installed plugin of the same name doesn't run.
func Enabled() map[string]Approval {
	out := Approvals()
	off := BundledOff()
	for _, b := range Bundles() {
		delete(out, b.Manifest.Name)
		if !slices.Contains(off, b.Manifest.Name) {
			out[b.Manifest.Name] = Approval{Digest: bundledDigest, Manifest: b.Manifest}
		}
	}
	return out
}

// RunBundled runs the bundled plugin name on fd 3, as the broker starts it.
func RunBundled(name string) error {
	b, ok := BundleNamed(name)
	if !ok {
		return errors.New(name + " isn't bundled with agtop")
	}
	f := os.NewFile(3, "plugin-ipc")
	if f == nil {
		return errors.New("no connection on fd 3")
	}
	return b.Run(f)
}
