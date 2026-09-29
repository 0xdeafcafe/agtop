package proc

import (
	"os"
	"testing"
	"time"
)

func TestParseStat(t *testing.T) {
	// A comm holding a space and parens, as a real one can.
	line := []byte("1234 (some (odd) prog) S 1 1234 1234 0 -1 4194304 100 0 0 0 55 20 0 0 20 0 4 0 987654 0 0 18446744073709551615 0 0 0 0 0 0 0 0 0 0 0 0 17 3 0 0 0 0 0 0 0 0 0 0 0 0 0")
	s, ok := parseStat(line)
	if !ok {
		t.Fatal("parseStat failed")
	}
	if s.comm != "some (odd) prog" || s.ppid != 1 {
		t.Fatalf("comm %q ppid %d", s.comm, s.ppid)
	}
	if want := time.Duration(55+20) * time.Second / clockTicks; s.cpu != want {
		t.Fatalf("cpu %v want %v", s.cpu, want)
	}
	if s.startTicks != 987654 {
		t.Fatalf("startTicks %d", s.startTicks)
	}
}

func TestParseStatShort(t *testing.T) {
	if _, ok := parseStat([]byte("1 (x) S 0")); ok {
		t.Fatal("too few fields should fail")
	}
	if _, ok := parseStat([]byte("no parens here")); ok {
		t.Fatal("no parens should fail")
	}
}

// list, Args and fillUsage read this test's own process from /proc.
func TestListAndArgsSelf(t *testing.T) {
	pid := os.Getpid()
	procs := list()
	var self *Proc
	for _, p := range procs {
		if p.PID == pid {
			self = p
		}
	}
	if self == nil {
		t.Fatalf("list() didn't find this process (pid %d) among %d", pid, len(procs))
	}
	if self.Comm == "" || self.Start.IsZero() {
		t.Fatalf("self = %+v", self)
	}
	fillUsage(self)
	if self.Footprint == 0 {
		t.Fatalf("fillUsage left Footprint 0: %+v", self)
	}
	args := Args(pid)
	if len(args) == 0 {
		t.Fatal("Args(self) returned nothing")
	}
	if CommandLine(pid) == "" {
		t.Fatal("CommandLine(self) is empty")
	}
}
