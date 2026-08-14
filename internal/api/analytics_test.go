package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"post-gen/internal/models"
)

func TestHandleEventsReturnsFeed(t *testing.T) {
	handler := NewServer(stubGenerator{
		storedEvents: []models.Event{
			{ID: 2, EventType: "POST_SUCCESS", Level: "SUCC", Source: "facebook", TraceID: "t1"},
			{ID: 1, EventType: "SCRAPE_SUCCESS", Level: "SUCC", Source: "amazon", TraceID: "t1"},
		},
	}, "")

	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/events?limit=10", nil))

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}

	var payload struct {
		Events []models.Event `json:"events"`
		Count  int            `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if payload.Count != 2 || len(payload.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", payload.Count)
	}
}

func TestHandleEventsRejectsBadSince(t *testing.T) {
	handler := NewServer(stubGenerator{}, "")

	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/events?since=yesterday", nil))

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an unparseable timestamp, got %d", resp.Code)
	}

	var payload map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&payload)
	// The message should tell the caller the expected format, not just "invalid".
	if payload["error"] == "" || !strings.Contains(payload["error"], "RFC3339") {
		t.Errorf("error should name the expected format, got %q", payload["error"])
	}
}

func TestHandleEventsAcceptsValidSince(t *testing.T) {
	handler := NewServer(stubGenerator{}, "")
	since := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)

	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/events?since="+since, nil))

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}
}

func TestHandleEventsMethodNotAllowed(t *testing.T) {
	handler := NewServer(stubGenerator{}, "")

	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/events", nil))

	if resp.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", resp.Code)
	}
}

func TestHandleEventTraceReturnsChain(t *testing.T) {
	handler := NewServer(stubGenerator{
		storedEvents: []models.Event{
			{ID: 1, EventType: "SCRAPE_STARTED", TraceID: "abc-123"},
			{ID: 2, EventType: "SCRAPE_SUCCESS", TraceID: "abc-123"},
		},
	}, "")

	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/events/abc-123", nil))

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}

	var payload struct {
		TraceID string         `json:"trace_id"`
		Events  []models.Event `json:"events"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if payload.TraceID != "abc-123" {
		t.Errorf("trace_id = %q", payload.TraceID)
	}
	if len(payload.Events) != 2 {
		t.Errorf("expected 2 events in the chain, got %d", len(payload.Events))
	}
}

func TestHandleEventTraceNotFound(t *testing.T) {
	// An unknown trace is a 404, not an empty 200 - the drawer needs to tell
	// the difference between "no such run" and "a run with no events".
	handler := NewServer(stubGenerator{}, "")

	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/events/does-not-exist", nil))

	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.Code)
	}
}

func TestHandleAnalyticsSummaryPassesWindow(t *testing.T) {
	handler := NewServer(stubGenerator{}, "")

	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/analytics/summary?days=30", nil))

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}

	var summary models.AnalyticsSummary
	if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if summary.Days != 30 {
		t.Errorf("Days = %d, want the requested 30", summary.Days)
	}
}

func TestHandleAnalyticsSummaryDefaultsWindow(t *testing.T) {
	handler := NewServer(stubGenerator{}, "")

	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/analytics/summary", nil))

	var summary models.AnalyticsSummary
	_ = json.NewDecoder(resp.Body).Decode(&summary)

	// 0 means "unset" to the handler; the engine applies the real default.
	if summary.Days != 0 {
		t.Errorf("Days = %d, want the unset sentinel passed through", summary.Days)
	}
}

func TestHandleWorkerStatus(t *testing.T) {
	handler := NewServer(stubGenerator{}, "")

	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/worker/status", nil))

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}

	var status models.WorkerStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if !status.Running || status.Phase != "idle" {
		t.Errorf("unexpected status: %+v", status)
	}
}

func TestAnalyticsRoutesRequireAuth(t *testing.T) {
	// These carry publish history and page ids, so they must sit behind the
	// same bearer check as everything else.
	handler := NewServer(stubGenerator{}, "secret-token")

	for _, path := range []string{"/events", "/events/abc", "/analytics/summary", "/analytics/channels", "/worker/status"} {
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, path, nil))

		if resp.Code != http.StatusUnauthorized {
			t.Errorf("%s returned %d without a token, want 401", path, resp.Code)
		}
	}
}
