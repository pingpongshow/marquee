package vodsave

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"marquee/internal/db"
	"marquee/internal/library"
	"marquee/internal/settings"
	"marquee/internal/vod"
)

const xcPass = "p@ss/w0rd&x"

// clips are small real videos made once with FFmpeg: 12 s of 1280×720 H.264 with stereo
// AAC, as MKV and as MPEG-TS.
var clips struct {
	once     sync.Once
	mkv, ts  []byte
	err      error
	duration time.Duration
}

func clip(t *testing.T, ext string) []byte {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	clips.once.Do(func() {
		dir, err := os.MkdirTemp("", "vodsave-clips")
		if err != nil {
			clips.err = err
			return
		}
		defer os.RemoveAll(dir)
		for _, e := range []string{"mkv", "ts"} {
			out := filepath.Join(dir, "clip."+e)
			cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=1280x720:rate=24:duration=12",
				"-f", "lavfi", "-i", "sine=frequency=440:duration=12", "-c:v", "libx264", "-preset", "ultrafast", "-g", "24",
				"-c:a", "aac", "-ac", "2", "-shortest", out)
			if b, err := cmd.CombinedOutput(); err != nil {
				clips.err = fmt.Errorf("%v: %s", err, b)
				return
			}
			b, _ := os.ReadFile(out)
			if e == "mkv" {
				clips.mkv = b
			} else {
				clips.ts = b
			}
		}
	})
	if clips.err != nil {
		t.Fatal(clips.err)
	}
	if ext == "ts" {
		return clips.ts
	}
	return clips.mkv
}

// fakeSource is an Xtream Codes server whose files are served with Range support, and can
// misbehave: drop connections, ignore Range, answer 5xx, go down, trickle.
type fakeSource struct {
	t        *testing.T
	mu       sync.Mutex
	files    map[string][]byte // "movie/101.mkv" -> bytes
	streams  string
	series   string
	info     map[string]string
	ranges   []string // Range headers asked for, in order
	order    []string // files asked for, in order
	inFlight int
	maxIn    int
	// behaviours
	dropAfter       int64         // the next response ends after this many bytes (once)
	ignoreRange     bool          // always the whole file
	failBounded     bool          // a Range with an end ("bytes=a-b") answers 500
	fileStatus      int           // file requests answer this (0 = normal)
	statusAfterDrop int           // fileStatus becomes this once a response was dropped
	down            atomic.Bool   // everything answers 503
	delay           time.Duration // per 64 KiB chunk
	srv             *httptest.Server
}

func newFakeSource(t *testing.T) *fakeSource {
	f := &fakeSource{t: t, files: map[string][]byte{}, info: map[string]string{}, streams: "[]", series: "[]"}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeSource) serve(w http.ResponseWriter, r *http.Request) {
	if f.down.Load() {
		http.Error(w, "down", http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()
	if r.URL.Path == "/player_api.php" {
		if q.Get("username") != "marquee" || q.Get("password") != xcPass {
			io.WriteString(w, `{"user_info":{"auth":0}}`)
			return
		}
		f.mu.Lock()
		streams, series, info := f.streams, f.series, f.info[q.Get("series_id")]
		f.mu.Unlock()
		switch q.Get("action") {
		case "":
			io.WriteString(w, `{"user_info":{"auth":1,"status":"Active"}}`)
		case "get_vod_categories", "get_series_categories":
			io.WriteString(w, `[{"category_id":"1","category_name":"All"}]`)
		case "get_vod_streams":
			io.WriteString(w, streams)
		case "get_series":
			io.WriteString(w, series)
		case "get_series_info":
			io.WriteString(w, info)
		case "get_vod_info":
			f.mu.Lock()
			vi := f.info["vod:"+q.Get("vod_id")]
			f.mu.Unlock()
			if vi == "" {
				vi = `{"info":[]}`
			}
			io.WriteString(w, vi)
		default:
			io.WriteString(w, `[]`)
		}
		return
	}
	// /movie/{user}/{pass}/{file} or /series/...
	parts := strings.Split(strings.TrimPrefix(r.URL.EscapedPath(), "/"), "/")
	for i, p := range parts {
		parts[i], _ = url.PathUnescape(p)
	}
	if len(parts) != 4 || parts[1] != "marquee" || parts[2] != xcPass {
		http.NotFound(w, r)
		return
	}
	key := parts[0] + "/" + parts[3]
	f.mu.Lock()
	data, ok := f.files[key]
	status := f.fileStatus
	f.ranges = append(f.ranges, r.Header.Get("Range"))
	f.order = append(f.order, parts[3])
	drop := f.dropAfter
	f.dropAfter = 0
	if drop > 0 && f.statusAfterDrop != 0 {
		f.fileStatus = f.statusAfterDrop
	}
	ignore := f.ignoreRange
	failBounded := f.failBounded
	f.inFlight++
	f.maxIn = max(f.maxIn, f.inFlight)
	delay := f.delay
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.inFlight--
		f.mu.Unlock()
	}()
	if status != 0 {
		http.Error(w, "nope", status)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	start, end := int64(0), int64(len(data))
	if rg := r.Header.Get("Range"); rg != "" && !ignore {
		from, to, bounded := strings.Cut(strings.TrimPrefix(rg, "bytes="), "-")
		start, _ = strconv.ParseInt(from, 10, 64)
		if bounded && to != "" {
			if failBounded {
				http.Error(w, "nope", http.StatusInternalServerError)
				return
			}
			e, _ := strconv.ParseInt(to, 10, 64)
			end = min(e+1, end)
		}
		if start >= int64(len(data)) {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end-1, len(data)))
		w.Header().Set("Content-Length", strconv.Itoa(int(end-start)))
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(http.StatusOK)
	}
	body := data[start:end]
	sent := int64(0)
	for len(body) > 0 {
		n := min(len(body), 64<<10)
		if drop > 0 && sent+int64(n) > drop {
			n = int(drop - sent)
			w.Write(body[:n])
			if fl, ok := w.(http.Flusher); ok {
				fl.Flush()
			}
			// End the connection mid-body.
			if hj, ok := w.(http.Hijacker); ok {
				c, _, err := hj.Hijack()
				if err == nil {
					c.Close()
				}
			}
			return
		}
		if _, err := w.Write(body[:n]); err != nil {
			return
		}
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
		sent += int64(n)
		body = body[n:]
		if delay > 0 {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(delay):
			}
		}
	}
}

type env struct {
	t       *testing.T
	s       *Service
	vod     *vod.Service
	src     *fakeSource
	root    string
	movies  string
	shows   string
	db      interface{}
	scans   []int64
	scanMu  sync.Mutex
	logs    *syncBuffer
	failed  []Save
	drained []string
	cancel  context.CancelFunc
	playing atomic.Value // []Playing
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

// newEnv makes a database with one Xtream Codes source (the fake) holding films (and
// series) and a movies and a shows library; the queue isn't running until start.
func newEnv(t *testing.T, films string, mutate func(*settings.VODSave)) *env {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	e := &env{t: t, root: dir, movies: filepath.Join(dir, "media", "Movies"), shows: filepath.Join(dir, "media", "Shows"), logs: &syncBuffer{}}
	os.MkdirAll(e.movies, 0o755)
	os.MkdirAll(e.shows, 0o755)
	e.src = newFakeSource(t)
	e.src.streams = films
	store, _ := settings.Open(ctx, d)
	if _, err := store.Update(ctx, func(s *settings.Settings) error {
		s.Integrations.VODEnabled = true
		s.Integrations.VODSources = []settings.VODSource{{ID: "xc", Name: "Dispatcharr", Kind: settings.VODKindXtream,
			URL: e.src.srv.URL, Username: "marquee", Password: xcPass, Enabled: true}}
		s.VODSave.MoviesFolder, s.VODSave.ShowsFolder = e.movies, e.shows
		if mutate != nil {
			mutate(&s.VODSave)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	libs := library.NewStore(d)
	libs.Create(ctx, "Movies", library.Movies, []string{e.movies}, library.Options{})
	libs.Create(ctx, "Shows", library.Shows, []string{e.shows}, library.Options{})
	res, _ := d.Exec(`INSERT INTO users (username, display_name, is_admin) VALUES ('admin', 'Admin', 1)`)
	_ = res
	e.vod = &vod.Service{DB: d, Settings: store, Streams: &vod.Streams{}, HTTP: &http.Client{Timeout: 10 * time.Second}}
	e.vod.Refresh(ctx, false)
	e.playing.Store([]Playing(nil))
	e.s = &Service{DB: d, Settings: store, VOD: e.vod, FFmpeg: "ffmpeg", FFprobe: "ffprobe",
		Libraries: libs.List,
		Scan: func(id int64) {
			e.scanMu.Lock()
			e.scans = append(e.scans, id)
			e.scanMu.Unlock()
		},
		Activity:       func(context.Context) []Playing { return e.playing.Load().([]Playing) },
		OnFailed:       func(s Save) { e.failed = append(e.failed, s) },
		OnDrained:      func(done, failed int, titles []string) { e.drained = append(e.drained, fmt.Sprint(done, failed)) },
		Tick:           20 * time.Millisecond,
		Backoff:        []time.Duration{30 * time.Millisecond, 30 * time.Millisecond},
		Grace:          300 * time.Millisecond,
		SourceDownWait: 100 * time.Millisecond,
	}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return e
}

func (e *env) start() {
	ctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	done := make(chan struct{})
	go func() { e.s.Run(ctx); close(done) }()
	e.t.Cleanup(func() {
		cancel()
		<-done
		time.Sleep(50 * time.Millisecond) // the jobs' last writes
	})
}

func (e *env) movieID(title string) int64 {
	e.t.Helper()
	var id int64
	if err := e.s.DB.QueryRow("SELECT id FROM vod_movies WHERE title=?", title).Scan(&id); err != nil {
		e.t.Fatalf("film %q: %v", title, err)
	}
	return id
}

func (e *env) save(id int64) Save {
	e.t.Helper()
	v, err := e.s.one(context.Background(), id)
	if err != nil {
		e.t.Fatal(err)
	}
	return v
}

// wait waits for a save to reach a state.
func (e *env) wait(id int64, states ...string) Save {
	e.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		v := e.save(id)
		for _, s := range states {
			if v.State == s {
				return v
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	v := e.save(id)
	e.t.Fatalf("save %d is %s (%s), not %v", id, v.State, v.Error, states)
	return v
}

func (e *env) waitFor(what string, ok func() bool) {
	e.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	e.t.Fatalf("timed out waiting for %s", what)
}

func sha(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// noSecrets checks the password is nowhere: the queue's rows, the logs.
func (e *env) noSecrets() {
	e.t.Helper()
	rows, _ := e.s.DB.Query("SELECT * FROM vod_saves")
	cols, _ := rows.Columns()
	var dump strings.Builder
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		rows.Scan(ptrs...)
		fmt.Fprint(&dump, vals...)
	}
	rows.Close()
	list, _ := e.s.List(context.Background())
	fmt.Fprintf(&dump, "%+v", list)
	for _, secret := range []string{xcPass, url.PathEscape(xcPass), url.QueryEscape(xcPass)} {
		if strings.Contains(dump.String(), secret) {
			e.t.Fatalf("the password is in the queue: %s", dump.String())
		}
		if strings.Contains(e.logs.String(), secret) {
			e.t.Fatalf("the password is in the logs: %s", e.logs.String())
		}
	}
}

const oneFilm = `[{"name":"EN - Clip Film (2020)","stream_id":"101","container_extension":"mkv","category_id":"1"}]`
