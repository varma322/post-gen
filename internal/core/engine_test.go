package core

import (
	"errors"
	"strings"
	"testing"

	"post-gen/internal/config"
	"post-gen/internal/models"
	"post-gen/internal/scraper"
)

type stubScraper struct {
	product *models.Product
	err     error
}

func (s stubScraper) Scrape(url string) (*models.Product, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.product, nil
}

func TestGeneratePostsRejectsUnknownAccount(t *testing.T) {
	engine := Engine{
		accounts: []models.Account{{Name: "afficart", TemplatePath: "templates/afficart.tmpl"}},
	}

	_, err := engine.GeneratePosts([]string{"https://amazon.in/example"}, []string{"missing"})
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

	results, err := engine.GeneratePosts([]string{"not-a-url"}, []string{"afficart"})
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

	results, err := engine.GeneratePosts([]string{"https://example.com/product"}, []string{"afficart"})
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

	results, err := engine.GeneratePosts([]string{"https://amazon.in/example"}, nil)
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

	results, err := engine.GeneratePosts([]string{"https://amazon.in/example"}, []string{"afficart"})
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
