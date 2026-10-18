// Package sysstats samples how busy the machine is, the way Plex's dashboard shows it: the
// whole machine's CPU and memory next to Marquee's own (the server and the FFmpeg processes
// it runs), and, on NVIDIA, the GPU and its encoder and decoder. Samples are kept for an
// hour in memory; nothing is stored. On systems without /proc (development on a Mac) there
// are no samples.
package sysstats

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Sample is one reading.
type Sample struct {
	At time.Time
	// HostCPU and ServerCPU are percentages of the whole machine (all cores = 100).
	HostCPU, ServerCPU float64
	// HostMemUsed is the machine's memory in use (total minus available); ServerMem is
	// Marquee's (resident memory of the server and its child processes). Bytes.
	HostMemUsed, ServerMem int64
	// GPU, GPUEncoder and GPUDecoder are NVIDIA utilisation percentages; -1 = not known.
	GPU, GPUEncoder, GPUDecoder float64
}

// Host is what doesn't change between samples.
type Host struct {
	CPUCores    int
	MemTotal    int64
	GPUName     string
	GPUMemTotal int64
}

const keep = 360 // an hour at one sample every 10 s

// Sampler takes a sample every Every (default 10 s) until its context ends.
type Sampler struct {
	Every time.Duration
	// Proc is the proc filesystem (default /proc; tests).
	Proc string
	// NvidiaSMI is nvidia-smi's path; "" looks it up (none = no GPU readings).
	NvidiaSMI string

	mu       sync.Mutex
	samples  []Sample
	host     Host
	asked    time.Time // GPU readings are only taken while someone is looking
	prevTot  uint64
	prevIdle uint64
	prevProc uint64
}

func (s *Sampler) proc() string {
	if s.Proc != "" {
		return s.Proc
	}
	return "/proc"
}

// Run samples until ctx ends.
func (s *Sampler) Run(ctx context.Context) {
	if _, err := os.Stat(filepath.Join(s.proc(), "stat")); err != nil {
		return
	}
	if s.NvidiaSMI == "" {
		if p, err := exec.LookPath("nvidia-smi"); err == nil {
			s.NvidiaSMI = p
		}
	}
	s.mu.Lock()
	s.host = Host{CPUCores: runtime.NumCPU(), MemTotal: memTotal(s.proc())}
	s.mu.Unlock()
	if s.NvidiaSMI != "" {
		if name, mem, ok := gpuInfo(ctx, s.NvidiaSMI); ok {
			s.mu.Lock()
			s.host.GPUName, s.host.GPUMemTotal = name, mem
			s.mu.Unlock()
		}
	}
	every := s.Every
	if every <= 0 {
		every = 10 * time.Second
	}
	s.Sample(ctx) // primes the CPU counters
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Sample(ctx)
		}
	}
}

// Sample takes one reading (the first only primes the CPU counters).
func (s *Sampler) Sample(ctx context.Context) {
	tot, idle, ok := cpuTotals(s.proc())
	if !ok {
		return
	}
	tree := processTree(s.proc(), os.Getpid())
	var procJiffies uint64
	var rss int64
	page := int64(os.Getpagesize())
	for _, pid := range tree {
		j, r := procUsage(s.proc(), pid)
		procJiffies += j
		rss += r * page
	}
	smp := Sample{At: time.Now(), HostMemUsed: memUsed(s.proc()), ServerMem: rss, GPU: -1, GPUEncoder: -1, GPUDecoder: -1}

	s.mu.Lock()
	prevTot, prevIdle, prevProc := s.prevTot, s.prevIdle, s.prevProc
	s.prevTot, s.prevIdle, s.prevProc = tot, idle, procJiffies
	wantGPU := s.NvidiaSMI != "" && time.Since(s.asked) < 2*time.Minute
	s.mu.Unlock()
	if prevTot == 0 || tot <= prevTot {
		return
	}
	dt := float64(tot - prevTot)
	smp.HostCPU = clamp(100 * (1 - float64(idle-prevIdle)/dt))
	if procJiffies >= prevProc {
		smp.ServerCPU = clamp(100 * float64(procJiffies-prevProc) / dt)
	}
	if wantGPU {
		smp.GPU, smp.GPUEncoder, smp.GPUDecoder = gpuUse(ctx, s.NvidiaSMI)
	}
	s.mu.Lock()
	s.samples = append(s.samples, smp)
	if len(s.samples) > keep {
		s.samples = append(s.samples[:0], s.samples[len(s.samples)-keep:]...)
	}
	s.mu.Unlock()
}

// Snapshot returns the machine's details and the last hour's samples, oldest first, and
// notes that someone is looking (so GPU readings are taken for the next two minutes).
func (s *Sampler) Snapshot() (Host, []Sample) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asked = time.Now()
	return s.host, append([]Sample(nil), s.samples...)
}

func clamp(v float64) float64 { return max(0, min(100, v)) }

// cpuTotals reads the machine's total and idle (idle + iowait) jiffies from /proc/stat.
func cpuTotals(proc string) (total, idle uint64, ok bool) {
	b, err := os.ReadFile(filepath.Join(proc, "stat"))
	if err != nil {
		return 0, 0, false
	}
	line, _, _ := bytes.Cut(b, []byte("\n"))
	f := strings.Fields(string(line))
	if len(f) < 5 || f[0] != "cpu" {
		return 0, 0, false
	}
	for i, v := range f[1:] {
		n, _ := strconv.ParseUint(v, 10, 64)
		if i == 7 || i == 8 { // guest time is already counted in user
			continue
		}
		total += n
		if i == 3 || i == 4 {
			idle += n
		}
	}
	return total, idle, true
}

// processTree is pid and all its descendants.
func processTree(proc string, pid int) []int {
	entries, err := os.ReadDir(proc)
	if err != nil {
		return []int{pid}
	}
	children := map[int][]int{}
	for _, e := range entries {
		p, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if pp, ok := ppid(proc, p); ok {
			children[pp] = append(children[pp], p)
		}
	}
	out := []int{pid}
	for i := 0; i < len(out); i++ {
		out = append(out, children[out[i]]...)
	}
	return out
}

// statFields are /proc/<pid>/stat's fields after the command name (field 3 onwards).
func statFields(proc string, pid int) []string {
	b, err := os.ReadFile(filepath.Join(proc, strconv.Itoa(pid), "stat"))
	if err != nil {
		return nil
	}
	i := bytes.LastIndexByte(b, ')')
	if i < 0 {
		return nil
	}
	return strings.Fields(string(b[i+1:]))
}

func ppid(proc string, pid int) (int, bool) {
	f := statFields(proc, pid)
	if len(f) < 2 {
		return 0, false
	}
	p, err := strconv.Atoi(f[1])
	return p, err == nil
}

// procUsage is a process's CPU jiffies (user + system) and resident pages.
func procUsage(proc string, pid int) (uint64, int64) {
	f := statFields(proc, pid)
	if len(f) < 22 {
		return 0, 0
	}
	ut, _ := strconv.ParseUint(f[11], 10, 64)
	st, _ := strconv.ParseUint(f[12], 10, 64)
	rss, _ := strconv.ParseInt(f[21], 10, 64)
	return ut + st, rss
}

func meminfo(proc string) map[string]int64 {
	out := map[string]int64{}
	fh, err := os.Open(filepath.Join(proc, "meminfo"))
	if err != nil {
		return out
	}
	defer fh.Close()
	sc := bufio.NewScanner(fh)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		f := strings.Fields(v)
		if len(f) > 0 {
			n, _ := strconv.ParseInt(f[0], 10, 64)
			out[k] = n * 1024 // kB
		}
	}
	return out
}

func memTotal(proc string) int64 { return meminfo(proc)["MemTotal"] }

func memUsed(proc string) int64 {
	m := meminfo(proc)
	if m["MemAvailable"] == 0 {
		return 0
	}
	return m["MemTotal"] - m["MemAvailable"]
}

func smi(ctx context.Context, bin string, query string) []string {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--query-gpu="+query, "--format=csv,noheader,nounits", "-i", "0").Output()
	if err != nil {
		return nil
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	f := strings.Split(line, ",")
	for i := range f {
		f[i] = strings.TrimSpace(f[i])
	}
	return f
}

func gpuInfo(ctx context.Context, bin string) (string, int64, bool) {
	f := smi(ctx, bin, "name,memory.total")
	if len(f) < 2 {
		return "", 0, false
	}
	mib, _ := strconv.ParseInt(f[1], 10, 64)
	return f[0], mib << 20, true
}

func gpuUse(ctx context.Context, bin string) (gpu, enc, dec float64) {
	f := smi(ctx, bin, "utilization.gpu,utilization.encoder,utilization.decoder")
	if len(f) < 3 {
		return -1, -1, -1
	}
	num := func(s string) float64 {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return -1
		}
		return v
	}
	return num(f[0]), num(f[1]), num(f[2])
}
