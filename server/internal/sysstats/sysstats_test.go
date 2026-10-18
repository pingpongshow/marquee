package sysstats

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// fakeProc writes a /proc with the machine's CPU counters, memory, and processes
// (pid → ppid, jiffies, resident pages).
func fakeProc(t *testing.T, dir string, user, idle uint64, procs map[int][3]int) {
	t.Helper()
	write := func(p, s string) {
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// cpu user nice system idle iowait irq softirq steal guest guest_nice
	write(filepath.Join(dir, "stat"), "cpu  "+strconv.FormatUint(user, 10)+" 0 0 "+strconv.FormatUint(idle, 10)+" 0 0 0 0 50 0\ncpu0 1 2 3\n")
	write(filepath.Join(dir, "meminfo"), "MemTotal:       16000000 kB\nMemFree:         1000000 kB\nMemAvailable:    12000000 kB\n")
	for pid, p := range procs {
		// pid (comm with) spaces) state ppid ... utime(14) stime(15) ... rss(24)
		f := make([]string, 22)
		for i := range f {
			f[i] = "0"
		}
		f[0], f[1], f[11], f[21] = "S", strconv.Itoa(p[0]), strconv.Itoa(p[1]), strconv.Itoa(p[2])
		s := strconv.Itoa(pid) + " (ffmpeg (x) y) "
		for i, v := range f {
			if i > 0 {
				s += " "
			}
			s += v
		}
		write(filepath.Join(dir, strconv.Itoa(pid), "stat"), s+"\n")
	}
}

func TestSample(t *testing.T) {
	dir := t.TempDir()
	me := os.Getpid()
	// Marquee, its FFmpeg (a child) and the FFmpeg's helper (a grandchild); 999 is someone else.
	procs := map[int][3]int{me: {1, 100, 1000}, me + 1: {me, 50, 2000}, me + 2: {me + 1, 10, 10}, 999: {1, 5000, 99999}}
	fakeProc(t, dir, 1000, 9000, procs)
	s := &Sampler{Proc: dir, NvidiaSMI: "/nonexistent"}
	s.Sample(context.Background()) // primes
	if _, got := s.Snapshot(); len(got) != 0 {
		t.Fatalf("primed sample kept: %v", got)
	}
	// 1000 more jiffies: 600 busy (400 idle); Marquee's tree used 200 of them.
	procs[me] = [3]int{1, 200, 1000}
	procs[me+1] = [3]int{me, 140, 2000}
	procs[me+2] = [3]int{me + 1, 20, 10}
	fakeProc(t, dir, 1600, 9400, procs)
	s.Sample(context.Background())
	_, got := s.Snapshot()
	if len(got) != 1 {
		t.Fatalf("samples: %v", got)
	}
	g := got[0]
	if g.HostCPU < 59.9 || g.HostCPU > 60.1 || g.ServerCPU < 19.9 || g.ServerCPU > 20.1 {
		t.Errorf("cpu: host %v server %v", g.HostCPU, g.ServerCPU)
	}
	page := int64(os.Getpagesize())
	if g.ServerMem != 3010*page || g.HostMemUsed != 4000000*1024 {
		t.Errorf("memory: server %d host %d", g.ServerMem, g.HostMemUsed)
	}
	if g.GPU != -1 {
		t.Errorf("gpu without nvidia-smi: %v", g.GPU)
	}
}

func TestNoProc(t *testing.T) {
	s := &Sampler{Proc: filepath.Join(t.TempDir(), "missing")}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.Run(ctx) // returns at once
	if h, got := s.Snapshot(); len(got) != 0 || h.CPUCores != 0 {
		t.Errorf("samples without /proc: %v %v", h, got)
	}
}
