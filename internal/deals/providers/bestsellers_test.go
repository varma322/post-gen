package providers

import (
	"context"
	"errors"
	"strings"
	"testing"

	"post-gen/internal/config"
	"post-gen/internal/models"
	"post-gen/internal/scraper"
)

// fakeIndex reports a fixed set of ASINs as already stored.
type fakeIndex struct {
	known map[string]bool
	err   error
	asked [][]string
}

func (f *fakeIndex) KnownASINs(ctx context.Context, asins []string) (map[string]bool, error) {
	f.asked = append(f.asked, asins)
	if f.err != nil {
		return nil, f.err
	}

	found := map[string]bool{}
	for _, asin := range asins {
		if f.known[asin] {
			found[asin] = true
		}
	}
	return found, nil
}

func TestDedupeKeepsTheFirstSighting(t *testing.T) {
	// The two listings overlap heavily, and movers-and-shakers is read first so
	// its ordering wins.
	unique := dedupe([]models.DealCandidate{
		{ASIN: "B0FIRST0001", Title: "From movers"},
		{ASIN: "B0FIRST0001", Title: "From bestsellers"},
		{ASIN: "B0SECOND001", Title: "Other"},
		{ASIN: "", Title: "No ASIN at all"},
	})

	if len(unique) != 2 {
		t.Fatalf("got %d candidates, want 2", len(unique))
	}
	if unique[0].Title != "From movers" {
		t.Errorf("kept %q, want the first sighting", unique[0].Title)
	}
}

func TestWithoutKnownDropsStoredASINs(t *testing.T) {
	// This is what keeps the fallback affordable across runs: bestseller lists
	// barely move, so most tiles are already known after the first pass.
	index := &fakeIndex{known: map[string]bool{"B0KNOWN0001": true}}
	provider := NewBestSellers(nil, index, "")

	fresh, err := provider.withoutKnown(context.Background(), []models.DealCandidate{
		{ASIN: "B0KNOWN0001"},
		{ASIN: "B0FRESH0001"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(fresh) != 1 || fresh[0].ASIN != "B0FRESH0001" {
		t.Errorf("got %+v, want only the unseen ASIN", fresh)
	}
	if len(index.asked) != 1 || len(index.asked[0]) != 2 {
		t.Errorf("index was asked %v, want one batched lookup", index.asked)
	}
}

func TestWithoutKnownProceedsWhenTheIndexFails(t *testing.T) {
	// Knowing less than we could is not a reason to discover nothing - the
	// upsert deduplicates anyway, this only saves work.
	index := &fakeIndex{err: errors.New("database down")}
	provider := NewBestSellers(nil, index, "")

	candidates := []models.DealCandidate{{ASIN: "B0ANYITEM01"}}
	fresh, err := provider.withoutKnown(context.Background(), candidates)
	if err != nil {
		t.Fatalf("an index failure should not fail discovery: %v", err)
	}
	if len(fresh) != 1 {
		t.Errorf("got %d candidates, want the batch passed through", len(fresh))
	}
}

func TestWithoutKnownWithNoIndex(t *testing.T) {
	provider := NewBestSellers(nil, nil, "")

	candidates := []models.DealCandidate{{ASIN: "B0ANYITEM01"}}
	fresh, err := provider.withoutKnown(context.Background(), candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fresh) != 1 {
		t.Errorf("got %d candidates, want them all without an index", len(fresh))
	}
}

func TestBestSellersNameIdentifiesTheSource(t *testing.T) {
	if got := NewBestSellers(nil, nil, "").Name(); got != models.DealProviderScraper {
		t.Errorf("Name = %q, want %q", got, models.DealProviderScraper)
	}
}

func TestBestSellersRejectsAnUnmappedCategory(t *testing.T) {
	// A category discovery searches but the fallback cannot reach should say so
	// rather than silently returning nothing, which reads as "no deals".
	// A configured lister, so the category check is what fails rather than the
	// nil-lister guard ahead of it.
	provider := NewBestSellers(scraper.NewAmazonListScraper(config.ListingSelectors{Item: ".tile"}), nil, "")

	_, err := provider.Discover(context.Background(), models.DealQuery{Category: "Automotive"})
	if err == nil {
		t.Fatal("expected an unmapped category to be reported")
	}
	if !strings.Contains(err.Error(), "Automotive") {
		t.Errorf("error %q should name the category", err)
	}
}

func TestBestSellersWithoutAListerFails(t *testing.T) {
	provider := NewBestSellers(nil, nil, "")

	if _, err := provider.Discover(context.Background(), models.DealQuery{Category: "Electronics"}); err == nil {
		t.Error("expected a provider with no lister to report an error")
	}
}

func TestEveryScoredCategoryHasASlug(t *testing.T) {
	// A category the matrix can search but the fallback cannot reach would
	// simply stop being discovered whenever the API is unavailable.
	for name := range CategorySlugs {
		if strings.TrimSpace(CategorySlugs[name]) == "" {
			t.Errorf("category %q has a blank slug", name)
		}
	}

	for _, required := range []string{"Electronics", "Kitchen", "Home", "Fashion", "Books"} {
		if _, ok := CategorySlugs[required]; !ok {
			t.Errorf("scoring category %q has no listing slug", required)
		}
	}
}
