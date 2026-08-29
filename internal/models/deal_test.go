package models

import (
	"strings"
	"testing"
)

func validDeal() Deal {
	return Deal{
		ASIN:     "B0TEST00001",
		Title:    "Test product",
		URL:      "https://www.amazon.in/dp/B0TEST00001",
		Provider: DealProviderCreatorAPI,
		Status:   DealNew,
	}
}

func TestDealValidateAcceptsAWellFormedDeal(t *testing.T) {
	if err := validDeal().Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDealValidateRequiresASINAndURL(t *testing.T) {
	deal := validDeal()
	deal.ASIN = "  "
	if err := deal.Validate(); err == nil {
		t.Error("expected a blank ASIN to be rejected")
	}

	deal = validDeal()
	deal.URL = ""
	if err := deal.Validate(); err == nil {
		t.Error("expected a missing URL to be rejected")
	}
}

func TestDealValidateRejectsAnUnknownProvider(t *testing.T) {
	deal := validDeal()
	deal.Provider = "telegram_relay"

	err := deal.Validate()
	if err == nil {
		t.Fatal("expected an unknown provider to be rejected")
	}
	// The message should name the offending value, not just say "invalid".
	if got := err.Error(); !strings.Contains(got, "telegram_relay") {
		t.Errorf("error %q should name the bad provider", got)
	}
}

func TestDealValidateRejectsAnUnknownStatus(t *testing.T) {
	deal := validDeal()
	deal.Status = "publishing"

	if err := deal.Validate(); err == nil {
		t.Error("expected an unknown status to be rejected")
	}
}

func TestValidDealStatusCoversThePipeline(t *testing.T) {
	for _, status := range []string{DealNew, DealApproved, DealQueued, DealPosted, DealExpired, DealIgnored} {
		if !ValidDealStatus(status) {
			t.Errorf("%q should be a valid status", status)
		}
	}
	if ValidDealStatus("") || ValidDealStatus("nonsense") {
		t.Error("unknown statuses should not validate")
	}
}

func TestDealSavings(t *testing.T) {
	deal := validDeal()
	deal.Price = 1499
	deal.OldPrice = 2999

	if got := deal.Savings(); got != 1500 {
		t.Errorf("Savings = %v, want 1500", got)
	}
}

func TestDealSavingsIsZeroWhenThereIsNoDiscount(t *testing.T) {
	// An inflated-MRP feed sometimes reports an old price at or below the
	// current one; that is not a negative saving.
	deal := validDeal()
	deal.Price = 1499
	deal.OldPrice = 1200

	if got := deal.Savings(); got != 0 {
		t.Errorf("Savings = %v, want 0 when old price is lower", got)
	}

	deal.OldPrice = 0
	if got := deal.Savings(); got != 0 {
		t.Errorf("Savings = %v, want 0 when no old price is known", got)
	}
}

func TestCandidateConvertsToANewDeal(t *testing.T) {
	candidate := DealCandidate{
		ASIN:        "B0CAND00001",
		Title:       "Candidate",
		URL:         "https://www.amazon.in/dp/B0CAND00001",
		Category:    "Electronics",
		ImageURL:    "https://example.test/i.jpg",
		Price:       999,
		OldPrice:    2499,
		DiscountPct: 60,
		Provider:    DealProviderScraper,
	}

	deal := candidate.Deal()

	if deal.Status != DealNew {
		t.Errorf("status = %q, want a converted candidate to start as new", deal.Status)
	}
	if deal.ASIN != candidate.ASIN || deal.Category != candidate.Category {
		t.Error("conversion dropped identifying fields")
	}
	if deal.DiscountPercent != 60 {
		t.Errorf("discount = %d, want 60", deal.DiscountPercent)
	}
	if err := deal.Validate(); err != nil {
		t.Errorf("a converted candidate should validate: %v", err)
	}
}
