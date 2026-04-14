package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"post-gen/internal/core"
	"post-gen/internal/models"
	postgenWeb "post-gen/web"
)

// NewServer creates an HTTP handler exposing the generation API and web UI.
func NewServer(engine Generator) http.Handler {
	srv := server{engine: engine}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", srv.handleHealth)
	mux.HandleFunc("/accounts", srv.handleAccounts)
	mux.HandleFunc("/generate", srv.handleGenerate)
	mux.HandleFunc("/generate/stream", srv.handleGenerateStream)
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

func (s server) handleGenerateStream(w http.ResponseWriter, r *http.Request) {
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
	if err := validateRequestedAccounts(s.engine.Accounts(), accounts); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming unsupported"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	totalResults := 0
	successCount := 0
	failureCount := 0
	totalURLs := len(urls)

	for index, rawURL := range urls {
		if err := writeSSE(w, "progress", streamProgressPayload{Current: index + 1, Total: totalURLs, URL: rawURL}); err != nil {
			return
		}
		flusher.Flush()

		results, err := s.engine.GeneratePosts([]string{rawURL}, accounts)
		if err != nil {
			if writeErr := writeSSE(w, "error", map[string]string{"error": "failed to generate posts"}); writeErr != nil {
				return
			}
			flusher.Flush()
			continue
		}

		for _, result := range results {
			totalResults++
			if result.Error == "" {
				successCount++
			} else {
				failureCount++
			}

			if err := writeSSE(w, "result", streamResultPayload{Result: result}); err != nil {
				return
			}
			flusher.Flush()
		}
	}

	_ = writeSSE(w, "done", streamDonePayload{TotalResults: totalResults, Success: successCount, Failed: failureCount})
	flusher.Flush()
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

func validateRequestedAccounts(available []models.Account, requested []string) error {
	if len(requested) == 0 {
		return nil
	}

	lookup := make(map[string]struct{}, len(available))
	for _, account := range available {
		lookup[account.Name] = struct{}{}
	}

	for _, name := range requested {
		if _, exists := lookup[name]; !exists {
			return core.AccountNotFoundError{Name: name}
		}
	}

	return nil
}

func writeSSE(w http.ResponseWriter, event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}

	return nil
}
