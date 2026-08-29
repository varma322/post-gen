package models

import (
	"fmt"
	"strings"
	"time"
)

// Deal statuses.
const (
	// DealNew is a freshly discovered deal that has not been acted on.
	DealNew = "new"
	// DealApproved cleared manual review but is not queued yet.
	DealApproved = "approved"
	// DealQueued has been pushed into queued_products.
	DealQueued = "queued"
	// DealPosted reached a page.
	DealPosted = "posted"
	// DealExpired stopped appearing in discovery runs.
	DealExpired = "expired"
	// DealIgnored was rejected, by score or by hand. Re-discovering an ignored
	// deal must not quietly revive it.
	DealIgnored = "ignored"
)

// Discovery providers.
const (
	// DealProviderCreatorAPI is the Creators API searchItems path.
	DealProviderCreatorAPI = "creator_api"
	// DealProviderScraper is the Best Sellers HTML listing fallback.
	DealProviderScraper = "scraper"
)

// Deal is a discovered product, stored and scored before it reaches the queue.
//
// Prices are numeric here, unlike Product, because scoring needs to compare
// them. Product keeps its string prices for template rendering and is left
// alone; a Deal is converted into one only when handed to the AI layer.
type Deal struct {
	ID              int       `json:"id"`
	ASIN            string    `json:"asin"`
	Title           string    `json:"title"`
	URL             string    `json:"url"`
	Category        string    `json:"category,omitempty"`
	ImageURL        string    `json:"image_url,omitempty"`
	Price           float64   `json:"price"`
	OldPrice        float64   `json:"old_price"`
	DiscountPercent int       `json:"discount_percent"`
	Score           int       `json:"score"`
	Provider        string    `json:"provider"`
	Status          string    `json:"status"`
	FirstSeen       time.Time `json:"first_seen"`
	LastSeen        time.Time `json:"last_seen"`
	CreatedAt       time.Time `json:"created_at"`
}

// Savings is the absolute amount off, in the marketplace's currency.
//
// It is derived rather than stored: old_price and price are what discovery
// observes, and keeping a third column consistent with them buys nothing.
func (d Deal) Savings() float64 {
	if d.OldPrice <= d.Price {
		return 0
	}
	return d.OldPrice - d.Price
}

// Validate checks a deal carries what discovery must supply.
func (d Deal) Validate() error {
	if strings.TrimSpace(d.ASIN) == "" {
		return fmt.Errorf("deal asin is required")
	}
	if strings.TrimSpace(d.URL) == "" {
		return fmt.Errorf("deal %s has no url", d.ASIN)
	}
	switch d.Provider {
	case DealProviderCreatorAPI, DealProviderScraper:
	default:
		return fmt.Errorf("unknown deal provider %q: expected %q or %q",
			d.Provider, DealProviderCreatorAPI, DealProviderScraper)
	}
	if !ValidDealStatus(d.Status) {
		return fmt.Errorf("unknown deal status %q", d.Status)
	}
	return nil
}

// ValidDealStatus reports whether status is one this pipeline recognises.
func ValidDealStatus(status string) bool {
	switch status {
	case DealNew, DealApproved, DealQueued, DealPosted, DealExpired, DealIgnored:
		return true
	}
	return false
}

// DealCandidate is what a discovery provider emits: enough to identify and
// dedupe a product, not necessarily enough to score it.
//
// The Best Sellers fallback fills Price and OldPrice from the listing tile when
// it can, so a candidate that clearly misses the discount threshold is dropped
// before it costs a full product fetch. Zero means "not determined", not "free".
type DealCandidate struct {
	ASIN        string
	Title       string
	URL         string
	Category    string
	ImageURL    string
	Price       float64
	OldPrice    float64
	DiscountPct int
	Provider    string
}

// DealFilter narrows a deal listing. Zero values mean "no constraint", so the
// empty filter lists everything.
type DealFilter struct {
	Status   string
	Category string
	Provider string
	MinScore int
	Limit    int
}

// DealQuery is one cell of the discovery matrix: a category to search, how deep
// to page into it, and how steep a discount to insist on.
//
// Category is a label carried through onto the candidates, not a search term.
// The actual narrowing is done by BrowseNodeID, which must be numeric and must
// resolve - searchItems accepts a numeric node that does not exist and quietly
// returns an unfiltered keyword search instead of an error.
type DealQuery struct {
	Category     string
	BrowseNodeID string
	Keywords     string
	MinSavingPct int
	Page         int
}

// Deal converts a candidate into a storable deal in the "new" state.
func (c DealCandidate) Deal() Deal {
	return Deal{
		ASIN:            c.ASIN,
		Title:           c.Title,
		URL:             c.URL,
		Category:        c.Category,
		ImageURL:        c.ImageURL,
		Price:           c.Price,
		OldPrice:        c.OldPrice,
		DiscountPercent: c.DiscountPct,
		Provider:        c.Provider,
		Status:          DealNew,
	}
}
