package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed index.html app.js styles.css favicon.svg icons.svg
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
	// Try to serve the requested file
	_, err := fs.Stat(FS, r.URL.Path[1:]) // Remove leading /
	if err == nil {
		// File exists, serve it normally
		s.handler.ServeHTTP(w, r)
		return
	}

	// File doesn't exist; check if it's a known static extension
	// If it's not (e.g., /app/dashboard instead of /index.html), fall back to SPA index
	ext := getExtension(r.URL.Path)
	if ext == "" || ext == ".html" {
		// No extension or .html → likely a route; serve index.html for client-side routing
		r.URL.Path = "/index.html"
		s.handler.ServeHTTP(w, r)
		return
	}

	// Unknown file (e.g., .js without .map, .css not found) → 404
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
