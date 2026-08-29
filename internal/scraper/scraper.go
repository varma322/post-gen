package scraper

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"post-gen/internal/config"
	"post-gen/internal/models"
)

// Scraper defines the interface for platform-specific scraping logic.
type Scraper interface {
	Scrape(ctx context.Context, url string) (*models.Product, error)
}

// ScrapeMeta describes how a scrape was actually satisfied. The Scraper Stats
// widget needs a count of API-to-HTML fallbacks, and only the Creators API
// wrapper knows when one happened.
type ScrapeMeta struct {
	// Source is "creators_api" or "html".
	Source string
	// FallbackReason is empty unless the Creators API path was abandoned for
	// the HTML scraper, in which case it says why.
	FallbackReason string
}

// Fellback reports whether the scrape ended up on the HTML fallback path.
func (m ScrapeMeta) Fellback() bool { return m.FallbackReason != "" }

// MetaScraper is an optional interface a Scraper may implement to report how
// the scrape was served. Call sites type-assert for it and fall back to plain
// Scrape, so implementations that don't care (AmazonScraper) stay untouched.
type MetaScraper interface {
	Scraper
	ScrapeWithMeta(ctx context.Context, url string) (*models.Product, ScrapeMeta, error)
}

// ScrapeReportingMeta runs s, using the richer MetaScraper path when s
// supports it and synthesising a plain "html" result when it does not.
func ScrapeReportingMeta(ctx context.Context, s Scraper, url string) (*models.Product, ScrapeMeta, error) {
	if ms, ok := s.(MetaScraper); ok {
		return ms.ScrapeWithMeta(ctx, url)
	}
	product, err := s.Scrape(ctx, url)
	return product, ScrapeMeta{Source: "html"}, err
}

// GetScraper returns an appropriate Scraper implementation based on the URL domain.
func GetScraper(rawURL string, allSelectors config.Selectors) (Scraper, error) {
	if strings.Contains(rawURL, "amazon") || strings.Contains(rawURL, "amzn.") {
		sel, ok := allSelectors["amazon"]
		if !ok {
			return nil, errors.New("amazon selectors missing from selectors.json")
		}

		htmlScraper := NewAmazonScraper(sel)

		if client := NewCreatorAPIClient(htmlScraper); client != nil {
			return client, nil
		}

		return htmlScraper, nil
	}

	// Future platforms like Flipkart can be added here
	// if strings.Contains(rawURL, "flipkart") { ... }

	return nil, fmt.Errorf("unsupported platform for URL: %s", rawURL)
}

// IsValidURL checks if a string is a valid URL with a scheme and host.
func IsValidURL(u string) bool {
	_, err := url.ParseRequestURI(u)
	if err != nil {
		return false
	}
	parsed, err := url.Parse(u)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return false
	}
	return true
}

