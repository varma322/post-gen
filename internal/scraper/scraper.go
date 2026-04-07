package scraper

import (
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"post-gen/internal/config"
	"post-gen/internal/models"
)

// Scraper defines the interface for platform-specific scraping logic.
type Scraper interface {
	Scrape(url string) (*models.Product, error)
}

// GetScraper returns an appropriate Scraper implementation based on the URL domain.
func GetScraper(url string, allSelectors config.Selectors) (Scraper, error) {
	if strings.Contains(url, "amazon") {
		sel, ok := allSelectors["amazon"]
		if !ok {
			return nil, errors.New("amazon selectors missing from selectors.json")
		}
		return NewAmazonScraper(sel), nil
	}

	// Future platforms like Flipkart can be added here
	// if strings.Contains(url, "flipkart") { ... }

	return nil, fmt.Errorf("unsupported platform for URL: %s", url)
}

var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.1 Safari/605.1.15",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:109.0) Gecko/20100101 Firefox/110.0",
}

// Shared Utilities for scrapers

func cleanText(text string) string {
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, "\n", " ")
	re := regexp.MustCompile(`\s+`)
	return re.ReplaceAllString(text, " ")
}

func cleanPrice(price string) string {
	price = strings.ReplaceAll(price, "₹", "")
	price = strings.TrimSpace(price)
	price = strings.TrimSuffix(price, ".")
	return price
}

func calculateDiscount(dealPriceStr, mrpStr string) string {
	if dealPriceStr == "" || mrpStr == "" {
		return ""
	}
	dpStr := strings.ReplaceAll(dealPriceStr, ",", "")
	mrpCleanStr := strings.ReplaceAll(mrpStr, ",", "")

	dp, err1 := strconv.ParseFloat(dpStr, 64)
	mrp, err2 := strconv.ParseFloat(mrpCleanStr, 64)

	if err1 == nil && err2 == nil && mrp > 0 && dp < mrp {
		discount := ((mrp - dp) / mrp) * 100
		return fmt.Sprintf("%.0f", discount)
	}
	return ""
}

func getHttpClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second}
}

func getRandomUserAgent() string {
	rand.Seed(time.Now().UnixNano())
	return userAgents[rand.Intn(len(userAgents))]
}
