package scraper

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"post-gen/internal/config"
	"post-gen/internal/models"
)

// Scraper defines the interface for platform-specific scraping logic.
type Scraper interface {
	Scrape(ctx context.Context, url string) (*models.Product, error)
}

// GetScraper returns an appropriate Scraper implementation based on the URL domain.
func GetScraper(rawURL string, allSelectors config.Selectors) (Scraper, error) {
	if strings.Contains(rawURL, "amazon") || strings.Contains(rawURL, "amzn.") {
		sel, ok := allSelectors["amazon"]
		if !ok {
			return nil, errors.New("amazon selectors missing from selectors.json")
		}

		htmlScraper := NewAmazonScraper(sel)

		// Check for Creators API credentials
		clientID := os.Getenv("Credential_ID")
		if clientID == "" {
			clientID = os.Getenv("AMAZON_CREATOR_CLIENT_ID")
		}
		clientSecret := os.Getenv("Secret")
		if clientSecret == "" {
			clientSecret = os.Getenv("AMAZON_CREATOR_CLIENT_SECRET")
		}
		partnerTag := os.Getenv("AMAZON_CREATOR_PARTNER_TAG")
		if partnerTag == "" {
			partnerTag = os.Getenv("Application_ID")
		}
		tokenURL := os.Getenv("AMAZON_CREATOR_TOKEN_URL")

		if clientID != "" && clientSecret != "" {
			return NewAmazonCreatorAPIScraper(clientID, clientSecret, tokenURL, partnerTag, htmlScraper), nil
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

