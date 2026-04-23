package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestHandleTemplatesListReturnsTemplateInfo(t *testing.T) {
	templatesDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(templatesDir, "afficart.tmpl"), []byte("{{.Title}}"), 0644); err != nil {
		t.Fatalf("failed to write template: %v", err)
	}

	handler := newServer(stubGenerator{accounts: []models.Account{{
		Name:         "afficart",
		TemplatePath: "templates/afficart.tmpl",
	}}}, templatesDir)

	req := httptest.NewRequest(http.MethodGet, "/templates", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}

	var payload templatesResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(payload.Templates) != 1 {
		t.Fatalf("expected 1 template, got %d", len(payload.Templates))
	}

	if payload.Templates[0].Name != "afficart.tmpl" {
		t.Fatalf("expected template name afficart.tmpl, got %s", payload.Templates[0].Name)
	}

	if len(payload.Templates[0].Accounts) != 1 || payload.Templates[0].Accounts[0] != "afficart" {
		t.Fatalf("expected account usage to include afficart, got %#v", payload.Templates[0].Accounts)
	}
}

func TestHandleTemplateGetReturnsContent(t *testing.T) {
	templatesDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(templatesDir, "sample.tmpl"), []byte("hello {{.Title}}"), 0644); err != nil {
		t.Fatalf("failed to write template: %v", err)
	}

	handler := newServer(stubGenerator{}, templatesDir)
	req := httptest.NewRequest(http.MethodGet, "/templates/sample.tmpl", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}

	var payload templateResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if payload.Name != "sample.tmpl" {
		t.Fatalf("expected template name sample.tmpl, got %s", payload.Name)
	}

	if payload.Content != "hello {{.Title}}" {
		t.Fatalf("unexpected template content: %s", payload.Content)
	}
}

func TestHandleTemplatePutSavesAndCreatesBackup(t *testing.T) {
	templatesDir := t.TempDir()
	templatePath := filepath.Join(templatesDir, "sample.tmpl")
	if err := os.WriteFile(templatePath, []byte("old content"), 0644); err != nil {
		t.Fatalf("failed to write original template: %v", err)
	}

	handler := newServer(stubGenerator{}, templatesDir)
	body := bytes.NewBufferString(`{"content":"new {{.Title}}"}`)
	req := httptest.NewRequest(http.MethodPut, "/templates/sample.tmpl", body)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}

	data, err := os.ReadFile(templatePath)
	if err != nil {
		t.Fatalf("failed reading updated template: %v", err)
	}
	if string(data) != "new {{.Title}}" {
		t.Fatalf("expected updated template content, got %s", string(data))
	}

	files, err := os.ReadDir(templatesDir)
	if err != nil {
		t.Fatalf("failed to read template dir: %v", err)
	}

	backupFound := false
	for _, f := range files {
		if strings.HasPrefix(f.Name(), "sample.tmpl.bak-") {
			backupFound = true
			break
		}
	}

	if !backupFound {
		t.Fatal("expected backup file to be created")
	}
}

func TestHandleTemplatePutRejectsInvalidTemplateSyntax(t *testing.T) {
	templatesDir := t.TempDir()
	handler := newServer(stubGenerator{}, templatesDir)
	body := bytes.NewBufferString(`{"content":"{{if .Title}}"}`)
	req := httptest.NewRequest(http.MethodPut, "/templates/sample.tmpl", body)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.Code)
	}
}

func TestHandleTemplateRejectsPathTraversal(t *testing.T) {
	handler := newServer(stubGenerator{}, t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/templates/%2e%2e%2fsecrets.tmpl", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.Code)
	}
}
