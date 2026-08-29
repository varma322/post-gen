package scraper

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"post-gen/internal/config"
	"post-gen/internal/models"
)

// asinAttrRegex matches a canonical /dp/<ASIN> path in a tile's link, used when
// the tile carries no data-asin attribute of its own.
var listingASINRegex = asinRegex

// ListOptions describes one listing page to read.
type ListOptions struct {
	// Slug is the Amazon category slug, e.g. "electronics".
	Slug string
	// Category is the label carried onto candidates. It is not derived from the
	// slug, because the scoring categories are ours and the slugs are Amazon's.
	Category string
	// Marketplace defaults to the Indian storefront.
	Marketplace string
	// MinDiscount drops tiles whose discount can be computed and falls short.
	//
	// Verified against the live page: Best Sellers tiles carry only the current
	// price, with no strikethrough list price and no percentage, so on
	// amazon.in this filter almost never fires and a tile is kept for the
	// product page to judge. It is retained because some marketplaces and some
	// tile variants do carry a list price, and filtering there is free.
	MinDiscount int
	// MoversAndShakers reads the rank-gain list instead of the bestseller list.
	// It has the same HTML shape but tracks price drops far better, because a
	// product's rank spikes when its price falls.
	MoversAndShakers bool
	// Limit caps how many candidates are returned. Zero means no cap.
	Limit int
}

// AmazonListScraper reads Amazon listing pages - Best Sellers and Movers and
// Shakers - into deal candidates.
//
// This is the discovery fallback, used only when no API-eligible account is
// available. It emits candidates, not finished deals: the tile carries enough
// to identify a product and to reject an obviously undiscounted one, and the
// product page fills in the rest.
type AmazonListScraper struct {
	selectors config.ListingSelectors
}

// NewAmazonListScraper builds a lister from the amazon_listings selectors.
func NewAmazonListScraper(selectors config.ListingSelectors) *AmazonListScraper {
	return &AmazonListScraper{selectors: selectors}
}

// List reads one listing page.
func (l *AmazonListScraper) List(ctx context.Context, opts ListOptions) ([]models.DealCandidate, error) {
	if strings.TrimSpace(l.selectors.Item) == "" {
		return nil, fmt.Errorf("listing selectors are not configured (amazon_listings in selectors.json)")
	}
	if strings.TrimSpace(opts.Slug) == "" {
		return nil, fmt.Errorf("a category slug is required")
	}

	marketplace := opts.Marketplace
	if marketplace == "" {
		marketplace = "www.amazon.in"
	}

	listType := "bestsellers"
	if opts.MoversAndShakers {
		listType = "movers-and-shakers"
	}
	pageURL := fmt.Sprintf("https://%s/gp/%s/%s", marketplace, listType, strings.Trim(opts.Slug, "/"))

	doc, err := fetchListingDocument(ctx, pageURL)
	if err != nil {
		return nil, err
	}

	candidates := make([]models.DealCandidate, 0, 50)
	seen := make(map[string]bool, 50)

	doc.Find(l.selectors.Item).EachWithBreak(func(_ int, tile *goquery.Selection) bool {
		if opts.Limit > 0 && len(candidates) >= opts.Limit {
			return false
		}

		candidate, ok := l.candidateFromTile(tile, marketplace, opts)
		if !ok || seen[candidate.ASIN] {
			return true
		}

		seen[candidate.ASIN] = true
		candidates = append(candidates, candidate)
		return true
	})

	if len(candidates) == 0 {
		// Worth saying out loud: an empty list from a page that returned 200 is
		// how a selector break presents itself, and it is indistinguishable
		// from a genuinely empty category unless someone looks.
		log.Printf("[WARN] Best Sellers: %s matched no product tiles. The amazon_listings selectors may be stale.", pageURL)
	}

	return candidates, nil
}

// candidateFromTile reads one product tile, reporting false for a tile that is
// not a product or that clearly misses the discount threshold.
func (l *AmazonListScraper) candidateFromTile(
	tile *goquery.Selection, marketplace string, opts ListOptions,
) (models.DealCandidate, bool) {
	asin := l.tileASIN(tile)
	if asin == "" {
		return models.DealCandidate{}, false
	}

	title := cleanText(tile.Find(l.selectors.Title).First().Text())
	if title == "" {
		return models.DealCandidate{}, false
	}

	candidate := models.DealCandidate{
		ASIN:     asin,
		Title:    title,
		Category: opts.Category,
		Provider: models.DealProviderScraper,
		// Canonical rather than the tile's href, which carries tracking
		// parameters and, on some tiles, a tag that is not ours to use.
		URL: "https://" + marketplace + "/dp/" + asin,
	}

	price, hasPrice := parsePriceToFloat(tile.Find(l.selectors.Price).First().Text())
	mrp, hasMRP := parsePriceToFloat(tile.Find(l.selectors.MRP).First().Text())

	if hasPrice {
		candidate.Price = price
	}
	if hasMRP {
		candidate.OldPrice = mrp
	}

	if hasPrice && hasMRP && mrp > price {
		candidate.DiscountPct = int((mrp - price) / mrp * 100)

		if opts.MinDiscount > 0 && candidate.DiscountPct < opts.MinDiscount {
			return models.DealCandidate{}, false
		}
	}

	return candidate, true
}

// tileASIN reads the ASIN from the tile's own attribute, falling back to the
// /dp/<ASIN> path in its link.
func (l *AmazonListScraper) tileASIN(tile *goquery.Selection) string {
	if asin, ok := tile.Attr("data-asin"); ok && len(strings.TrimSpace(asin)) == 10 {
		return strings.ToUpper(strings.TrimSpace(asin))
	}

	if l.selectors.ASIN != "" {
		if asin, ok := tile.Find(l.selectors.ASIN).First().Attr("data-asin"); ok && len(strings.TrimSpace(asin)) == 10 {
			return strings.ToUpper(strings.TrimSpace(asin))
		}
	}

	href, ok := tile.Find(l.selectors.Link).First().Attr("href")
	if !ok {
		return ""
	}
	if matches := listingASINRegex.FindStringSubmatch(href); len(matches) > 1 {
		return strings.ToUpper(matches[1])
	}

	return ""
}

// fetchListingDocument retrieves a listing page with the same pacing and
// user-agent rotation the product scraper uses.
//
// A 403 aborts immediately: Amazon serving a hard block will not relent within
// a retry window, and hammering it is how an IP earns a longer one.
func fetchListingDocument(ctx context.Context, pageURL string) (*goquery.Document, error) {
	const maxRetries = 3

	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
		if err != nil {
			return nil, fmt.Errorf("creating listing request: %w", err)
		}

		req.Header.Set("User-Agent", getRandomUserAgent())
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

		resp, err := getHttpClient().Do(req)
		if err != nil {
			lastErr = err
			log.Printf("[WARN] Best Sellers: attempt %d/%d for %s failed: %v", attempt, maxRetries, pageURL, err)
		} else if resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()

			doc, err := goquery.NewDocumentFromReader(resp.Body)
			if err != nil {
				return nil, fmt.Errorf("parsing listing page: %w", err)
			}
			if isCaptchaPage(doc) {
				return nil, fmt.Errorf("listing page %s served a CAPTCHA", pageURL)
			}
			return doc, nil
		} else {
			resp.Body.Close()
			if resp.StatusCode == http.StatusForbidden {
				return nil, fmt.Errorf("listing page %s returned HTTP 403: Amazon blocked this request", pageURL)
			}
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			log.Printf("[WARN] Best Sellers: attempt %d/%d for %s got %v", attempt, maxRetries, pageURL, lastErr)
		}

		if attempt < maxRetries {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(rand.Intn(3)+2) * time.Second):
			}
		}
	}

	return nil, fmt.Errorf("fetching %s after %d attempts: %w", pageURL, maxRetries, lastErr)
}
