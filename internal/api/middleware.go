package api

import "net/http"

// LoggingMiddleware is a placeholder for future request logging and observability.
// Wire it in NewServer once structured logging is added.
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}
