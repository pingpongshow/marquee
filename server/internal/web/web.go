// Package web serves the React client: embedded in release builds, or from disk when
// MARQUEE_WEB_DIR is set (development). Unknown paths fall back to index.html so
// client-side routes work on reload.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

//go:embed all:dist
var embedded embed.FS

func Handler(dir string) http.Handler {
	var files fs.FS
	if dir != "" {
		files = os.DirFS(dir)
	} else {
		files, _ = fs.Sub(embedded, "dist")
	}
	fileServer := http.FileServerFS(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(files, p); err != nil {
			if path.Ext(p) != "" { // a missing asset is a real 404, not a client route
				http.NotFound(w, r)
				return
			}
			r = r.Clone(r.Context())
			r.URL.Path = "/"
			p = "index.html"
		}
		if strings.HasPrefix(p, "assets/") {
			// Vite emits content-hashed asset names, so they can be cached forever.
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		if _, err := fs.Stat(files, "index.html"); err != nil && p == "index.html" {
			http.Error(w, "web client not built", http.StatusNotFound)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}
