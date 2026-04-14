package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"post-gen/internal/core"
	postgenWeb "post-gen/web"
)

// NewServer creates an HTTP handler exposing the generation API and web UI.
func NewServer(engine Generator) http.Handler {
	srv := server{engine: engine}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", srv.handleHealth)
	mux.HandleFunc("/accounts", srv.handleAccounts)
	mux.HandleFunc("/generate", srv.handleGenerate)
	mux.Handle("/", http.FileServer(http.FS(postgenWeb.FS)))
	return mux
}

func (s server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"accounts": s.engine.Accounts()})
}

func (s server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}

	defer r.Body.Close()

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	var req generateRequest
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return
	}

	urls := normalizeValues(req.URLs)
	if len(urls) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "at least one URL is required"})
		return
	}

	accounts := normalizeValues(req.Accounts)
	results, err := s.engine.GeneratePosts(urls, accounts)
	if err != nil {
		var accountErr core.AccountNotFoundError
		if errors.As(err, &accountErr) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate posts"})
		return
	}

	writeJSON(w, http.StatusOK, generateResponse{Results: results})
}

func normalizeValues(values []string) []string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			normalized = append(normalized, trimmed)
		}
	}
	return normalized
}

func methodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
