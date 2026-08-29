package providers

import (
	"context"
	"fmt"
	"log"
	"strings"

	"post-gen/internal/models"
	"post-gen/internal/scraper"
)

// CategorySlugs maps a scoring category to the Amazon listing slug that holds
// it. Only categories listed here can be served by the fallback.
//
// These are Amazon's slugs, not our category names, so the mapping is stated
// rather than derived - "Home" is "kitchen" on amazon.in, which no amount of
// lower-casing would have guessed.
var CategorySlugs = map[string]string{
	"Electronics": "electronics",
	"Kitchen":     "kitchen",
	"Home":        "home-improvement",
	"Fashion":     "apparel",
	"Books":       "books",
}

// defaultTilesPerListing bounds a cold run. Two listings at this cap is the
// worst case for one category, and each unseen tile costs a product fetch.
const defaultTilesPerListing = 20

// ASINIndex reports which ASINs are already stored, so the lister can skip
// products it has seen. *db.Pool satisfies it.
type ASINIndex interface {
	KnownASINs(ctx context.Context, asins []string) (map[string]bool, error)
}

// BestSellers discovers deals by reading Amazon's Best Sellers and Movers and
// Shakers listings.
//
// It is the fallback, not a second source: discovery only reaches it when no
// API-eligible account can answer. It is also the fragile path - listing pages
// rotate their class names far more often than product pages - so it is built
// to fail visibly and cheaply rather than quietly.
type BestSellers struct {
	lister      *scraper.AmazonListScraper
	index       ASINIndex
	marketplace string
	// moversAndShakers reads the rank-gain list as well, which tracks price
	// drops better than raw bestseller rank.
	moversAndShakers bool
	// perPage caps tiles taken from one listing, and it is the only thing
	// bounding the cost of a cold run.
	//
	// Best Sellers tiles carry no list price, so the discount pre-filter cannot
	// fire and every tile taken costs a product fetch the first time its
	// category is seen. Deduplication against stored ASINs makes later runs
	// nearly free, but the first one is paid in full - so this stays small.
	perPage int
}

// NewBestSellers builds the fallback provider. index may be nil, in which case
// no deduplication is done and every tile is returned.
func NewBestSellers(lister *scraper.AmazonListScraper, index ASINIndex, marketplace string) *BestSellers {
	if marketplace == "" {
		marketplace = "www.amazon.in"
	}
	return &BestSellers{
		lister:           lister,
		index:            index,
		marketplace:      marketplace,
		moversAndShakers: true,
		perPage:          defaultTilesPerListing,
	}
}

// Name identifies this provider in stored deals and analytics.
func (p *BestSellers) Name() string { return models.DealProviderScraper }

// Discover reads the listings for one query's category.
//
// The query's saving tier is passed to the lister, though on amazon.in it
// rarely narrows anything: bestseller tiles publish no list price, so a
// product's discount is unknown until its page is fetched.
func (p *BestSellers) Discover(ctx context.Context, query models.DealQuery) ([]models.DealCandidate, error) {
	if p.lister == nil {
		return nil, fmt.Errorf("best sellers provider has no lister")
	}

	slug, ok := CategorySlugs[strings.TrimSpace(query.Category)]
	if !ok {
		return nil, fmt.Errorf("no Amazon listing slug for category %q", query.Category)
	}

	opts := scraper.ListOptions{
		Slug:        slug,
		Category:    query.Category,
		Marketplace: p.marketplace,
		MinDiscount: query.MinSavingPct,
		Limit:       p.perPage,
	}

	candidates, err := p.lister.List(ctx, opts)
	if err != nil {
		return nil, err
	}

	if p.moversAndShakers {
		// Rank-gain first: a product's rank spikes when its price falls, so
		// these are likelier to be genuine drops than steady bestsellers.
		movers := opts
		movers.MoversAndShakers = true

		moved, err := p.lister.List(ctx, movers)
		if err != nil {
			// One list failing should not lose the other; the bestseller list
			// is the dependable half.
			log.Printf("[WARN] Best Sellers: movers-and-shakers for %s failed: %v", query.Category, err)
		} else {
			candidates = append(moved, candidates...)
		}
	}

	return p.withoutKnown(ctx, dedupe(candidates))
}

// dedupe removes repeats, keeping the first sighting. The two listings overlap
// heavily, and Movers and Shakers is deliberately read first so its ordering
// wins.
func dedupe(candidates []models.DealCandidate) []models.DealCandidate {
	seen := make(map[string]bool, len(candidates))
	unique := make([]models.DealCandidate, 0, len(candidates))

	for _, candidate := range candidates {
		if candidate.ASIN == "" || seen[candidate.ASIN] {
			continue
		}
		seen[candidate.ASIN] = true
		unique = append(unique, candidate)
	}

	return unique
}

// withoutKnown drops candidates already in the deal store.
//
// This is what keeps the fallback affordable across runs: bestseller lists
// barely move day to day, so after the first run most tiles are already known
// and need no product fetch at all.
func (p *BestSellers) withoutKnown(ctx context.Context, candidates []models.DealCandidate) ([]models.DealCandidate, error) {
	if p.index == nil || len(candidates) == 0 {
		return candidates, nil
	}

	asins := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		asins = append(asins, candidate.ASIN)
	}

	known, err := p.index.KnownASINs(ctx, asins)
	if err != nil {
		// Knowing less than we could is not a reason to discover nothing; the
		// upsert deduplicates anyway, this only saves work.
		log.Printf("[WARN] Best Sellers: could not check known ASINs, proceeding without dedupe: %v", err)
		return candidates, nil
	}

	fresh := make([]models.DealCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if !known[candidate.ASIN] {
			fresh = append(fresh, candidate)
		}
	}

	return fresh, nil
}
