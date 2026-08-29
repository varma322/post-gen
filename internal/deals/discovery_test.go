package deals

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"post-gen/internal/models"
)

// fakeStore records upserts and reports each ASIN as new the first time.
type fakeStore struct {
	mu    sync.Mutex
	deals map[string]models.Deal
	err   error
	calls int
}

func newFakeStore() *fakeStore {
	return &fakeStore{deals: map[string]models.Deal{}}
}

func (f *fakeStore) UpsertDeal(ctx context.Context, deal models.Deal) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++
	if f.err != nil {
		return false, f.err
	}

	_, existed := f.deals[deal.ASIN]
	f.deals[deal.ASIN] = deal
	return !existed, nil
}

// fakeProvider answers every query with the same candidates, or an error.
type fakeProvider struct {
	name       string
	candidates []models.DealCandidate
	err        error
	queries    []models.DealQuery
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Discover(ctx context.Context, query models.DealQuery) ([]models.DealCandidate, error) {
	f.queries = append(f.queries, query)
	if f.err != nil {
		return nil, f.err
	}

	stamped := make([]models.DealCandidate, 0, len(f.candidates))
	for _, candidate := range f.candidates {
		candidate.Category = query.Category
		candidate.Provider = f.name
		stamped = append(stamped, candidate)
	}
	return stamped, nil
}

func candidate(asin string, discount int) models.DealCandidate {
	return models.DealCandidate{
		ASIN:        asin,
		Title:       "Product " + asin,
		URL:         "https://www.amazon.in/dp/" + asin,
		Price:       1000,
		OldPrice:    4000,
		DiscountPct: discount,
	}
}

func oneCategory() []Category {
	return []Category{{Name: "Electronics", BrowseNodeID: "976419031"}}
}

func TestRunStoresCandidates(t *testing.T) {
	store := newFakeStore()
	provider := &fakeProvider{name: models.DealProviderCreatorAPI, candidates: []models.DealCandidate{
		candidate("B0RUN000001", 70),
		candidate("B0RUN000002", 55),
	}}

	service := NewService(store, nil, []DiscoveryProvider{provider},
		WithCategories(oneCategory()), WithSavingTiers([]int{50}))

	result, err := service.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.New != 2 {
		t.Errorf("New = %d, want 2", result.New)
	}
	if result.Updated != 0 {
		t.Errorf("Updated = %d, want 0 on a first run", result.Updated)
	}
	if got := result.ByProvider[models.DealProviderCreatorAPI]; got != 2 {
		t.Errorf("ByProvider = %v, want both attributed to the API", result.ByProvider)
	}
	if len(store.deals) != 2 {
		t.Errorf("stored %d deals, want 2", len(store.deals))
	}
	for _, deal := range store.deals {
		if deal.Category != "Electronics" {
			t.Errorf("deal %s lost its category", deal.ASIN)
		}
		if deal.Status != models.DealNew {
			t.Errorf("deal %s stored as %q, want new", deal.ASIN, deal.Status)
		}
	}
}

func TestRunDeduplicatesWithinARun(t *testing.T) {
	// The same ASIN routinely appears under several saving tiers of one
	// category. Storing it once per sighting would inflate the counts and emit
	// a spurious update for work already done.
	store := newFakeStore()
	provider := &fakeProvider{name: models.DealProviderCreatorAPI, candidates: []models.DealCandidate{
		candidate("B0DUPE00001", 80),
	}}

	service := NewService(store, nil, []DiscoveryProvider{provider},
		WithCategories(oneCategory()), WithSavingTiers([]int{30, 50, 70}))

	result, err := service.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Queries != 3 {
		t.Fatalf("Queries = %d, want three tiers", result.Queries)
	}
	if result.Candidates != 3 {
		t.Errorf("Candidates = %d, want the raw count before dedupe", result.Candidates)
	}
	if result.New != 1 {
		t.Errorf("New = %d, want the ASIN stored once", result.New)
	}
	if store.calls != 1 {
		t.Errorf("store called %d times, want 1", store.calls)
	}
}

func TestRunCountsUpdatesSeparatelyFromNew(t *testing.T) {
	store := newFakeStore()
	provider := &fakeProvider{name: models.DealProviderCreatorAPI, candidates: []models.DealCandidate{
		candidate("B0AGAIN0001", 70),
	}}
	service := NewService(store, nil, []DiscoveryProvider{provider},
		WithCategories(oneCategory()), WithSavingTiers([]int{50}))

	if _, err := service.Run(context.Background()); err != nil {
		t.Fatalf("first run: %v", err)
	}

	result, err := service.Run(context.Background())
	if err != nil {
		t.Fatalf("second run: %v", err)
	}

	if result.New != 0 || result.Updated != 1 {
		t.Errorf("New = %d, Updated = %d; want a re-sighting counted as an update", result.New, result.Updated)
	}
}

func TestRunContinuesAfterAFailedQuery(t *testing.T) {
	// A throttle on one category must not cost the rest of the matrix.
	store := newFakeStore()
	failing := &failOnceProvider{
		name:       models.DealProviderCreatorAPI,
		failOnTier: 70,
		candidates: []models.DealCandidate{candidate("B0PARTIAL01", 55)},
	}

	service := NewService(store, nil, []DiscoveryProvider{failing},
		WithCategories(oneCategory()), WithSavingTiers([]int{30, 50, 70}))

	result, err := service.Run(context.Background())
	if err != nil {
		t.Fatalf("a partial run should not fail: %v", err)
	}

	if result.Failed != 1 {
		t.Errorf("Failed = %d, want the one throttled tier", result.Failed)
	}
	if result.New != 1 {
		t.Errorf("New = %d, want the surviving tiers to still store", result.New)
	}
	if len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "Electronics") {
		t.Errorf("Errors = %v, want the failure named for the operator", result.Errors)
	}
}

// failOnceProvider fails only for one saving tier.
type failOnceProvider struct {
	name       string
	failOnTier int
	candidates []models.DealCandidate
}

func (f *failOnceProvider) Name() string { return f.name }

func (f *failOnceProvider) Discover(ctx context.Context, query models.DealQuery) ([]models.DealCandidate, error) {
	if query.MinSavingPct == f.failOnTier {
		return nil, errors.New("rate limited")
	}
	stamped := make([]models.DealCandidate, 0, len(f.candidates))
	for _, candidate := range f.candidates {
		candidate.Category = query.Category
		candidate.Provider = f.name
		stamped = append(stamped, candidate)
	}
	return stamped, nil
}

func TestRunFailsWhenEveryQueryFails(t *testing.T) {
	store := newFakeStore()
	provider := &fakeProvider{name: models.DealProviderCreatorAPI, err: errors.New("no eligible account")}

	service := NewService(store, nil, []DiscoveryProvider{provider},
		WithCategories(oneCategory()), WithSavingTiers([]int{50}))

	result, err := service.Run(context.Background())
	if err == nil {
		t.Fatal("a run that stored nothing and failed everything should report an error")
	}
	if result.Failed != 1 {
		t.Errorf("Failed = %d, want 1", result.Failed)
	}
}

func TestRunFallsThroughToTheNextProviderOnError(t *testing.T) {
	store := newFakeStore()
	primary := &fakeProvider{name: models.DealProviderCreatorAPI, err: errors.New("throttled")}
	fallback := &fakeProvider{name: models.DealProviderScraper, candidates: []models.DealCandidate{
		candidate("B0FALLBK001", 60),
	}}

	service := NewService(store, nil, []DiscoveryProvider{primary, fallback},
		WithCategories(oneCategory()), WithSavingTiers([]int{50}))

	result, err := service.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.New != 1 {
		t.Fatalf("New = %d, want the fallback's result stored", result.New)
	}
	if got := result.ByProvider[models.DealProviderScraper]; got != 1 {
		t.Errorf("ByProvider = %v, want the deal attributed to the scraper", result.ByProvider)
	}
	if result.Failed != 0 {
		t.Errorf("Failed = %d; a query served by the fallback is not a failure", result.Failed)
	}
}

func TestRunPrefersThePrimaryProvider(t *testing.T) {
	// The scraper is a fallback, not a second source: it must not run when the
	// API answered.
	store := newFakeStore()
	primary := &fakeProvider{name: models.DealProviderCreatorAPI, candidates: []models.DealCandidate{
		candidate("B0PRIMARY01", 70),
	}}
	fallback := &fakeProvider{name: models.DealProviderScraper, candidates: []models.DealCandidate{
		candidate("B0SHOULDNT1", 70),
	}}

	service := NewService(store, nil, []DiscoveryProvider{primary, fallback},
		WithCategories(oneCategory()), WithSavingTiers([]int{50}))

	if _, err := service.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(fallback.queries) != 0 {
		t.Errorf("the fallback ran %d queries; it should stay idle while the API answers", len(fallback.queries))
	}
	if _, stored := store.deals["B0SHOULDNT1"]; stored {
		t.Error("the fallback's deal was stored despite the API answering")
	}
}

func TestRunFallsThroughWhenAProviderReturnsNothing(t *testing.T) {
	store := newFakeStore()
	empty := &fakeProvider{name: models.DealProviderCreatorAPI}
	fallback := &fakeProvider{name: models.DealProviderScraper, candidates: []models.DealCandidate{
		candidate("B0EMPTY0001", 60),
	}}

	service := NewService(store, nil, []DiscoveryProvider{empty, fallback},
		WithCategories(oneCategory()), WithSavingTiers([]int{50}))

	result, err := service.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.New != 1 {
		t.Errorf("New = %d, want the fallback consulted when the primary is empty", result.New)
	}
}

func TestRunTreatsAnEmptyMatrixCellAsNormal(t *testing.T) {
	// Every provider answering with nothing is a normal result for a steep tier
	// in a thin category, not a failure.
	store := newFakeStore()
	empty := &fakeProvider{name: models.DealProviderCreatorAPI}

	service := NewService(store, nil, []DiscoveryProvider{empty},
		WithCategories(oneCategory()), WithSavingTiers([]int{70}))

	result, err := service.Run(context.Background())
	if err != nil {
		t.Fatalf("an empty result should not be an error: %v", err)
	}
	if result.Failed != 0 || result.New != 0 {
		t.Errorf("result = %+v, want an empty but successful run", result)
	}
}

func TestRunAppliesTheScorer(t *testing.T) {
	store := newFakeStore()
	provider := &fakeProvider{name: models.DealProviderCreatorAPI, candidates: []models.DealCandidate{
		candidate("B0SCORED001", 70),
	}}

	service := NewService(store, nil, []DiscoveryProvider{provider},
		WithCategories(oneCategory()), WithSavingTiers([]int{50}),
		WithScorer(func(deal models.Deal) int { return deal.DiscountPercent + 5 }))

	if _, err := service.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := store.deals["B0SCORED001"].Score; got != 75 {
		t.Errorf("Score = %d, want the scorer's value", got)
	}
}

func TestRunStoresUnscoredWithoutAScorer(t *testing.T) {
	store := newFakeStore()
	provider := &fakeProvider{name: models.DealProviderCreatorAPI, candidates: []models.DealCandidate{
		candidate("B0UNSCORED1", 70),
	}}

	service := NewService(store, nil, []DiscoveryProvider{provider},
		WithCategories(oneCategory()), WithSavingTiers([]int{50}))

	if _, err := service.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := store.deals["B0UNSCORED1"].Score; got != 0 {
		t.Errorf("Score = %d, want 0 when no scorer is configured", got)
	}
}

func TestRunSurvivesAStoreFailure(t *testing.T) {
	store := newFakeStore()
	store.err = errors.New("database is down")
	provider := &fakeProvider{name: models.DealProviderCreatorAPI, candidates: []models.DealCandidate{
		candidate("B0STOREERR1", 70),
	}}

	service := NewService(store, nil, []DiscoveryProvider{provider},
		WithCategories(oneCategory()), WithSavingTiers([]int{50}))

	result, err := service.Run(context.Background())
	if err == nil {
		t.Fatal("a run that stored nothing should report an error")
	}
	if result.Failed == 0 {
		t.Error("the store failure should be counted")
	}
}

func TestRunRequiresProvidersAndCategories(t *testing.T) {
	store := newFakeStore()

	if _, err := NewService(store, nil, nil).Run(context.Background()); err == nil {
		t.Error("expected a service with no providers to refuse to run")
	}

	provider := &fakeProvider{name: models.DealProviderCreatorAPI}
	service := NewService(store, nil, []DiscoveryProvider{provider}, WithCategories(nil))
	if _, err := service.Run(context.Background()); err == nil {
		t.Error("expected an empty matrix to refuse to run")
	}
}

func TestRunStopsOnACancelledContext(t *testing.T) {
	store := newFakeStore()
	provider := &fakeProvider{name: models.DealProviderCreatorAPI, candidates: []models.DealCandidate{
		candidate("B0CANCEL001", 70),
	}}

	service := NewService(store, nil, []DiscoveryProvider{provider},
		WithCategories(oneCategory()), WithSavingTiers([]int{30, 50, 70}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := service.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if store.calls != 0 {
		t.Errorf("store called %d times after cancellation", store.calls)
	}
}
