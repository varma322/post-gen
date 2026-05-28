package scraper

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestIsOutOfStockPage(t *testing.T) {
	html := `<html><body><div id="availability">Currently unavailable.</div></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("failed to build document: %v", err)
	}

	if !isOutOfStockPage(doc) {
		t.Fatal("expected out-of-stock page detection to be true")
	}
}

func TestIsCaptchaPage(t *testing.T) {
	html := `<html><head><title>Amazon CAPTCHA</title></head><body><form action="/errors/validateCaptcha"></form></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("failed to build document: %v", err)
	}

	if !isCaptchaPage(doc) {
		t.Fatal("expected captcha page detection to be true")
	}
}

func TestPriceAndDiscountParsing(t *testing.T) {
	// Test parsePriceToFloat
	if val, ok := parsePriceToFloat("₹1,399.00"); !ok || val != 1399.00 {
		t.Errorf("expected 1399.00, got %v (ok: %v)", val, ok)
	}
	if val, ok := parsePriceToFloat("M.R.P.: ₹1,549"); !ok || val != 1549.0 {
		t.Errorf("expected 1549.0, got %v (ok: %v)", val, ok)
	}

	// Test parseDiscountPercentage
	if val, ok := parseDiscountPercentage("-10%"); !ok || val != 10.0 {
		t.Errorf("expected 10.0, got %v (ok: %v)", val, ok)
	}

	// Test extractScrapedSavings with HTML doc
	html := `<html><body>
		<span class="savingsPercentage">-10%</span>
		<span id="dealprice_savings">Save ₹150</span>
	</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("failed to parse test HTML: %v", err)
	}

	pct, amt, hasPct, hasAmt := extractScrapedSavings(doc)
	if !hasPct || pct != 10.0 {
		t.Errorf("expected pct = 10, got %f (hasPct: %v)", pct, hasPct)
	}
	if !hasAmt || amt != 150.0 {
		t.Errorf("expected amt = 150, got %f (hasAmt: %v)", amt, hasAmt)
	}
}

