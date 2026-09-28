package netwatch

import (
	"errors"
	"net"
	"testing"
	"time"
)

func TestCountersAndNetwork(t *testing.T) {
	c, ok := counters()
	key, label := network()
	t.Logf("counters=%v ok=%v key=%q label=%q", c, ok, key, label)
	if key == "" {
		t.Fatal("no network key")
	}
}

func TestRunPausesWhileDown(t *testing.T) {
	w.Lock()
	w.started, w.s.Known, w.s.Up = true, true, false
	w.Unlock()
	defer func() { w.Lock(); w.started, w.s = false, State{}; w.jobs = map[string]*Job{}; w.Unlock() }()
	if Do("usage", func() error { t.Fatal("ran while down"); return nil }) != ErrOffline {
		t.Fatal("not held back")
	}
	if j := Now().Jobs[0]; !j.Paused || j.Skipped != 1 {
		t.Fatalf("job %+v", j)
	}
	ch := Changes()
	w.s.Known = true
	w.took(checkResult{up: true, latency: time.Millisecond})
	select {
	case <-ch:
	default:
		t.Fatal("coming back up didn't say so")
	}
	if Now().Jobs[0].Paused {
		t.Fatal("still paused once up")
	}
}

func TestIsNetErr(t *testing.T) {
	_, err := net.DialTimeout("tcp", "127.0.0.1:1", time.Second)
	if !IsNetErr(err) || IsNetErr(errors.New("401 unauthorized")) || IsNetErr(ErrOffline) {
		t.Fatal("wrong")
	}
}

func TestGrewWraps(t *testing.T) {
	if grew(10, 25) != 15 || grew(1<<32-10, 5) != 15 || grew(1<<40, 5) != 0 {
		t.Fatal("wrong")
	}
}

func TestRates(t *testing.T) {
	Start()
	time.Sleep(2500 * time.Millisecond)
	s := Now()
	t.Logf("%+v", s)
	if !s.Rated || !s.Known {
		t.Fatal("no reading")
	}
}
