package scraper

import (
	"fmt"
	"math/rand"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.1 Safari/605.1.15",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:109.0) Gecko/20100101 Firefox/110.0",
}

func init() {
	rand.Seed(time.Now().UnixNano()) //nolint:staticcheck
}

// retryMsg returns a human-readable retry hint based on whether this is the last attempt.
func retryMsg(isLast bool) string {
	if isLast {
		return "No more retries."
	}
	return "Will retry..."
}

// FindFirst tries multiple comma-separated selectors and returns the first non-empty result.
func FindFirst(doc *goquery.Document, selectors string, cleaner func(string) string) string {
	parts := strings.Split(selectors, ",")
	for _, s := range parts {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		val := cleaner(doc.Find(s).First().Text())
		if val != "" {
			return val
		}
	}
	return ""
}

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

// sharedHTTPClient is a single package-level client reused across all scrape requests.
// Reusing the client (and its underlying Transport) allows TCP connections to be pooled,
// preventing socket exhaustion during bulk processing.
var sharedHTTPClient = &http.Client{
	Timeout: 15 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	},
}

func getHttpClient() *http.Client {
	return sharedHTTPClient
}

func getRandomUserAgent() string {
	return userAgents[rand.Intn(len(userAgents))]
}

// parsePriceToFloat cleans and parses a price string into a float64
func parsePriceToFloat(priceStr string) (float64, bool) {
	priceStr = strings.ReplaceAll(priceStr, ",", "")
	priceStr = strings.ReplaceAll(priceStr, "₹", "")
	priceStr = strings.TrimSpace(priceStr)

	// Regex to match the first number (integer or decimal)
	re := regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`)
	match := re.FindString(priceStr)
	if match == "" {
		return 0, false
	}

	val, err := strconv.ParseFloat(match, 64)
	if err != nil {
		return 0, false
	}
	return val, true
}

// parseDiscountPercentage parses a discount percentage string into a float64
func parseDiscountPercentage(discountStr string) (float64, bool) {
	discountStr = strings.ReplaceAll(discountStr, "%", "")
	discountStr = strings.ReplaceAll(discountStr, "-", "")
	discountStr = strings.TrimSpace(discountStr)
	val, err := strconv.ParseFloat(discountStr, 64)
	if err != nil {
		return 0, false
	}
	return val, true
}

// extractScrapedSavings parses the discount percentage or saving amount from the page
func extractScrapedSavings(doc *goquery.Document) (scrapedPct float64, scrapedAmount float64, hasPct bool, hasAmount bool) {
	// Look for percentage saving
	pctSel := ".savingsPercentage, .reinventPriceSavingsPercentageMargin, .apex-savings-percentage"
	pctText := doc.Find(pctSel).First().Text()
	pctText = strings.TrimSpace(pctText)
	if pctText != "" {
		if val, ok := parseDiscountPercentage(pctText); ok && val > 0 {
			scrapedPct = val
			hasPct = true
		}
	}

	// Look for absolute saving amount
	amtSel := "#regularprice_savings, #dealprice_savings, .reinventPriceSavings, .price-character-saving-text"
	amtText := doc.Find(amtSel).First().Text()
	amtText = strings.TrimSpace(amtText)
	if amtText != "" {
		if val, ok := parsePriceToFloat(amtText); ok && val > 0 {
			scrapedAmount = val
			hasAmount = true
		}
	}

	return
}

