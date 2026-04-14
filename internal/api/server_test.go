package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"post-gen/internal/core"
	"post-gen/internal/models"
)

type stubGenerator struct {
	results      []core.Result
	err          error
	accounts     []models.Account
	generateFunc func(urls []string, accountNames []string) ([]core.Result, error)
}

func (s stubGenerator) GeneratePosts(urls []string, accountNames []string) ([]core.Result, error) {
	if s.generateFunc != nil {
		return s.generateFunc(urls, accountNames)
	}
	return s.results, s.err
}

func (s stubGenerator) Accounts() []models.Account {
	return s.accounts
}

func TestHandleGenerateReturnsResults(t *testing.T) {
	handler := NewServer(stubGenerator{
		results: []core.Result{{URL: "https://amazon.in/example", Account: "afficart", Output: "generated post"}},
	})

	body := bytes.NewBufferString(`{"urls":["https://amazon.in/example"],"accounts":["afficart"]}`)
	req := httptest.NewRequest(http.MethodPost, "/generate", body)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}

	var payload generateResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(payload.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(payload.Results))
	}
}

func TestHandleGenerateRejectsMalformedJSON(t *testing.T) {
	handler := NewServer(stubGenerator{})
	req := httptest.NewRequest(http.MethodPost, "/generate", bytes.NewBufferString(`{"urls":`))
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.Code)
	}
}

func TestHandleGenerateRejectsEmptyURLs(t *testing.T) {
	handler := NewServer(stubGenerator{})
	req := httptest.NewRequest(http.MethodPost, "/generate", bytes.NewBufferString(`{"urls":[],"accounts":["afficart"]}`))
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.Code)
	}
}

func TestHandleGenerateMapsAccountErrorsToBadRequest(t *testing.T) {
	handler := NewServer(stubGenerator{err: core.AccountNotFoundError{Name: "missing"}})
	req := httptest.NewRequest(http.MethodPost, "/generate", bytes.NewBufferString(`{"urls":["https://amazon.in/example"],"accounts":["missing"]}`))
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.Code)
	}
}

func TestHandleGenerateMapsUnexpectedErrorsToServerError(t *testing.T) {
	handler := NewServer(stubGenerator{err: errors.New("boom")})
	req := httptest.NewRequest(http.MethodPost, "/generate", bytes.NewBufferString(`{"urls":["https://amazon.in/example"]}`))
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", resp.Code)
	}
}

func TestHandleGenerateReturnsMixedBatchResults(t *testing.T) {
	handler := NewServer(stubGenerator{
		results: []core.Result{
			{URL: "https://amazon.in/ok", Account: "afficart", Output: "generated post"},
			{URL: "https://amazon.in/fail", Account: "afficart", Error: "failed to extract title"},
		},
	})

	body := bytes.NewBufferString(`{"urls":["https://amazon.in/ok","https://amazon.in/fail"],"accounts":["afficart"]}`)
	req := httptest.NewRequest(http.MethodPost, "/generate", body)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}

	var payload generateResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(payload.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(payload.Results))
	}

	if payload.Results[0].Error != "" {
		t.Fatalf("expected first result success, got error: %s", payload.Results[0].Error)
	}

	if payload.Results[1].Error == "" {
		t.Fatalf("expected second result to contain error")
	}
}

func TestHandleGenerateMethodNotAllowed(t *testing.T) {
	handler := NewServer(stubGenerator{})
	req := httptest.NewRequest(http.MethodGet, "/generate", nil)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", resp.Code)
	}

	if allow := resp.Header().Get("Allow"); allow != http.MethodPost {
		t.Fatalf("expected Allow header %q, got %q", http.MethodPost, allow)
	}
}

func TestHandleGenerateStreamRejectsMalformedJSON(t *testing.T) {
	handler := NewServer(stubGenerator{})
	req := httptest.NewRequest(http.MethodPost, "/generate/stream", bytes.NewBufferString(`{"urls":`))
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.Code)
	}
}

func TestHandleGenerateStreamReturnsEvents(t *testing.T) {
	handler := NewServer(stubGenerator{
		accounts: []models.Account{{Name: "afficart", TemplatePath: "templates/afficart.tmpl"}},
		generateFunc: func(urls []string, accountNames []string) ([]core.Result, error) {
			return []core.Result{{
				URL:     urls[0],
				Account: "afficart",
				Output:  "generated",
			}}, nil
		},
	})

	body := bytes.NewBufferString(`{"urls":["https://amazon.in/p1","https://amazon.in/p2"],"accounts":["afficart"]}`)
	req := httptest.NewRequest(http.MethodPost, "/generate/stream", body)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}

	contentType := resp.Header().Get("Content-Type")
	if !bytes.Contains([]byte(contentType), []byte("text/event-stream")) {
		t.Fatalf("unexpected content type: %s", contentType)
	}

	bodyText := resp.Body.String()
	if !strings.Contains(bodyText, "event: progress") {
		t.Fatalf("expected progress event, got %s", bodyText)
	}
	if !strings.Contains(bodyText, "event: result") {
		t.Fatalf("expected result event, got %s", bodyText)
	}
	if !strings.Contains(bodyText, "event: done") {
		t.Fatalf("expected done event, got %s", bodyText)
	}
}

func TestHandleAccountsReturnsConfiguredAccounts(t *testing.T) {
	handler := NewServer(stubGenerator{accounts: []models.Account{{Name: "afficart", TemplatePath: "templates/afficart.tmpl"}}})
	req := httptest.NewRequest(http.MethodGet, "/accounts", nil)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}

	var payload map[string][]models.Account
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(payload["accounts"]) != 1 {
		t.Fatalf("expected 1 account, got %d", len(payload["accounts"]))
	}
}

func TestHandleHomeReturnsHTML(t *testing.T) {
	handler := NewServer(stubGenerator{})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}

	contentType := resp.Header().Get("Content-Type")
	if !bytes.Contains([]byte(contentType), []byte("text/html")) {
		t.Fatalf("unexpected content type: %s", contentType)
	}

	if !bytes.Contains(resp.Body.Bytes(), []byte("PostGen Cloud MVP")) {
		t.Fatalf("expected home page content, got %s", resp.Body.String())
	}
}

func TestHandleHomeMissingFileReturns404(t *testing.T) {
	handler := NewServer(stubGenerator{})
	req := httptest.NewRequest(http.MethodGet, "/does-not-exist.txt", nil)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", resp.Code)
	}
}
