// Package server wires the HTTP stack: middleware, API routes and the web client.
package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"marquee/internal/api"
	"marquee/internal/auth"
	"marquee/internal/netclass"
	"marquee/internal/vod"
	"marquee/internal/web"
)

type Deps struct {
	Handlers   *api.Handlers
	Stream     http.Handler // media URLs under /api/v1/stream/
	Downloads  http.Handler // offline download files under /api/v1/download/
	Live       http.Handler // live TV streams under /api/v1/live/
	VOD        http.Handler // movies on demand under /api/v1/vod-stream/ (LIVE-6)
	Auth       *auth.Service
	Classifier *netclass.Classifier
	WebDir     string
}

func New(d Deps) http.Handler {
	strict := api.NewStrictHandlerWithOptions(d.Handlers, nil, api.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		},
	})
	mux := http.NewServeMux()
	api.HandlerWithOptions(strict, api.StdHTTPServerOptions{
		BaseURL:    "/api/v1",
		BaseRouter: mux,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		},
	})
	if d.Stream != nil {
		mux.Handle("/api/v1/stream/", d.Stream)
	}
	if d.Downloads != nil {
		mux.Handle("/api/v1/download/", d.Downloads)
	}
	if d.Live != nil {
		mux.Handle("/api/v1/live/", d.Live)
	}
	if d.VOD != nil {
		mux.Handle(vod.StreamPrefix, d.VOD)
	}
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "unknown API endpoint")
	})
	mux.Handle("/", web.Handler(d.WebDir))

	return recoverer(logRequests(withIdentity(d, mux)))
}

// maxBodyBytes bounds API request bodies (JSON, and avatar uploads of up to 10 MB), so a
// client, signed in or not, can't make the server read an endless body into memory.
const maxBodyBytes = 16 << 20

// withIdentity classifies the network and resolves the bearer token (if any) into a session.
// Endpoints decide for themselves whether a session is required.
func withIdentity(d Deps, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil && strings.HasPrefix(r.URL.Path, "/api/") {
			r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		}
		ip := netclass.ClientIP(r)
		ctx := api.WithRequestInfo(r.Context(), api.RequestInfo{ClientIP: ip.String(), Class: d.Classifier.Classify(r)})
		if tok := auth.BearerToken(r); tok != "" {
			s, ok, err := d.Auth.ResolveToken(ctx, tok, ip.String())
			if err != nil {
				slog.ErrorContext(ctx, "resolve token", "err", err)
				writeError(w, http.StatusInternalServerError, "internal", "internal server error")
				return
			}
			if !ok {
				writeError(w, http.StatusUnauthorized, "invalid_token", "session expired or revoked")
				return
			}
			ctx = auth.WithSession(ctx, s)
		} else if key := r.URL.Query().Get("key"); key != "" && auth.IsImageRequest(r) {
			// Image URLs carry an image key, not the sign-in token (D85).
			s, ok := d.Auth.ResolveImageKey(ctx, key)
			if !ok {
				writeError(w, http.StatusUnauthorized, "invalid_key", "image key not valid")
				return
			}
			ctx = auth.WithSession(ctx, s)
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		// The web app must not be framed by other sites (clickjacking), and runs only its own
		// scripts (see contentSecurityPolicy).
		w.Header().Set("X-Frame-Options", "DENY")
		if w.Header().Get("Content-Security-Policy") == "" {
			w.Header().Set("Content-Security-Policy", contentSecurityPolicy)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			return
		}
		lvl := slog.LevelDebug
		if sw.status >= 500 {
			lvl = slog.LevelError
		}
		slog.Log(r.Context(), lvl, "http", "method", r.Method, "path", r.URL.Path,
			"status", sw.status, "dur", time.Since(start).Round(time.Microsecond), "ip", netclass.ClientIP(r).String())
	})
}

func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil && v != http.ErrAbortHandler {
				slog.Error("panic", "err", v, "stack", string(debug.Stack()))
				writeError(w, http.StatusInternalServerError, "internal", "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(api.Error{Code: code, Message: msg})
}

// contentSecurityPolicy limits what the web app's pages may load and run, so that even if
// something slipped injected markup into a page (a crafted title, a subtitle), the browser
// would refuse to run it:
//   - scripts and workers only from Marquee itself (the bundle has no inline scripts), plus
//     WebAssembly for styled subtitles (JASSUB) and blob: workers (hls.js);
//   - images from anywhere (posters in Fix match, Discover and Requests come straight from
//     TMDB and other providers; images can't run code);
//   - video and audio from Marquee and blob: (hls.js feeds the player through Media Source);
//   - frames only for trailers on YouTube; no plugins; forms and <base> only to Marquee;
//   - and no framing by other sites.
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self' 'wasm-unsafe-eval'; " +
	"worker-src 'self' blob:; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob: https: http:; " +
	"media-src 'self' blob: data:; " +
	"font-src 'self' data: blob:; " +
	"connect-src 'self'; " +
	"frame-src https://www.youtube-nocookie.com https://www.youtube.com; " +
	"object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"
