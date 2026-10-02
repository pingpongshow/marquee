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
	"marquee/internal/web"
)

type Deps struct {
	Handlers   *api.Handlers
	Stream     http.Handler // media URLs under /api/v1/stream/
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
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "unknown API endpoint")
	})
	mux.Handle("/", web.Handler(d.WebDir))

	return recoverer(logRequests(withIdentity(d, mux)))
}

// withIdentity classifies the network and resolves the bearer token (if any) into a session.
// Endpoints decide for themselves whether a session is required.
func withIdentity(d Deps, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
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
