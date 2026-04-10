package scraper

import (
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"time"

	"post-gen/internal/config"
	"post-gen/internal/models"

	"github.com/PuerkitoBio/goquery"
)

// AmazonScraper implements the Scraper interface for Amazon product pages.
type AmazonScraper struct {
	Sel config.PlatformSelectors
}

// NewAmazonScraper initializes a new AmazonScraper with the provided selectors.
func NewAmazonScraper(sel config.PlatformSelectors) *AmazonScraper {
	return &AmazonScraper{Sel: sel}
}

// Scrape performs the scraping logic for Amazon.
func (a *AmazonScraper) Scrape(url string) (*models.Product, error) {
	maxRetries := 3
	var res *http.Response
	var err error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt == 1 {
			// Small random initial delay to simulate human pace
			time.Sleep(time.Duration(rand.Intn(1000)) * time.Millisecond)
		}

		req, reqErr := http.NewRequest("GET", url, nil)
		if reqErr != nil {
			return nil, reqErr
		}

		req.Header.Set("User-Agent", getRandomUserAgent())
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")

		client := getHttpClient()
		res, err = client.Do(req)

		if err == nil && res.StatusCode == 200 {
			break // Success
		}

		// --- Classify the error ---
		isLastAttempt := attempt == maxRetries

		if err != nil {
			// Check for timeout specifically
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				log.Printf("[TIMEOUT] Attempt %d/%d timed out. %s", attempt, maxRetries, retryMsg(isLastAttempt))
			} else {
				log.Printf("[NET_ERR] Attempt %d/%d network error: %v. %s", attempt, maxRetries, err, retryMsg(isLastAttempt))
			}
			if isLastAttempt {
				return nil, err
			}
			time.Sleep(time.Duration(rand.Intn(3)+2) * time.Second) // 2–4s retry delay
			continue
		}

		// HTTP error response
		statusCode := res.StatusCode
		res.Body.Close()

		if statusCode == 403 {
			// Hard block — retrying won't help
			log.Printf("[BLOCKED] Attempt %d/%d: HTTP 403 Forbidden. Amazon has blocked this request. Aborting retries.", attempt, maxRetries)
			return nil, fmt.Errorf("HTTP 403: Amazon returned a hard block (Forbidden)")
		}

		if statusCode == 429 {
			// Rate-limited — back off longer before retrying
			backoff := time.Duration(rand.Intn(10)+10) * time.Second // 10–20s
			log.Printf("[RATE_LIMITED] Attempt %d/%d: HTTP 429 Too Many Requests. %s (backoff: %v)", attempt, maxRetries, retryMsg(isLastAttempt), backoff)
			if isLastAttempt {
				return nil, fmt.Errorf("HTTP 429: Amazon rate-limited this request after %d attempts", maxRetries)
			}
			time.Sleep(backoff)
			continue
		}

		// Other HTTP status (5xx, 4xx, etc.)
		log.Printf("[HTTP_ERR] Attempt %d/%d: HTTP %d %s. %s", attempt, maxRetries, statusCode, res.Status, retryMsg(isLastAttempt))
		if isLastAttempt {
			return nil, fmt.Errorf("HTTP %d: %s after %d attempts", statusCode, res.Status, maxRetries)
		}
		time.Sleep(time.Duration(rand.Intn(3)+1) * time.Second) // 1–3s retry delay
	}
	defer res.Body.Close()

	// Load the HTML document
	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return nil, err
	}

	var product models.Product
	product.Link = url

	// Extract data using platform-specific selectors
	product.Title = FindFirst(doc, a.Sel.Title, cleanText)
	product.DealPrice = FindFirst(doc, a.Sel.Price, cleanPrice)
	product.MRP = FindFirst(doc, a.Sel.MRP, cleanPrice)

	// Features
	doc.Find(a.Sel.Features).Each(func(i int, s *goquery.Selection) {
		feature := cleanText(s.Text())
		if feature != "" {
			product.Features = append(product.Features, feature)
		}
	})

	// Truncate features for template neatness
	if len(product.Features) > 6 {
		product.Features = product.Features[:6]
	}

	// Logic for calculating discount
	product.Discount = calculateDiscount(product.DealPrice, product.MRP)

	// Block detection
	if product.Title == "" || product.DealPrice == "" {
		return nil, errors.New("failed to extract Amazon product title or price - possible layout change, block, or CAPTCHA")
	}

	return &product, nil
}
