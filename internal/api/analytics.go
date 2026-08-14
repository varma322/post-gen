package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"post-gen/internal/models"
)

// handleEvents serves the Activity Log feed.
//
// Filters: level, source, account, type, since (RFC3339), q (free text),
// limit. Everything is applied in SQL; the handler only parses.
func (s server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}

	query := r.URL.Query()
	filter := models.EventFilter{
		Level:   strings.TrimSpace(query.Get("level")),
		Source:  strings.TrimSpace(query.Get("source")),
		Account: strings.TrimSpace(query.Get("account")),
		Type:    strings.TrimSpace(query.Get("type")),
		Search:  strings.TrimSpace(query.Get("q")),
	}

	if raw := query.Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			filter.Limit = parsed
		}
	}

	if raw := query.Get("since"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "invalid 'since' value: expected an RFC3339 timestamp such as 2026-08-14T10:00:00Z",
			})
			return
		}
		filter.Since = &parsed
	}

	events, err := s.engine.QueryEvents(r.Context(), filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"events": events, "count": len(events)})
}

// handleEventTrace serves every event for one pipeline run, powering the
// detail drawer's "view in context" action.
func (s server) handleEventTrace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}

	traceID := strings.TrimPrefix(r.URL.Path, "/events/")
	if traceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "trace id is required"})
		return
	}

	events, err := s.engine.EventsByTrace(r.Context(), traceID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if len(events) == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no events found for that trace"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"trace_id": traceID, "events": events})
}

// handleAnalyticsSummary serves the dashboard's single load-on-mount payload.
func (s server) handleAnalyticsSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}

	summary, err := s.engine.AnalyticsSummary(r.Context(), windowDays(r))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, summary)
}

// handleAnalyticsChannels serves per-account performance with daily series.
func (s server) handleAnalyticsChannels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}

	channels, err := s.engine.ChannelAnalytics(r.Context(), windowDays(r))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"channels": channels})
}

// handleWorkerStatus reports what the background publisher is doing.
func (s server) handleWorkerStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}

	writeJSON(w, http.StatusOK, s.engine.WorkerStatus())
}

// windowDays reads the ?days= window, leaving validation to the engine so the
// bound lives in one place.
func windowDays(r *http.Request) int {
	if raw := r.URL.Query().Get("days"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			return parsed
		}
	}
	return 0
}
