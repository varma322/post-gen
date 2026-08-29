package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"post-gen/internal/events"
	"strings"
	"testing"
	"time"

	"post-gen/internal/core"
	"post-gen/internal/deals"
	"post-gen/internal/models"
)

type stubGenerator struct {
	results      []core.Result
	err          error
	accounts     []models.Account
	generateFunc func(urls []string, accountNames []string) ([]core.Result, error)
	storedEvents []models.Event
	eventsErr    error

	deals          []models.Deal
	dealsErr       error
	discoverResult *deals.Result
	discoverErr    error
}

func (s stubGenerator) GeneratePosts(ctx context.Context, urls []string, accountNames []string) ([]core.Result, error) {
	if s.generateFunc != nil {
		return s.generateFunc(urls, accountNames)
	}
	return s.results, s.err
}

func (s stubGenerator) GeneratePostsWithPublish(ctx context.Context, urls []string, accountNames []string, publish bool, delayBetweenPosts time.Duration, onCooldown func(time.Duration)) ([]core.Result, error) {
	if s.generateFunc != nil {
		return s.generateFunc(urls, accountNames)
	}
	return s.results, s.err
}

func (s stubGenerator) PublishPost(accountName, postText string) (string, error) {
	return "mock_publish_id", nil
}

func (s stubGenerator) Accounts() []models.Account {
	return s.accounts
}

func (s stubGenerator) ReloadAccounts() error {
	return nil
}

func (s stubGenerator) SaveAccounts(_ []models.Account) error {
	return nil
}

func (s stubGenerator) DeleteAccount(_ string) error {
	return nil
}

func (s stubGenerator) Paths() core.Paths {
	return core.Paths{}
}

func (s stubGenerator) AddQueuedProduct(ctx context.Context, url string) error {
	return nil
}

func (s stubGenerator) GetQueuedProducts(ctx context.Context) ([]models.QueuedProduct, error) {
	return nil, nil
}

func (s stubGenerator) DeleteQueuedProduct(ctx context.Context, id int) error {
	return nil
}

func (s stubGenerator) AddAccountLink(ctx context.Context, accountName, url string) error {
	return nil
}

func (s stubGenerator) GetAccountLinks(ctx context.Context, accountName string) ([]models.AccountLink, error) {
	return nil, nil
}

func (s stubGenerator) DeleteAccountLink(ctx context.Context, id int) error {
	return nil
}

func (s stubGenerator) TriggerAutoPostJob(ctx context.Context, rotateOldLinks bool) (int, error) {
	return 0, nil
}

// Events returns a nil Logger, which is a valid no-op: Emit, Dropped, and
// Close all handle a nil receiver, so handlers need no guard around it.
func (s stubGenerator) Events() *events.Logger {
	return nil
}

func (s stubGenerator) QueryEvents(ctx context.Context, filter models.EventFilter) ([]models.Event, error) {
	return s.storedEvents, s.eventsErr
}

func (s stubGenerator) EventsByTrace(ctx context.Context, traceID string) ([]models.Event, error) {
	return s.storedEvents, s.eventsErr
}

func (s stubGenerator) AnalyticsSummary(ctx context.Context, days int) (*models.AnalyticsSummary, error) {
	return &models.AnalyticsSummary{Days: days}, nil
}

func (s stubGenerator) ChannelAnalytics(ctx context.Context, days int) ([]models.ChannelStats, error) {
	return nil, nil
}

func (s stubGenerator) Settings(ctx context.Context) (*models.SettingsView, error) {
	return &models.SettingsView{Sources: map[string]string{}}, nil
}

func (s stubGenerator) SaveSettings(ctx context.Context, update models.SettingsUpdate) error {
	return nil
}

func (s stubGenerator) Schedules(ctx context.Context) ([]models.JobSchedule, error) {
	return nil, nil
}

func (s stubGenerator) CreateSchedule(ctx context.Context, schedule models.JobSchedule) (*models.JobSchedule, error) {
	return &schedule, nil
}

func (s stubGenerator) UpdateSchedule(ctx context.Context, schedule models.JobSchedule) (*models.JobSchedule, error) {
	return &schedule, nil
}

func (s stubGenerator) DeleteSchedule(ctx context.Context, id int) error {
	return nil
}

func (s stubGenerator) RunSchedule(ctx context.Context, id int) (int, error) {
	return 1, nil
}

func (s stubGenerator) Deals(ctx context.Context, filter models.DealFilter) ([]models.Deal, error) {
	return s.deals, s.dealsErr
}

func (s stubGenerator) Deal(ctx context.Context, asin string) (*models.Deal, error) {
	if s.dealsErr != nil {
		return nil, s.dealsErr
	}
	for i := range s.deals {
		if s.deals[i].ASIN == asin {
			return &s.deals[i], nil
		}
	}
	return nil, nil
}

func (s stubGenerator) SetDealStatus(ctx context.Context, asin, status string) (bool, error) {
	if s.dealsErr != nil {
		return false, s.dealsErr
	}
	for _, deal := range s.deals {
		if deal.ASIN == asin {
			return true, nil
		}
	}
	return false, nil
}

func (s stubGenerator) QueueDeal(ctx context.Context, asin string) (*models.Deal, error) {
	if s.dealsErr != nil {
		return nil, s.dealsErr
	}
	for i := range s.deals {
		if s.deals[i].ASIN == asin {
			queued := s.deals[i]
			queued.Status = models.DealQueued
			return &queued, nil
		}
	}
	return nil, nil
}

func (s stubGenerator) DiscoverDeals(ctx context.Context) (*deals.Result, error) {
	if s.discoverErr != nil {
		return s.discoverResult, s.discoverErr
	}
	if s.discoverResult != nil {
		return s.discoverResult, nil
	}
	return &deals.Result{ByProvider: map[string]int{}}, nil
}

func (s stubGenerator) DealAnalytics(ctx context.Context) (*models.DealAnalytics, error) {
	if s.dealsErr != nil {
		return nil, s.dealsErr
	}
	byStatus := map[string]int{}
	byProvider := map[string]int{}
	for _, deal := range s.deals {
		byStatus[deal.Status]++
		byProvider[deal.Provider]++
	}
	return &models.DealAnalytics{
		Total: len(s.deals), ByStatus: byStatus, ByProvider: byProvider,
		ProviderShare: map[string]float64{}, TopCategories: nil,
	}, nil
}

func (s stubGenerator) RescoreDeals(ctx context.Context) (int, error) {
	if s.dealsErr != nil {
		return 0, s.dealsErr
	}
	return len(s.deals), nil
}

func (s stubGenerator) WorkerStatus() models.WorkerStatus {
	return models.WorkerStatus{Running: true, Phase: "idle"}
}

func (s stubGenerator) GetActiveJob(ctx context.Context) (*models.PublicationJob, error) {
	return nil, nil
}

func (s stubGenerator) CancelActiveJobs(ctx context.Context) error {
	return nil
}

func (s stubGenerator) GetStats(ctx context.Context, limit int) (*models.Stats, error) {
	return &models.Stats{}, nil
}


func TestHandleGenerateReturnsResults(t *testing.T) {
	handler := NewServer(stubGenerator{
		results: []core.Result{{URL: "https://amazon.in/example", Account: "afficart", Output: "generated post"}},
	}, "")

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
	handler := NewServer(stubGenerator{}, "")
	req := httptest.NewRequest(http.MethodPost, "/generate", bytes.NewBufferString(`{"urls":`))
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.Code)
	}
}

func TestHandleGenerateRejectsEmptyURLs(t *testing.T) {
	handler := NewServer(stubGenerator{}, "")
	req := httptest.NewRequest(http.MethodPost, "/generate", bytes.NewBufferString(`{"urls":[],"accounts":["afficart"]}`))
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.Code)
	}
}

func TestHandleGenerateMapsAccountErrorsToBadRequest(t *testing.T) {
	handler := NewServer(stubGenerator{err: core.AccountNotFoundError{Name: "missing"}}, "")
	req := httptest.NewRequest(http.MethodPost, "/generate", bytes.NewBufferString(`{"urls":["https://amazon.in/example"],"accounts":["missing"]}`))
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.Code)
	}
}

func TestHandleGenerateMapsUnexpectedErrorsToServerError(t *testing.T) {
	handler := NewServer(stubGenerator{err: errors.New("boom")}, "")
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
	}, "")

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
	handler := NewServer(stubGenerator{}, "")
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
	handler := NewServer(stubGenerator{}, "")
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
	}, "")

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
	handler := NewServer(stubGenerator{accounts: []models.Account{{Name: "afficart", TemplatePath: "templates/afficart.tmpl"}}}, "")
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
	handler := NewServer(stubGenerator{}, "")
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

	if !bytes.Contains(resp.Body.Bytes(), []byte("postgen-ui")) {
		t.Fatalf("expected home page content, got %s", resp.Body.String())
	}
}

func TestHandleHomeMissingFileReturns404(t *testing.T) {
	handler := NewServer(stubGenerator{}, "")
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
	}}}, templatesDir, "")

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

	handler := newServer(stubGenerator{}, templatesDir, "")
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

	handler := newServer(stubGenerator{}, templatesDir, "")
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
	handler := newServer(stubGenerator{}, templatesDir, "")
	body := bytes.NewBufferString(`{"content":"{{if .Title}}"}`)
	req := httptest.NewRequest(http.MethodPut, "/templates/sample.tmpl", body)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.Code)
	}
}

func TestHandleTemplateRejectsPathTraversal(t *testing.T) {
	handler := newServer(stubGenerator{}, t.TempDir(), "")
	req := httptest.NewRequest(http.MethodGet, "/templates/%2e%2e%2fsecrets.tmpl", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.Code)
	}
}

// --- Auth-specific tests ---

func TestBearerTokenMiddlewareBlocksUnauthenticatedRequests(t *testing.T) {
	handler := NewServer(stubGenerator{}, "secret-token")
	req := httptest.NewRequest(http.MethodGet, "/accounts", nil)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", resp.Code)
	}
}

func TestBearerTokenMiddlewareAllowsCorrectToken(t *testing.T) {
	handler := NewServer(stubGenerator{accounts: []models.Account{{Name: "afficart"}}}, "secret-token")
	req := httptest.NewRequest(http.MethodGet, "/accounts", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
}

func TestBearerTokenMiddlewareRejectsWrongToken(t *testing.T) {
	handler := NewServer(stubGenerator{}, "secret-token")
	req := httptest.NewRequest(http.MethodGet, "/accounts", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", resp.Code)
	}
}

func TestBearerTokenMiddlewareSkipsHealthEndpoint(t *testing.T) {
	handler := NewServer(stubGenerator{}, "secret-token")
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected /health to be accessible without token, got %d", resp.Code)
	}
}

func TestBearerTokenMiddlewareDisabledWhenTokenEmpty(t *testing.T) {
	handler := NewServer(stubGenerator{accounts: []models.Account{{Name: "afficart"}}}, "")
	req := httptest.NewRequest(http.MethodGet, "/accounts", nil)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected auth disabled (no token), got %d", resp.Code)
	}
}

func TestHandleGenerateLink(t *testing.T) {
	handler := NewServer(stubGenerator{}, "")

	// 1. Basic URL + explicit Tag request
	body := bytes.NewBufferString(`{"url":"https://www.amazon.in/dp/B0D1234567","tag":"customtag-21"}`)
	req := httptest.NewRequest(http.MethodPost, "/generate/link", body)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}

	var payload affiliateLinkResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if payload.AffiliateURL != "https://www.amazon.in/dp/B0D1234567?th=1&tag=customtag-21" {
		t.Fatalf("unexpected affiliate URL output: %s", payload.AffiliateURL)
	}

	// 2. Full URL with nested tag, extracting automatically
	body2 := bytes.NewBufferString(`{"url":"https://www.amazon.in/Stanley-70-964E-Combination-Spanner-12-Pieces/dp/B00ICIKIW2?tag=smartbuy016-21"}`)
	req2 := httptest.NewRequest(http.MethodPost, "/generate/link", body2)
	resp2 := httptest.NewRecorder()

	handler.ServeHTTP(resp2, req2)

	if resp2.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp2.Code)
	}

	var payload2 affiliateLinkResponse
	if err := json.NewDecoder(resp2.Body).Decode(&payload2); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if payload2.AffiliateURL != "https://www.amazon.in/dp/B00ICIKIW2?th=1&tag=smartbuy016-21" {
		t.Fatalf("unexpected affiliate URL extracted: %s", payload2.AffiliateURL)
	}

	// 3. Missing tag and no default env var, expecting 400 Bad Request
	body3 := bytes.NewBufferString(`{"url":"https://www.amazon.in/dp/B0D1234567"}`)
	req3 := httptest.NewRequest(http.MethodPost, "/generate/link", body3)
	resp3 := httptest.NewRecorder()

	os.Setenv("DEFAULT_AFFILIATE_TAG", "")

	handler.ServeHTTP(resp3, req3)

	if resp3.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp3.Code)
	}
}


// capturingGenerator records what SaveAccounts was handed, so a handler's
// defaulting can be asserted on the value that would actually be persisted.
type capturingGenerator struct {
	stubGenerator
	saved *[]models.Account
}

func (c capturingGenerator) SaveAccounts(accounts []models.Account) error {
	*c.saved = accounts
	return nil
}

// TestCreateAccountDefaultsUseAIOn is the regression that silently shipped raw
// Amazon copy: handleCreateAccount never set UseAI, so every account added
// through the UI got Go's zero value and skipped enrichment entirely.
func TestCreateAccountDefaultsUseAIOn(t *testing.T) {
	var saved []models.Account
	handler := NewServer(capturingGenerator{saved: &saved}, "")

	body := `{"name":"newpage","template_path":"templates/afficart.tmpl","affiliate_tag":"tag-21",
		"facebook_page_id":"1","facebook_access_token":"t","max_posts_per_day":0,
		"active_hours_start":"","active_hours_end":"","min_delay_minutes":0}`
	req := httptest.NewRequest(http.MethodPost, "/accounts", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", resp.Code, resp.Body.String())
	}
	if len(saved) != 1 {
		t.Fatalf("expected 1 saved account, got %d", len(saved))
	}
	if !saved[0].UseAI {
		t.Errorf("UseAI = false for a new account; AI enrichment must be on by default")
	}
}

func TestCreateAccountHonoursExplicitUseAIFalse(t *testing.T) {
	var saved []models.Account
	handler := NewServer(capturingGenerator{saved: &saved}, "")

	body := `{"name":"rawpage","template_path":"templates/afficart.tmpl","use_ai":false,
		"affiliate_tag":"","facebook_page_id":"","facebook_access_token":"",
		"max_posts_per_day":0,"active_hours_start":"","active_hours_end":"","min_delay_minutes":0}`
	req := httptest.NewRequest(http.MethodPost, "/accounts", strings.NewReader(body))
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", resp.Code, resp.Body.String())
	}
	if saved[0].UseAI {
		t.Errorf("UseAI = true despite an explicit opt-out")
	}
}

// TestUpdateAccountPreservesUseAIWhenAbsent covers the quick active/inactive
// toggle, which resends a partial account body.
func TestUpdateAccountPreservesUseAIWhenAbsent(t *testing.T) {
	var saved []models.Account
	existing := []models.Account{{Name: "afficart", TemplatePath: "templates/afficart.tmpl", UseAI: true}}
	handler := NewServer(capturingGenerator{stubGenerator: stubGenerator{accounts: existing}, saved: &saved}, "")

	body := `{"name":"afficart","template_path":"templates/afficart.tmpl","affiliate_tag":"",
		"facebook_page_id":"","facebook_access_token":"","max_posts_per_day":0,
		"active_hours_start":"","active_hours_end":"","min_delay_minutes":0}`
	req := httptest.NewRequest(http.MethodPut, "/accounts/afficart", strings.NewReader(body))
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	if !saved[0].UseAI {
		t.Errorf("UseAI was reset to false by an update that never mentioned it")
	}
}
