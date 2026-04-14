package scraper

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"post-gen/internal/config"
	"post-gen/internal/models"
)

// Scraper defines the interface for platform-specific scraping logic.
type Scraper interface {
	Scrape(url string) (*models.Product, error)
}

// GetScraper returns an appropriate Scraper implementation based on the URL domain.
func GetScraper(rawURL string, allSelectors config.Selectors) (Scraper, error) {
	if strings.Contains(rawURL, "amazon") || strings.Contains(rawURL, "amzn.") {
		sel, ok := allSelectors["amazon"]
		if !ok {
			return nil, errors.New("amazon selectors missing from selectors.json")
		}
		return NewAmazonScraper(sel), nil
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
