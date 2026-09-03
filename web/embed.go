package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed index.html app.js styles.css favicon.svg
var FS embed.FS

// SPAHandler serves the React single-page application, routing all non-file requests to index.html.
// This enables client-side routing in React without 404s on page refresh or direct navigation.
func SPAHandler(staticFS fs.FS) http.Handler {
	fsys, _ := fs.Sub(staticFS, ".")
	return &spaFileServer{http.FileServer(http.FS(fsys))}
}

type spaFileServer struct {
	handler http.Handler
}

// ServeHTTP implements http.Handler, serving files or falling back to index.html for SPA routing.
func (s *spaFileServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		s.handler.ServeHTTP(w, r)
		return
	}

	// Try to serve the requested file if it exists in the embed FS
	if _, err := fs.Stat(FS, path); err == nil {
		s.handler.ServeHTTP(w, r)
		return
	}

	// File doesn't exist; check if it's a route (no extension or .html) and serve index.html
	ext := getExtension(r.URL.Path)
	if ext == "" || ext == ".html" {
		data, err := fs.ReadFile(FS, "index.html")
		if err == nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data)
			return
		}
	}

	// Unknown static file -> 404
	w.WriteHeader(http.StatusNotFound)
}

func getExtension(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			break
		}
		if path[i] == '.' {
			return path[i:]
		}
	}
	return ""
}
