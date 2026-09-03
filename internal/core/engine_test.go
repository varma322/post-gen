package core

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"post-gen/internal/config"
	"post-gen/internal/models"
	"post-gen/internal/publisher"
	"post-gen/internal/scraper"
)

type stubScraper struct {
	product *models.Product
	err     error
}

func (s stubScraper) Scrape(ctx context.Context, url string) (*models.Product, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.product != nil && s.product.DealPrice == "" {
		s.product.DealPrice = "1,000"
	}
	return s.product, nil
}

func TestGeneratePostsRejectsUnknownAccount(t *testing.T) {
	engine := Engine{
		accounts: []models.Account{{Name: "afficart", TemplatePath: "templates/afficart.tmpl"}},
	}

	_, err := engine.GeneratePosts(context.Background(), []string{"https://amazon.in/example"}, []string{"missing"})
	if err == nil {
		t.Fatal("expected unknown account error")
	}

	if !strings.Contains(err.Error(), "account \"missing\" not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGeneratePostsReturnsInvalidURLResult(t *testing.T) {
	engine := Engine{
		accounts: []models.Account{{Name: "afficart", TemplatePath: "templates/afficart.tmpl"}},
	}

	results, err := engine.GeneratePosts(context.Background(), []string{"not-a-url"}, []string{"afficart"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].Error == "" || !strings.Contains(results[0].Error, "invalid URL format") {
		t.Fatalf("expected invalid URL error, got %#v", results[0])
	}
}

func TestGeneratePostsReturnsUnsupportedPlatformResult(t *testing.T) {
	engine := Engine{
		accounts:       []models.Account{{Name: "afficart", TemplatePath: "templates/afficart.tmpl"}},
		selectors:      config.Selectors{},
		scraperFactory: scraper.GetScraper,
		postGenerator: func(product models.Product, path string) (string, error) {
			return "", nil
		},
	}

	results, err := engine.GeneratePosts(context.Background(), []string{"https://example.com/product"}, []string{"afficart"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if !strings.Contains(results[0].Error, "unsupported platform") {
		t.Fatalf("expected unsupported platform error, got %#v", results[0])
	}
}

func TestGeneratePostsReturnsRenderedOutputForEachAccount(t *testing.T) {
	product := &models.Product{Title: "Example Product", Link: "https://amazon.in/example"}
	engine := Engine{
		accounts: []models.Account{
			{Name: "afficart", TemplatePath: "templates/afficart.tmpl"},
			{Name: "smartbuy", TemplatePath: "templates/smartbuy.tmpl"},
		},
		selectors: config.Selectors{},
		scraperFactory: func(url string, sel config.Selectors) (scraper.Scraper, error) {
			return stubScraper{product: product}, nil
		},
		postGenerator: func(product models.Product, path string) (string, error) {
			return product.Title + " => " + path, nil
		},
	}

	results, err := engine.GeneratePosts(context.Background(), []string{"https://amazon.in/example"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	for _, result := range results {
		if result.Error != "" {
			t.Fatalf("expected success result, got %#v", result)
		}
		if result.Output == "" {
			t.Fatalf("expected rendered output, got %#v", result)
		}
		if result.Product.Tagline != defaultTagline {
			t.Fatalf("expected default tagline enrichment, got %#v", result.Product)
		}
		if result.Product.Hashtags != defaultHashtags {
			t.Fatalf("expected default hashtags enrichment, got %#v", result.Product)
		}
	}
}

func TestGeneratePostsReturnsGenerationErrorPerAccount(t *testing.T) {
	engine := Engine{
		accounts:  []models.Account{{Name: "afficart", TemplatePath: "templates/afficart.tmpl"}},
		selectors: config.Selectors{},
		scraperFactory: func(url string, sel config.Selectors) (scraper.Scraper, error) {
			return stubScraper{product: &models.Product{Title: "Example Product", Link: url}}, nil
		},
		postGenerator: func(product models.Product, path string) (string, error) {
			return "", errors.New("template parse failed")
		},
	}

	results, err := engine.GeneratePosts(context.Background(), []string{"https://amazon.in/example"}, []string{"afficart"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if !strings.Contains(results[0].Error, "generating post for afficart") {
		t.Fatalf("expected generation error, got %#v", results[0])
	}
}

func TestGeneratePostsKeepsFullAmazonURLBeforeScrape(t *testing.T) {
	var capturedURL string

	engine := Engine{
		accounts:  []models.Account{{Name: "afficart", TemplatePath: "templates/afficart.tmpl"}},
		selectors: config.Selectors{},
		scraperFactory: func(url string, sel config.Selectors) (scraper.Scraper, error) {
			capturedURL = url
			return stubScraper{product: &models.Product{Title: "Example Product", Link: url}}, nil
		},
		postGenerator: func(product models.Product, path string) (string, error) {
			return "ok", nil
		},
	}

	messy := "https://www.amazon.in/Some-Title/dp/B0F7QR75X2?tag=abc&ref=something"
	results, err := engine.GeneratePosts(context.Background(), []string{messy}, []string{"afficart"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	expected := messy
	if capturedURL != expected {
		t.Fatalf("expected full URL %q, got %q", expected, capturedURL)
	}

	if results[0].URL != expected {
		t.Fatalf("expected result URL %q, got %q", expected, results[0].URL)
	}
}

func TestGeneratePostsKeepsShortLinkUnchanged(t *testing.T) {
	engine := Engine{
		accounts:       []models.Account{{Name: "afficart", TemplatePath: "templates/afficart.tmpl"}},
		selectors:      config.Selectors{},
		scraperFactory: scraper.GetScraper,
		postGenerator: func(product models.Product, path string) (string, error) {
			return "", nil
		},
	}

	shortURL := "https://amzn.in/d/xyz123"
	results, err := engine.GeneratePosts(context.Background(), []string{shortURL}, []string{"afficart"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].URL != shortURL {
		t.Fatalf("expected unchanged short URL %q, got %q", shortURL, results[0].URL)
	}
}

func TestGeneratePostsInjectsAffiliateTagPerAccount(t *testing.T) {
	var generatedProduct models.Product

	engine := Engine{
		accounts: []models.Account{{
			Name:         "zonerush",
			TemplatePath: "templates/zonerush.tmpl",
			AffiliateTag: "zonrushdeals-21",
		}},
		selectors: config.Selectors{},
		scraperFactory: func(url string, sel config.Selectors) (scraper.Scraper, error) {
			return stubScraper{product: &models.Product{Title: "Example Product", Link: url}}, nil
		},
		postGenerator: func(product models.Product, path string) (string, error) {
			generatedProduct = product
			return "ok", nil
		},
	}

	_, err := engine.GeneratePosts(context.Background(), []string{"https://www.amazon.in/dp/B0F7QR75X2"}, []string{"zonerush"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "https://www.amazon.in/dp/B0F7QR75X2?th=1&tag=zonrushdeals-21"
	if generatedProduct.Link != want {
		t.Fatalf("expected injected affiliate tag URL %q, got %q", want, generatedProduct.Link)
	}
}

func TestGeneratePostsKeepsURLWhenAffiliateTagEmpty(t *testing.T) {
	var generatedProduct models.Product

	engine := Engine{
		accounts: []models.Account{{
			Name:         "afficart",
			TemplatePath: "templates/afficart.tmpl",
		}},
		selectors: config.Selectors{},
		scraperFactory: func(url string, sel config.Selectors) (scraper.Scraper, error) {
			return stubScraper{product: &models.Product{Title: "Example Product", Link: url}}, nil
		},
		postGenerator: func(product models.Product, path string) (string, error) {
			generatedProduct = product
			return "ok", nil
		},
	}

	_, err := engine.GeneratePosts(context.Background(), []string{"https://www.amazon.in/dp/B0F7QR75X2"}, []string{"afficart"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "https://www.amazon.in/dp/B0F7QR75X2"
	if generatedProduct.Link != want {
		t.Fatalf("expected unchanged URL %q, got %q", want, generatedProduct.Link)
	}
}

func TestGeneratePostsWithPublishSendsPost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"fb_post_id"}`))
	}))
	defer server.Close()

	fbPub := publisher.NewFacebookPublisher()
	fbPub.BaseURL = server.URL

	engine := Engine{
		accounts: []models.Account{{
			Name:                "afficart",
			TemplatePath:        "templates/afficart.tmpl",
			FacebookPageID:      "123",
			FacebookAccessToken: "token",
		}},
		selectors: config.Selectors{},
		scraperFactory: func(url string, sel config.Selectors) (scraper.Scraper, error) {
			return stubScraper{product: &models.Product{Title: "Example", Link: url}}, nil
		},
		postGenerator: func(product models.Product, path string) (string, error) {
			return "rendered output", nil
		},
		fbPublisher: fbPub,
	}

	results, err := engine.GeneratePostsWithPublish(context.Background(), []string{"https://amazon.in/dp/B0F7QR75X2"}, []string{"afficart"}, true, 0, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].PublishID != "fb_post_id" {
		t.Fatalf("expected publish ID 'fb_post_id', got '%s'", results[0].PublishID)
	}

	if results[0].PublishError != "" {
		t.Fatalf("unexpected publish error: %s", results[0].PublishError)
	}
}

func TestGeneratePostsSkipsOutOfStock(t *testing.T) {
	engine := Engine{
		accounts: []models.Account{
			{Name: "afficart", TemplatePath: "templates/afficart.tmpl"},
		},
		selectors: config.Selectors{},
		scraperFactory: func(url string, sel config.Selectors) (scraper.Scraper, error) {
			// Stub scraper returns a product marked as Out of stock
			return stubScraper{product: &models.Product{
				Title:     "Out Of Stock Item",
				Link:      url,
				DealPrice: "Out of stock",
			}}, nil
		},
		postGenerator: func(product models.Product, path string) (string, error) {
			return "rendered output", nil
		},
	}

	results, err := engine.GeneratePosts(context.Background(), []string{"https://amazon.in/dp/B08JW4QBSL"}, []string{"afficart"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].Error == "" {
		t.Fatal("expected an error remark for out of stock product, got none")
	}

	if !strings.Contains(results[0].Error, "out of stock") {
		t.Fatalf("expected out of stock remark error, got '%s'", results[0].Error)
	}

	if results[0].Output != "" {
		t.Fatalf("expected no generated output, got '%s'", results[0].Output)
	}
}

func TestQueueAndAutoPostRequiresDatabase(t *testing.T) {
	engine := Engine{db: nil}

	err := engine.AddQueuedProduct(context.Background(), "https://amazon.in/dp/B0D1234567")
	if err == nil || !strings.Contains(err.Error(), "database required") {
		t.Fatalf("expected database required error, got %v", err)
	}

	_, err = engine.GetQueuedProducts(context.Background())
	if err == nil || !strings.Contains(err.Error(), "database required") {
		t.Fatalf("expected database required error, got %v", err)
	}

	err = engine.DeleteQueuedProduct(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "database required") {
		t.Fatalf("expected database required error, got %v", err)
	}

	_, err = engine.TriggerAutoPostJob(context.Background(), false)
	if err == nil || !strings.Contains(err.Error(), "database required") {
		t.Fatalf("expected database required error, got %v", err)
	}

	_, err = engine.GetActiveJob(context.Background())
	if err == nil || !strings.Contains(err.Error(), "database required") {
		t.Fatalf("expected database required error, got %v", err)
	}

	err = engine.CancelActiveJobs(context.Background())
	if err == nil || !strings.Contains(err.Error(), "database required") {
		t.Fatalf("expected database required error, got %v", err)
	}

	err = engine.AddAccountLink(context.Background(), "afficart", "https://amazon.in/dp/B0D1234567")
	if err == nil || !strings.Contains(err.Error(), "database required") {
		t.Fatalf("expected database required error, got %v", err)
	}

	_, err = engine.GetAccountLinks(context.Background(), "afficart")
	if err == nil || !strings.Contains(err.Error(), "database required") {
		t.Fatalf("expected database required error, got %v", err)
	}

	err = engine.DeleteAccountLink(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "database required") {
		t.Fatalf("expected database required error, got %v", err)
	}
}

func TestCheckDailyQuotaSkippedWithoutDatabase(t *testing.T) {
	// JSON-fallback mode has no publish history to count, so the quota is not
	// enforceable there and must not block a publish.
	engine := Engine{db: nil}

	acc := models.Account{Name: "afficart", MaxPostsPerDay: 1}
	if err := engine.checkDailyQuota(context.Background(), acc); err != nil {
		t.Fatalf("expected no error without a database, got %v", err)
	}
}

func TestCheckDailyQuotaSkippedWhenUncapped(t *testing.T) {
	// An account with no cap needs no count query at all, so this must return
	// before touching the nil pool rather than panicking on it.
	engine := Engine{db: nil}

	acc := models.Account{Name: "afficart", MaxPostsPerDay: 0}
	if err := engine.checkDailyQuota(context.Background(), acc); err != nil {
		t.Fatalf("expected no error for an uncapped account, got %v", err)
	}
}

func TestQuotaExceededErrorMessage(t *testing.T) {
	// The message reaches the operator through the Publisher screen, so it has
	// to say which account and by how much - "quota exceeded" alone is useless
	// when fifteen channels are selected.
	err := QuotaExceededError{Account: "Dealz Adda", Posted: 2, MaxPerDay: 1}

	message := err.Error()
	for _, want := range []string{"Dealz Adda", "2", "1"} {
		if !strings.Contains(message, want) {
			t.Errorf("error message %q should mention %q", message, want)
		}
	}
}

// accountBatchSize became a pure function when TriggerAutoPostJob started
// threading the eligibility check's count into it instead of re-querying.
// These cases pin the fail-open and quota-boundary behavior that move made
// easy to get wrong.
func TestAccountBatchSize(t *testing.T) {
	tests := []struct {
		name       string
		maxPerDay  int
		todayCount int
		want       int
	}{
		{"uncapped account gets one item", 0, 0, 1},
		{"uncapped account ignores the count", 0, 99, 1},
		{"negative cap is treated as uncapped", -3, 5, 1},
		{"unknown count fails open to one", 10, -1, 1},
		{"unknown count fails open even when uncapped", 0, -1, 1},
		{"fresh account gets its whole quota", 5, 0, 5},
		{"partly used quota gets the remainder", 5, 3, 2},
		{"exactly at quota gets nothing", 5, 5, 0},
		{"over quota gets nothing, never negative", 5, 9, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			acc := models.Account{Name: "acct", MaxPostsPerDay: tt.maxPerDay}
			if got := accountBatchSize(acc, tt.todayCount); got != tt.want {
				t.Errorf("accountBatchSize(max=%d, today=%d) = %d, want %d",
					tt.maxPerDay, tt.todayCount, got, tt.want)
			}
		})
	}
}
