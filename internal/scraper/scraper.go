package scraper

import (
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"post-gen/internal/config"
	"post-gen/internal/models"

	"github.com/PuerkitoBio/goquery"
)

var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.1 Safari/605.1.15",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:109.0) Gecko/20100101 Firefox/110.0",
}

// ScrapeProduct fetches and parses the product details from the given URL.
func ScrapeProduct(url string, sel *config.Selectors) (*models.Product, error) {
	maxRetries := 3
	var res *http.Response
	var err error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		// Anti-block: Random rate-limiting delay between 1 and 3 seconds
		rand.Seed(time.Now().UnixNano())
		delay := time.Duration(rand.Intn(3)+1) * time.Second
		if attempt > 1 {
			log.Printf("Attempt %d/%d: Retrying after %v...", attempt, maxRetries, delay)
		} else {
			// Optional tiny delay even on first attempt could help but omitted for speed.
			// Let's add a small random delay initially too, to simulate human pacing.
			time.Sleep(time.Duration(rand.Intn(1000)) * time.Millisecond)
		}

		if attempt > 1 {
			time.Sleep(delay)
		}

		req, reqErr := http.NewRequest("GET", url, nil)
		if reqErr != nil {
			return nil, reqErr
		}

		// Basic anti-block header
		req.Header.Set("User-Agent", userAgents[rand.Intn(len(userAgents))])
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")

		client := &http.Client{Timeout: 15 * time.Second}
		res, err = client.Do(req)

		if err == nil && res.StatusCode == 200 {
			break // Success, exit retry loop
		}

		// Need to close body if we got a response but it wasn't 200 (e.g. 503)
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

	// Extract data using selectors
	product.Title = cleanText(doc.Find(sel.Title).First().Text())
	product.DealPrice = cleanPrice(doc.Find(sel.Price).First().Text())
	product.MRP = cleanPrice(doc.Find(sel.MRP).First().Text())

	// Features
	doc.Find(sel.Features).Each(func(i int, s *goquery.Selection) {
		feature := cleanText(s.Text())
		if feature != "" {
			product.Features = append(product.Features, feature)
		}
	})

	// Limit features to 6 to prevent messy templates
	if len(product.Features) > 6 {
		product.Features = product.Features[:6]
	}

	// Calculate discount
	product.Discount = calculateDiscount(product.DealPrice, product.MRP)

	if product.Title == "" || product.DealPrice == "" {
		return nil, errors.New("failed to extract product title or price - possible layout change, block, or CAPTCHA")
	}

	return &product, nil
}

// cleanText trims whitespace and newlines
func cleanText(text string) string {
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, "\n", " ")
	// Collapse multiple spaces
	re := regexp.MustCompile(`\s+`)
	return re.ReplaceAllString(text, " ")
}

// cleanPrice removes currency symbols and extra characters
func cleanPrice(price string) string {
	price = strings.ReplaceAll(price, "₹", "")
	price = strings.TrimSpace(price)
	price = strings.TrimSuffix(price, ".")
	return price
}

// calculateDiscount computes the percentage off
func calculateDiscount(dealPriceStr, mrpStr string) string {
	if dealPriceStr == "" || mrpStr == "" {
		return ""
	}

	// Remove commas for parsing
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
