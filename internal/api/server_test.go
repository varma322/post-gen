package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"post-gen/internal/core"
	"post-gen/internal/models"
)

type stubGenerator struct {
	results  []core.Result
	err      error
	accounts []models.Account
}

func (s stubGenerator) GeneratePosts(urls []string, accountNames []string) ([]core.Result, error) {
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
