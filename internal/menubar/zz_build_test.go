package menubar

import (
	"os"
	"os/exec"
	"testing"
)

func TestBuildApp(t *testing.T) {
	if os.Getenv("AGTOP_BUILD_APP") == "" {
		t.Skip("set AGTOP_BUILD_APP=1 to build the Swift app")
	}
	t.Setenv("AGTOP_HOME", t.TempDir())
	// Building registers the app; this one mustn't stand in for the real one.
	t.Cleanup(func() { _ = exec.Command(lsregister, "-u", AppPath()).Run() })
	if rebuilt, err := buildApp(); err != nil || !rebuilt {
		t.Fatal(rebuilt, err)
	}
	if out, err := exec.Command("/usr/bin/codesign", "--verify", "--verbose", AppPath()).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if out, err := exec.Command("/usr/bin/plutil", "-lint", AppPath()+"/Contents/Info.plist").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	// A second build with nothing changed is skipped.
	if rebuilt, err := buildApp(); err != nil || rebuilt {
		t.Fatal(rebuilt, err)
	}
}
