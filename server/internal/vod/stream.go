package vod

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Films stream through the server: the source's HLS is passed on as it is (no transcode),
// with every playlist rewritten so its links come back here. Players then need nothing but
// the server (no cross-origin requests to the source's CDN, and it works away from home),
// and seeking is the player's own, since the source's playlists are complete.

// StreamPrefix is where sessions are served. The session id is the credential.
const StreamPrefix = "/api/v1/vod-stream/"

// idleAfter ends a session nobody has fetched from (paused films keep theirs for a while).
const idleAfter = 3 * time.Hour

type Streams struct {
	HTTP *http.Client
	// OnEnd, when set, hears a session that ended (stopped or idle), e.g. to stop the
	// transcode behind an Xtream Codes film.
	OnEnd func(*Stream)
	// Active, when set, keeps a session the server makes the HLS of while that is in use
	// (its segments aren't fetched from here).
	Active func(*Stream) bool

	mu       sync.Mutex
	sessions map[string]*Stream
}

// Stream is one viewer watching one film.
type Stream struct {
	ID      string
	UserID  int64
	MovieID int64
	// EpisodeID is set (and MovieID 0) when watching a series' episode.
	EpisodeID int64
	userAgent string
	master    string
	// PlaybackID is the server's own HLS session (playback.Manager) for a film that is a
	// file (Xtream Codes); "" when the source's HLS is passed through.
	PlaybackID string

	mu    sync.Mutex
	urls  []string
	index map[string]int
	last  time.Time
}

func newID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (st *Streams) client() *http.Client {
	if st.HTTP != nil {
		return st.HTTP
	}
	return &http.Client{Timeout: 0} // segments stream; the request context bounds them
}

// Start opens a session for a film's master playlist.
func (st *Streams) Start(userID, movieID int64, masterURL, userAgent string) *Stream {
	s := &Stream{ID: newID(), UserID: userID, MovieID: movieID, userAgent: userAgent, master: masterURL,
		index: map[string]int{}, last: time.Now()}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.sessions == nil {
		st.sessions = map[string]*Stream{}
	}
	st.sessions[s.ID] = s
	return s
}

func (st *Streams) Get(id string) *Stream {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.sessions[id]
}

// StartEpisodePlayback records an episode's session whose HLS the server makes itself.
func (st *Streams) StartEpisodePlayback(userID, episodeID int64, playbackID string) *Stream {
	s := st.Start(userID, 0, "", "")
	s.EpisodeID, s.PlaybackID = episodeID, playbackID
	return s
}

// StartPlayback records a session whose HLS the server makes itself (playbackID).
func (st *Streams) StartPlayback(userID, movieID int64, playbackID string) *Stream {
	s := st.Start(userID, movieID, "", "")
	s.PlaybackID = playbackID
	return s
}

func (st *Streams) Stop(id string) {
	st.mu.Lock()
	s := st.sessions[id]
	delete(st.sessions, id)
	st.mu.Unlock()
	if s != nil && st.OnEnd != nil {
		st.OnEnd(s)
	}
}

// Touch keeps a session alive (its player reported its position).
func (s *Stream) Touch() {
	s.mu.Lock()
	s.last = time.Now()
	s.mu.Unlock()
}

// Run ends idle sessions until ctx ends.
func (st *Streams) Run(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		st.mu.Lock()
		var ended []*Stream
		for id, s := range st.sessions {
			s.mu.Lock()
			idle := time.Since(s.last) > idleAfter
			s.mu.Unlock()
			if idle && s.PlaybackID != "" && st.Active != nil && st.Active(s) {
				idle = false
			}
			if idle {
				delete(st.sessions, id)
				ended = append(ended, s)
			}
		}
		st.mu.Unlock()
		for _, s := range ended {
			if st.OnEnd != nil {
				st.OnEnd(s)
			}
		}
	}
}

// ref returns the local name for an upstream link, adding it to the session once.
func (s *Stream) ref(abs string) string {
	n, ok := s.index[abs]
	if !ok {
		n = len(s.urls)
		s.urls = append(s.urls, abs)
		s.index[abs] = n
	}
	return strconv.Itoa(n) + linkExt(abs)
}

var safeExt = regexp.MustCompile(`^\.[a-z0-9]{1,5}$`)

func linkExt(abs string) string {
	u, err := url.Parse(abs)
	if err != nil {
		return ".bin"
	}
	ext := strings.ToLower(path.Ext(u.Path))
	if !safeExt.MatchString(ext) {
		return ".bin"
	}
	return ext
}

var uriAttr = regexp.MustCompile(`URI="([^"]+)"`)

// rewrite points every link in a playlist (segments, variants, renditions, keys) at the
// session. base is the playlist's own URL, after redirects.
func (s *Stream) rewrite(body []byte, base *url.URL) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	resolve := func(ref string) string {
		u, err := base.Parse(strings.TrimSpace(ref))
		if err != nil {
			return ref
		}
		return s.ref(u.String())
	}
	lines := strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n")
	for i, line := range lines {
		t := strings.TrimSpace(line)
		switch {
		case t == "":
		case !strings.HasPrefix(t, "#"):
			lines[i] = resolve(t)
		case strings.Contains(t, `URI="`):
			lines[i] = uriAttr.ReplaceAllStringFunc(t, func(m string) string {
				return `URI="` + resolve(uriAttr.FindStringSubmatch(m)[1]) + `"`
			})
		}
	}
	return []byte(strings.Join(lines, "\n"))
}

// Handler serves StreamPrefix<session>/master.m3u8 and the links it rewrites.
func (st *Streams) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only reads go to the source: the request's method is passed on.
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id, name, ok := strings.Cut(strings.TrimPrefix(r.URL.Path, StreamPrefix), "/")
		s := st.Get(id)
		if !ok || s == nil || s.master == "" {
			http.NotFound(w, r)
			return
		}
		s.mu.Lock()
		s.last = time.Now()
		target := ""
		if name == "master.m3u8" {
			target = s.master
		} else if n, err := strconv.Atoi(strings.TrimSuffix(name, path.Ext(name))); err == nil && n >= 0 && n < len(s.urls) {
			target = s.urls[n]
		}
		s.mu.Unlock()
		if target == "" {
			http.NotFound(w, r)
			return
		}
		st.proxy(w, r, s, target, name)
	})
}

func (st *Streams) proxy(w http.ResponseWriter, r *http.Request, s *Stream, target, name string) {
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target, nil)
	if err != nil {
		http.Error(w, "bad link", http.StatusBadGateway)
		return
	}
	if s.userAgent != "" {
		req.Header.Set("User-Agent", s.userAgent)
	}
	if rg := r.Header.Get("Range"); rg != "" {
		req.Header.Set("Range", rg)
	}
	resp, err := st.client().Do(req)
	if err != nil {
		if r.Context().Err() == nil {
			slog.Warn("movie stream", "err", err)
		}
		http.Error(w, "the source didn't answer", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		http.Error(w, "the source answered "+resp.Status, http.StatusBadGateway)
		return
	}
	ct := resp.Header.Get("Content-Type")
	if strings.HasSuffix(name, ".m3u8") || strings.Contains(strings.ToLower(ct), "mpegurl") {
		body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		if err != nil {
			http.Error(w, "the source's playlist didn't load", http.StatusBadGateway)
			return
		}
		out := s.rewrite(body, resp.Request.URL)
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Length", strconv.Itoa(len(out)))
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			w.Write(out)
		}
		return
	}
	for _, h := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Cache-Control"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if r.Method != http.MethodHead {
		io.Copy(w, resp.Body)
	}
}
