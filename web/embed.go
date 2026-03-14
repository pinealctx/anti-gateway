package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static/*
var staticFS embed.FS

// Handler returns an http.Handler that serves the embedded SPA.
// All unknown paths fall back to index.html for client-side routing.
func Handler() http.Handler {
	sub, _ := fs.Sub(staticFS, "static")
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to serve static files first
		path := r.URL.Path
		if path == "/" || path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(sub, path); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}
		// SPA fallback: serve index.html for unmatched paths
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
