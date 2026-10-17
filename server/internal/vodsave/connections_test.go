package vodsave

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"marquee/internal/settings"
)

func TestConnectionBudget(t *testing.T) {
	live := []LiveUse{
		{Host: "dispatcharr.lan", Max: 2, InUse: 1, Recordings: 1},
		{Host: "DISPATCHARR.lan", Max: 1, InUse: 1},
		{Host: "other.lan", Max: 5, InUse: 3},
		{Host: "dispatcharr.lan", Max: 0, InUse: 4}, // unlimited: no limit, its use not counted
	}
	playing := []Playing{
		{Kind: "vod", SourceID: "xc", Host: "dispatcharr.lan"},
		{Kind: "vod", SourceID: "xc", Host: "dispatcharr.lan"}, // a second stream: a second connection
		{Kind: "vod", SourceID: "other", Host: "dispatcharr.lan"},
		{Kind: "vod", SourceID: "far", Host: "far.lan"},
		{Kind: "live", Host: "dispatcharr.lan"}, // counted through InUse
		{Kind: "library"},
	}
	b := ConnectionBudget("xc", "dispatcharr.lan", live, playing)
	if b.Limit != 3 || b.Live != 2 || b.Recordings != 1 || b.VOD != 3 || b.Used() != 5 || b.Free() != -2 || b.Viewers() != 4 {
		t.Fatalf("budget: %+v free %d", b, b.Free())
	}
	if b.allowed(true) != 0 || b.allowed(false) != 2 {
		t.Errorf("allowed %d / %d", b.allowed(true), b.allowed(false))
	}
	b = ConnectionBudget("xc", "dispatcharr.lan", live, nil)
	if b.Free() != 1 || b.allowed(true) != 1 {
		t.Errorf("no playback: %+v", b)
	}
	// The hard ceiling.
	if b := ConnectionBudget("x", "big.lan", []LiveUse{{Host: "big.lan", Max: 10}}, nil); b.allowed(true) != maxConnections {
		t.Errorf("ceiling: %d", b.allowed(true))
	}
	// No Live TV source on the host, or no host: no limit known.
	if b := ConnectionBudget("xc", "nowhere.lan", live, playing); b.Limit != 0 {
		t.Errorf("unknown host: %+v", b)
	}
	if b := ConnectionBudget("xc", "", live, playing); b.Limit != 0 {
		t.Errorf("no host: %+v", b)
	}
}

// liveState is a fake Live TV source on the VOD source's host.
type liveState struct {
	mu                 sync.Mutex
	max, inUse, record int
	host               string
}

func (l *liveState) set(inUse, recordings int) {
	l.mu.Lock()
	l.inUse, l.record = inUse, recordings
	l.mu.Unlock()
}

func (l *liveState) uses() []LiveUse {
	l.mu.Lock()
	defer l.mu.Unlock()
	return []LiveUse{{Host: l.host, Max: l.max, InUse: l.inUse, Recordings: l.record}}
}

func withLive(e *env, max int) *liveState {
	l := &liveState{max: max, host: HostOf(e.src.srv.URL)}
	e.s.Connections = l.uses
	return l
}

func (f *fakeSource) inFlightNow() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.inFlight
}

func (e *env) connections() int {
	e.s.mu.Lock()
	defer e.s.mu.Unlock()
	n := 0
	for _, j := range e.s.jobs {
		n += j.open()
	}
	return n
}

func threeFilms(t *testing.T, e *env) []int64 {
	t.Helper()
	data := clip(t, "mkv")
	var ids []int64
	for i, title := range []string{"A (2001)", "B (2002)", "C (2003)"} {
		e.src.files[fmt.Sprintf("movie/%d.mkv", i+1)] = data
		r, err := e.s.SaveMovie(context.Background(), e.movieID(title), 1, false)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, r.Saves[0].ID)
	}
	return ids
}

const threeFilmsJSON = `[{"name":"A (2001)","stream_id":"1","container_extension":"mkv"},{"name":"B (2002)","stream_id":"2","container_extension":"mkv"},
	{"name":"C (2003)","stream_id":"3","container_extension":"mkv"}]`

// With Maximum streams 2 and nothing watched, two saves run at once (concurrentPerSource
// is 1); a viewer takes one connection back at once, the queue says why, and the save comes
// back after the grace period.
func TestFreeConnectionsGrowAndShrink(t *testing.T) {
	e := newEnv(t, threeFilmsJSON, nil)
	l := withLive(e, 2)
	ids := threeFilms(t, e)
	e.src.delay = 15 * time.Millisecond
	e.s.SegmentMin = 1 << 40 // one connection per save here
	e.start()
	// The most connections open at once, as the downloads see them (the fake server's own
	// count can overlap a finished download by a moment).
	var most atomic.Int32
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(2 * time.Millisecond):
				if n := int32(e.connections()); n > most.Load() {
					most.Store(n)
				}
			}
		}
	}()
	e.waitFor("two saves at once", func() bool { return e.connections() == 2 })

	l.set(1, 0) // someone tunes a channel
	e.waitFor("a connection given back", func() bool { return e.connections() == 1 && e.src.inFlightNow() <= 1 })
	e.waitFor("the reason", func() bool {
		list, _ := e.s.List(context.Background())
		for _, v := range list {
			if v.State == Queued && v.PauseReason == "Waiting: connections in use by viewers" {
				return true
			}
		}
		return false
	})
	l.set(0, 0)
	stopped := time.Now()
	e.waitFor("the connection taken back", func() bool { return e.connections() == 2 })
	if d := time.Since(stopped); d < e.s.Grace-50*time.Millisecond {
		t.Errorf("taken back after %v, before the grace period", d)
	}
	for _, id := range ids {
		if v := e.wait(id, Done, Failed); v.State != Done {
			t.Fatalf("save: %+v", v)
		}
	}
	if n := most.Load(); n > 2 {
		t.Errorf("%d at once with 2 connections", n)
	}
}

// Without a known limit (no Live TV source on the host), concurrentPerSource decides.
func TestNoLimitUsesConcurrentPerSource(t *testing.T) {
	e := newEnv(t, threeFilmsJSON, nil)
	e.s.Connections = func() []LiveUse { return []LiveUse{{Host: "elsewhere.lan", Max: 4}} }
	ids := threeFilms(t, e)
	e.src.delay = 5 * time.Millisecond
	e.start()
	for _, id := range ids {
		e.wait(id, Done)
	}
	if e.src.maxIn != 1 {
		t.Errorf("%d at once, want 1", e.src.maxIn)
	}
}

// A large file with free connections is fetched in ranges over several at once; the file
// is byte for byte the source's, with its SHA-256.
func TestSegmentedDownload(t *testing.T) {
	data := clip(t, "mkv")
	e := newEnv(t, oneFilm, nil)
	e.src.files["movie/101.mkv"] = data
	e.src.delay = 10 * time.Millisecond
	withLive(e, 4)
	e.s.SegmentMin, e.s.SplitMin = 64<<10, 32<<10
	r, _ := e.s.SaveMovie(context.Background(), e.movieID("EN - Clip Film (2020)"), 1, false)
	e.start()
	conns := 0
	e.waitFor("several connections", func() bool {
		list, _ := e.s.List(context.Background())
		for _, v := range list {
			conns = max(conns, v.Connections)
		}
		return conns > 1 || e.save(r.Saves[0].ID).State == Done
	})
	v := e.wait(r.Saves[0].ID, Done, Failed)
	if v.State != Done || v.SHA256 != sha(data) {
		t.Fatalf("save: %+v", v)
	}
	got, _ := os.ReadFile(v.TargetPath)
	if sha(got) != sha(data) {
		t.Fatal("the file isn't the source's")
	}
	if conns < 2 || e.src.maxIn < 2 {
		t.Errorf("connections %d, at once %d", conns, e.src.maxIn)
	}
	bounded := 0
	for _, rg := range e.src.ranges {
		if strings.Count(rg, "-") == 1 && !strings.HasSuffix(rg, "-") {
			bounded++
		}
	}
	if e.src.ranges[0] != "bytes=0-" || bounded == 0 {
		t.Errorf("ranges: %q", e.src.ranges)
	}
	if _, err := os.Stat(segsPath(r.Saves[0].TargetPath + ".part")); !os.IsNotExist(err) {
		t.Error("the segment state is left")
	}
}

// A segmented download stopped part way continues each range from where it got to.
func TestSegmentedResume(t *testing.T) {
	data := clip(t, "mkv")
	n := int64(len(data))
	e := newEnv(t, oneFilm, nil)
	e.src.files["movie/101.mkv"] = data
	withLive(e, 1)
	e.s.SegmentMin, e.s.SplitMin = 64<<10, 1<<40
	r, _ := e.s.SaveMovie(context.Background(), e.movieID("EN - Clip Film (2020)"), 1, false)
	part := r.Saves[0].TargetPath + ".part"
	os.MkdirAll(r.Saves[0].TargetPath[:strings.LastIndex(r.Saves[0].TargetPath, "/")], 0o775)
	// Two ranges, each partly there; the rest of the file is zeros.
	half := n / 2
	buf := make([]byte, n)
	copy(buf[:n/4], data[:n/4])
	copy(buf[half:half+n/8], data[half:half+n/8])
	if err := os.WriteFile(part, buf, 0o664); err != nil {
		t.Fatal(err)
	}
	if err := saveSegs(part, &segFile{Total: n, Segments: []*segment{{Start: 0, End: half, Done: n / 4}, {Start: half, End: n, Done: n / 8}}}); err != nil {
		t.Fatal(err)
	}
	e.start()
	v := e.wait(r.Saves[0].ID, Done, Failed)
	if v.State != Done || v.SHA256 != sha(data) {
		t.Fatalf("save: %+v", v)
	}
	want := []string{fmt.Sprintf("bytes=%d-%d", n/4, half-1), fmt.Sprintf("bytes=%d-%d", half+n/8, n-1)}
	if strings.Join(e.src.ranges, ",") != strings.Join(want, ",") {
		t.Errorf("ranges %q, want %q", e.src.ranges, want)
	}
}

// A server without Range support: one connection, the whole file.
func TestSegmentedFallsBackWithoutRange(t *testing.T) {
	data := clip(t, "mkv")
	e := newEnv(t, oneFilm, nil)
	e.src.files["movie/101.mkv"] = data
	e.src.ignoreRange = true
	withLive(e, 4)
	e.s.SegmentMin, e.s.SplitMin = 64<<10, 32<<10
	r, _ := e.s.SaveMovie(context.Background(), e.movieID("EN - Clip Film (2020)"), 1, false)
	e.start()
	v := e.wait(r.Saves[0].ID, Done, Failed)
	if v.State != Done || v.SHA256 != sha(data) || len(e.src.ranges) != 1 || e.src.maxIn != 1 {
		t.Fatalf("save: %+v ranges %q", v, e.src.ranges)
	}
}

// A range that fails falls back to one connection, continuing from what's complete.
func TestSegmentErrorFallsBack(t *testing.T) {
	data := clip(t, "mkv")
	e := newEnv(t, oneFilm, nil)
	e.src.files["movie/101.mkv"] = data
	e.src.failBounded = true
	e.src.delay = 10 * time.Millisecond
	withLive(e, 4)
	e.s.SegmentMin, e.s.SplitMin = 64<<10, 32<<10
	r, _ := e.s.SaveMovie(context.Background(), e.movieID("EN - Clip Film (2020)"), 1, false)
	e.start()
	v := e.wait(r.Saves[0].ID, Done, Failed)
	if v.State != Done || v.SHA256 != sha(data) {
		t.Fatalf("save: %+v ranges %q", v, e.src.ranges)
	}
	if v.Attempts != 0 {
		t.Errorf("attempts %d: the fallback isn't a failure", v.Attempts)
	}
	got, _ := os.ReadFile(v.TargetPath)
	if sha(got) != sha(data) {
		t.Fatal("the file isn't the source's")
	}
}

// A recording starting takes its connection from the downloads before it connects, even
// with pausing while watching off.
func TestRecordingMakesSavesYield(t *testing.T) {
	for _, pause := range []bool{true, false} {
		t.Run(fmt.Sprint("pause=", pause), func(t *testing.T) {
			e := newEnv(t, threeFilmsJSON, func(v *settings.VODSave) { v.PauseWhileWatching = pause })
			l := withLive(e, 2)
			threeFilms(t, e)
			e.src.delay = 30 * time.Millisecond
			e.s.SegmentMin = 1 << 40
			e.start()
			e.waitFor("two saves at once", func() bool { return e.connections() == 2 })
			l.set(1, 1) // the recording is counted, then asks for room
			e.s.Yield(context.Background(), l.host, 0)
			if n := e.connections(); n > 1 {
				t.Fatalf("%d connections after the recording asked for one", n)
			}
		})
	}
}

// With pausing while watching off, viewers don't take connections from the downloads.
func TestPauseWhileWatchingOffNeverYields(t *testing.T) {
	e := newEnv(t, threeFilmsJSON, func(v *settings.VODSave) { v.PauseWhileWatching = false })
	l := withLive(e, 2)
	threeFilms(t, e)
	e.src.delay = 30 * time.Millisecond
	e.s.SegmentMin = 1 << 40
	e.start()
	e.waitFor("two saves at once", func() bool { return e.connections() == 2 })
	l.set(1, 0)
	e.playing.Store([]Playing{{Kind: "vod", SourceID: "xc", Host: l.host}})
	e.s.Yield(context.Background(), l.host, 1)
	time.Sleep(200 * time.Millisecond)
	if n := e.connections(); n != 2 {
		t.Fatalf("%d connections; viewers shouldn't take them", n)
	}
}

// VOD playback starting (not counted yet) takes a connection from the downloads.
func TestVODPlaybackYield(t *testing.T) {
	e := newEnv(t, threeFilmsJSON, nil)
	l := withLive(e, 2)
	threeFilms(t, e)
	e.src.delay = 30 * time.Millisecond
	e.s.SegmentMin = 1 << 40
	e.start()
	e.waitFor("two saves at once", func() bool { return e.connections() == 2 })
	e.s.Yield(context.Background(), l.host, 1)
	if n := e.connections(); n > 1 {
		t.Fatalf("%d connections after playback asked for one", n)
	}
	// Once it plays it's counted: still one.
	e.playing.Store([]Playing{{Kind: "vod", SourceID: "xc", Host: l.host}})
	time.Sleep(150 * time.Millisecond)
	if n := e.connections(); n > 1 {
		t.Fatalf("%d connections while it plays", n)
	}
}

// A viewer starting during a segmented download stops its newest ranges; they continue
// once the viewer stops, and the file is still the source's.
func TestSegmentedShrinksForViewers(t *testing.T) {
	data := clip(t, "mkv")
	e := newEnv(t, oneFilm, nil)
	e.src.files["movie/101.mkv"] = data
	e.src.delay = 20 * time.Millisecond
	l := withLive(e, 4)
	e.s.SegmentMin, e.s.SplitMin = 64<<10, 32<<10
	r, _ := e.s.SaveMovie(context.Background(), e.movieID("EN - Clip Film (2020)"), 1, false)
	e.start()
	e.waitFor("four connections", func() bool { return e.connections() == 4 })
	l.set(3, 0)
	e.waitFor("one connection left", func() bool { return e.connections() == 1 })
	if v := e.save(r.Saves[0].ID); v.State != Downloading {
		t.Fatalf("the save stopped: %+v", v)
	}
	l.set(0, 0)
	v := e.wait(r.Saves[0].ID, Done, Failed)
	got, _ := os.ReadFile(v.TargetPath)
	if v.State != Done || v.SHA256 != sha(data) || sha(got) != sha(data) {
		t.Fatalf("save: %+v", v)
	}
}
