package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/pprof"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/state"
	"github.com/0xdeafcafe/agtop/internal/ui"
)

// profiled runs f with a CPU profile to $AGTOP_CPUPROFILE and a heap profile
// to $AGTOP_MEMPROFILE when they are set; for the view and for hosts alike.
func profiled(f func()) {
	if p := os.Getenv("AGTOP_CPUPROFILE"); p != "" {
		if out, err := os.Create(p); err == nil {
			_ = pprof.StartCPUProfile(out)
			defer func() { pprof.StopCPUProfile(); out.Close() }()
		}
	}
	if p := os.Getenv("AGTOP_MEMPROFILE"); p != "" {
		defer func() {
			if out, err := os.Create(p); err == nil {
				runtime.GC()
				_ = pprof.Lookup("allocs").WriteTo(out, 0)
				out.Close()
			}
		}()
	}
	f()
}

// soak runs the whole view for a while against the real agents with no
// terminal, then prints what it cost: agtop --soak 60s 200x50.
// AGTOP_RENDER_SELECT opens a Session beside the list, as a user would.
func soak(args []string) {
	d, w, h := 30*time.Second, 200, 50
	if len(args) > 0 {
		if v, err := time.ParseDuration(args[0]); err == nil {
			d = v
		}
	}
	if len(args) > 1 {
		fmt.Sscanf(args[1], "%dx%d", &w, &h)
	}
	viewGC() // as the view runs
	m := ui.New(state.Load(), version)
	m.Offline()
	if want := os.Getenv("AGTOP_RENDER_SELECT"); want != "" {
		m.Select(want)
	}
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	out := &countWriter{}
	p := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(nil), tea.WithOutput(out),
		tea.WithWindowSize(w, h), tea.WithFPS(fps()), tea.WithoutSignalHandler())
	var ru0 syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru0)
	var ms0 runtime.MemStats
	runtime.ReadMemStats(&ms0)
	t0 := time.Now()
	stop := make(chan struct{})
	var peakHeap uint64
	go func() {
		// Sample the live heap the way a user's RSS would see it.
		for {
			select {
			case <-stop:
				return
			case <-time.After(250 * time.Millisecond):
				var ms runtime.MemStats
				runtime.ReadMemStats(&ms)
				peakHeap = max(peakHeap, ms.HeapInuse)
			}
		}
	}()
	_, _ = p.Run()
	close(stop)
	wall := time.Since(t0)
	var ru syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	cpu := time.Duration(ru.Utime.Nano()+ru.Stime.Nano()-ru0.Utime.Nano()-ru0.Stime.Nano()) * time.Nanosecond
	rss := uint64(ru.Maxrss) // bytes on darwin, KiB elsewhere
	if runtime.GOOS != "darwin" {
		rss <<= 10
	}
	fmt.Printf("soak %s at %dx%d\n", wall.Round(time.Millisecond), w, h)
	fmt.Printf("  cpu        %s (%.2f%% of one core)\n", cpu.Round(time.Millisecond), 100*cpu.Seconds()/wall.Seconds())
	fmt.Printf("  max rss    %.1f MB\n", float64(rss)/(1<<20))
	fmt.Printf("  heap now   %.1f MB in use, peak %.1f MB, sys %.1f MB\n", float64(ms.HeapInuse)/(1<<20), float64(peakHeap)/(1<<20), float64(ms.Sys)/(1<<20))
	fmt.Printf("  allocated  %.1f MB (%.1f MB/s), %d objects\n", float64(ms.TotalAlloc-ms0.TotalAlloc)/(1<<20),
		float64(ms.TotalAlloc-ms0.TotalAlloc)/(1<<20)/wall.Seconds(), ms.Mallocs-ms0.Mallocs)
	fmt.Printf("  gc         %d cycles, %s paused\n", ms.NumGC-ms0.NumGC, time.Duration(ms.PauseTotalNs-ms0.PauseTotalNs).Round(time.Microsecond))
	fmt.Printf("  terminal   %.1f KB written in %d writes\n", float64(out.n)/1024, out.writes)
	fmt.Printf("  goroutines %d\n", runtime.NumGoroutine())
	if s := strings.TrimSpace(os.Getenv("AGTOP_RENDER_SELECT")); s != "" {
		fmt.Printf("  session    %q\n", s)
	}
}

type countWriter struct{ n, writes int }

func (c *countWriter) Write(b []byte) (int, error) { c.n += len(b); c.writes++; return len(b), nil }

var _ io.Writer = (*countWriter)(nil)

// fps is the frame rate the view draws at; AGTOP_FPS overrides it, for
// measuring what the rate costs.
func fps() int {
	var n int
	if _, err := fmt.Sscanf(os.Getenv("AGTOP_FPS"), "%d", &n); err == nil && n > 0 {
		return n
	}
	return 120
}
