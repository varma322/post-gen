package scraper

import (
	"context"
	"testing"

	"post-gen/internal/models"
)

// stubScraper stands in for the HTML fallback, recording whether it was used.
type stubScraper struct {
	called bool
}

func (s *stubScraper) Scrape(ctx context.Context, url string) (*models.Product, error) {
	s.called = true
	return &models.Product{Title: "Stub product", DealPrice: "1,000.00"}, nil
}

func TestPartnerTagFromContext(t *testing.T) {
	ctx := WithPartnerTag(context.Background(), "dealzaddas-21")
	if got := partnerTagFrom(ctx); got != "dealzaddas-21" {
		t.Errorf("partnerTagFrom = %q, want the tag put in", got)
	}
}

func TestWithPartnerTagTrimsAndIgnoresBlank(t *testing.T) {
	ctx := WithPartnerTag(context.Background(), "  smartbuy016-21  ")
	if got := partnerTagFrom(ctx); got != "smartbuy016-21" {
		t.Errorf("partnerTagFrom = %q, want it trimmed", got)
	}

	// A blank tag must not shadow the configured default with an empty string.
	blank := WithPartnerTag(context.Background(), "   ")
	if got := partnerTagFrom(blank); got != "" {
		t.Errorf("a blank tag should not be stored, got %q", got)
	}
}

func TestEffectivePartnerTagPrefersTheAccountTag(t *testing.T) {
	scraper := &AmazonCreatorAPIScraper{defaultPartnerTag: "configured-21"}

	ctx := WithPartnerTag(context.Background(), "account-21")
	if got := scraper.effectivePartnerTag(ctx); got != "account-21" {
		t.Errorf("effectivePartnerTag = %q, want the per-account tag to win", got)
	}
}

func TestEffectivePartnerTagFallsBackToConfigured(t *testing.T) {
	scraper := &AmazonCreatorAPIScraper{defaultPartnerTag: "configured-21"}

	if got := scraper.effectivePartnerTag(context.Background()); got != "configured-21" {
		t.Errorf("effectivePartnerTag = %q, want the configured default", got)
	}
}

func TestEffectivePartnerTagHasNoHardcodedFallback(t *testing.T) {
	// Regression: this used to return the literal "notyoffers-21", attributing
	// every account's catalog lookups to one tag baked into the source - which
	// happened to belong to an account that had been deactivated.
	scraper := &AmazonCreatorAPIScraper{}

	if got := scraper.effectivePartnerTag(context.Background()); got != "" {
		t.Errorf("effectivePartnerTag = %q, want empty with nothing configured", got)
	}
}

func TestScrapeWithoutPartnerTagUsesHTMLFallback(t *testing.T) {
	// With no tag to attribute the call to, the API must not be called at all.
	// Borrowing another account's tag is the failure being prevented; the HTML
	// scraper returns the same product data with no attribution.
	stub := &stubScraper{}
	scraper := &AmazonCreatorAPIScraper{fallback: stub}

	_, meta, _ := scraper.ScrapeWithMeta(context.Background(), "https://www.amazon.in/dp/B0TEST00001")

	if !stub.called {
		t.Error("expected the HTML fallback to be used")
	}
	if meta.Source != "html" {
		t.Errorf("meta.Source = %q, want html", meta.Source)
	}
	if meta.FallbackReason != "no partner tag" {
		t.Errorf("meta.FallbackReason = %q, want it to name the missing tag", meta.FallbackReason)
	}
}

func TestScrapeMetaFellback(t *testing.T) {
	if (ScrapeMeta{Source: "creators_api"}).Fellback() {
		t.Error("an API-served scrape should not report a fallback")
	}
	if !(ScrapeMeta{Source: "html", FallbackReason: "api error"}).Fellback() {
		t.Error("a scrape with a reason should report a fallback")
	}
}
