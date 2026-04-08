package scraper

import (
	"errors"
	"fmt"
	"log"
	"math/rand"
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
		// Anti-block: Random rate-limiting delay between 1 and 3 seconds
		delay := time.Duration(rand.Intn(3)+1) * time.Second
		if attempt > 1 {
			log.Printf("[INFO] Attempt %d/%d: Retrying after %v...", attempt, maxRetries, delay)
		} else {
			// Small random sleep to simulate human usage pace
			time.Sleep(time.Duration(rand.Intn(1000)) * time.Millisecond)
		}

		if attempt > 1 {
			time.Sleep(delay)
		}

		req, reqErr := http.NewRequest("GET", url, nil)
		if reqErr != nil {
			return nil, reqErr
		}

		// Basic anti-block headers
		req.Header.Set("User-Agent", getRandomUserAgent())
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")

		client := getHttpClient()
		res, err = client.Do(req)

		if err == nil && res.StatusCode == 200 {
			break // Success, exit retry loop
		}

		if res != nil {
			res.Body.Close()
		}

		if attempt == maxRetries {
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("status code error: %d %s", res.StatusCode, res.Status)
		}
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
