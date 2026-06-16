package api

import (
	"net/http"
	"strings"
)

// LoggingMiddleware is a placeholder for future request logging and observability.
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}

// BearerTokenMiddleware enforces Bearer token authentication on all routes
// except those in the provided skip list (e.g. /health).
// If the configured token is empty, the middleware is a no-op (auth disabled).
func BearerTokenMiddleware(token string, skip []string, next http.Handler) http.Handler {
	skipSet := make(map[string]struct{}, len(skip))
	for _, path := range skip {
		skipSet[path] = struct{}{}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Auth is disabled when no token is configured
		if token == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Allow configured skip paths (e.g. /health)
		if _, skipped := skipSet[r.URL.Path]; skipped {
			next.ServeHTTP(w, r)
			return
		}

		// Extract token from "Authorization: Bearer <token>" header
		authHeader := r.Header.Get("Authorization")
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] != token {
			w.Header().Set("WWW-Authenticate", `Bearer realm="postgen"`)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized: valid Bearer token required"})
			return
		}

		next.ServeHTTP(w, r)
	})
}

// MaxBytesMiddleware limits the size of the request body to prevent unbounded memory usage.
func MaxBytesMiddleware(limit int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		next.ServeHTTP(w, r)
	})
}
