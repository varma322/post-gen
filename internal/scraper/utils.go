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

func getHttpClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second}
}

func getRandomUserAgent() string {
	return userAgents[rand.Intn(len(userAgents))]
}
