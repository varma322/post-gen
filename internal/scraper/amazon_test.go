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
