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

	"github.com/PuerkitoBio/goquery"
)

var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.1 Safari/605.1.15",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:109.0) Gecko/20100101 Firefox/110.0",
}

// ScrapeProduct fetches and parses the product details from the given URL.
func ScrapeProduct(url string, sel *config.Selectors) (*models.Product, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	// Basic anti-block header
	rand.Seed(time.Now().UnixNano())
	req.Header.Set("User-Agent", userAgents[rand.Intn(len(userAgents))])
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return nil, fmt.Errorf("status code error: %d %s", res.StatusCode, res.Status)
	}

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

	// Calculate discount
	product.Discount = calculateDiscount(product.DealPrice, product.MRP)

	if product.Title == "" {
		return nil, errors.New("failed to extract product title - possible layout change or CAPTCHA")
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
