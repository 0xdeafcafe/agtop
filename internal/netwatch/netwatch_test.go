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

func TestSteadiness(t *testing.T) {
	w.Lock()
	defer func() { w.hist, w.fails, w.s = nil, 0, State{}; w.Unlock() }()
	w.s.Known = true
	now := time.Now()
	if ok, why, _ := w.steady(now); !ok {
		t.Fatalf("no checks yet, yet shaky: %v", why)
	}
	w.Unlock()
	if d := w.took(checkResult{up: true, latency: 50 * time.Millisecond}); d != steadyEvery {
		t.Fatalf("steady check next in %s", d)
	}
	if d := w.took(checkResult{why: "timeout"}); d != downFirst {
		t.Fatalf("first failure next in %s", d)
	}
	w.took(checkResult{why: "timeout"})
	w.took(checkResult{why: "timeout"})
	if d := w.took(checkResult{why: "timeout"}); d != downMost {
		t.Fatalf("backoff not capped: %s", d)
	}
	if d := w.took(checkResult{up: true, latency: 50 * time.Millisecond}); d != shakyEvery {
		t.Fatalf("back up after failures, next in %s", d)
	}
	w.Lock()
	if ok, why, _ := w.steady(time.Now()); ok || len(why) != 1 {
		t.Fatalf("failures within the window: steady=%v %v", ok, why)
	}
	if ok, _, _ := w.steady(time.Now().Add(window + time.Second)); !ok {
		t.Fatal("failures past the window still count")
	}
}

func TestRates(t *testing.T) {
	sample = 100 * time.Millisecond
	Start()
	time.Sleep(time.Second)
	s := Now()
	t.Logf("%+v", s)
	if !s.Rated || !s.Known {
		t.Fatal("no reading")
	}
}
