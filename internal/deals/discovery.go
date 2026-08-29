package deals

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"post-gen/internal/events"
	"post-gen/internal/models"
)

// DiscoveryProvider is a source of deal candidates.
//
// The interface is declared here, where it is consumed, so the provider
// implementations never import this package back.
type DiscoveryProvider interface {
	// Name identifies the provider in stored deals and analytics.
	Name() string
	// Discover runs one cell of the query matrix.
	Discover(ctx context.Context, query models.DealQuery) ([]models.DealCandidate, error)
}

// Store is the persistence discovery needs. *db.Pool satisfies it.
type Store interface {
	UpsertDeal(ctx context.Context, deal models.Deal) (bool, error)
}

// PriceRecorder keeps a deal's observed prices, so scoring can measure a
// discount against a price the product really carried rather than against
// Amazon's list price. Optional: discovery works without one.
type PriceRecorder interface {
	RecordPriceObservation(ctx context.Context, asin string, price float64) (bool, error)
	ObservedHighs(ctx context.Context, asins []string, since time.Time) (map[string]float64, error)
}

// Scorer rates a deal so the queue decision can be made without a second pass.
// Discovery works without one, storing a score of zero.
type Scorer func(models.Deal) int

// Service runs discovery: it walks the query matrix across its providers,
// deduplicates, scores and stores what comes back.
type Service struct {
	providers []DiscoveryProvider
	store     Store
	events    *events.Logger

	categories  []Category
	tiers       []int
	marketplace string
	scorer      Scorer
	prices      PriceRecorder
	// priceWindow is how far back observed prices are trusted as a reference.
	priceWindow time.Duration

	// pause between queries, to pace a run against the API's rate limit.
	queryDelay time.Duration
}

// Option configures a Service.
type Option func(*Service)

// WithCategories replaces the categories the matrix is built from.
func WithCategories(categories []Category) Option {
	return func(s *Service) { s.categories = categories }
}

// WithSavingTiers replaces the discount floors each category is searched at.
func WithSavingTiers(tiers []int) Option {
	return func(s *Service) { s.tiers = tiers }
}

// WithMarketplace sets the storefront to search.
func WithMarketplace(marketplace string) Option {
	return func(s *Service) { s.marketplace = marketplace }
}

// WithScorer supplies the scoring function. Without one, deals are stored
// unscored for a later pass.
func WithScorer(scorer Scorer) Option {
	return func(s *Service) { s.scorer = scorer }
}

// WithPriceHistory records observed prices and scores against them.
//
// window is how far back a price is trusted as a reference: long enough to
// catch a pre-sale price, short enough that a year-old figure does not make a
// normal price look like a bargain.
func WithPriceHistory(recorder PriceRecorder, window time.Duration) Option {
	return func(s *Service) {
		s.prices = recorder
		if window > 0 {
			s.priceWindow = window
		}
	}
}

// WithQueryDelay paces the run. Zero runs queries back to back.
func WithQueryDelay(delay time.Duration) Option {
	return func(s *Service) { s.queryDelay = delay }
}

// NewService builds a discovery service. Providers are tried in order, so the
// primary source comes first and fallbacks after it.
func NewService(store Store, eventLog *events.Logger, providers []DiscoveryProvider, opts ...Option) *Service {
	service := &Service{
		providers:   providers,
		store:       store,
		events:      eventLog,
		categories:  VerifiedCategories,
		tiers:       DefaultSavingTiers,
		marketplace: "www.amazon.in",
		priceWindow: DefaultPriceWindow,
	}

	for _, opt := range opts {
		opt(service)
	}

	return service
}

// Result summarises one discovery run.
type Result struct {
	Queries    int `json:"queries"`
	Candidates int `json:"candidates"`
	New        int `json:"new"`
	Updated    int `json:"updated"`
	Failed     int `json:"failed"`
	// ByProvider counts stored deals per provider, which is the API-versus-
	// scraper split analytics reports.
	ByProvider map[string]int `json:"by_provider"`
	// Errors holds one entry per query that failed, for the operator.
	Errors []string `json:"errors,omitempty"`
	// ElapsedMS is milliseconds rather than a time.Duration, which would
	// serialise as an unreadable nanosecond count.
	ElapsedMS int64 `json:"elapsed_ms"`

	// Elapsed is the same figure for Go callers.
	Elapsed time.Duration `json:"-"`
}

// Run executes the whole query matrix once.
//
// A query that fails does not stop the run: providers are tried in order per
// query, and a throttle on one category should not cost the rest of the matrix.
// The run only fails outright if it could not store anything at all.
func (s *Service) Run(ctx context.Context) (Result, error) {
	started := time.Now()
	traceID := events.NewTraceID()

	result := Result{ByProvider: map[string]int{}}

	if len(s.providers) == 0 {
		return result, errors.New("discovery has no providers configured")
	}

	queries := BuildMatrix(s.categories, s.tiers)
	result.Queries = len(queries)
	if len(queries) == 0 {
		return result, errors.New("discovery matrix is empty; no categories configured")
	}

	s.emit(events.Event{
		Type:    events.DiscoveryStarted,
		TraceID: traceID,
		Message: fmt.Sprintf("Discovery started: %d queries across %d categories", len(queries), len(s.categories)),
	})

	// Deduplicate within the run. The same ASIN routinely appears under several
	// saving tiers of one category, and storing it once per sighting would
	// inflate the counts and emit a DEAL_UPDATED for work already done.
	seen := make(map[string]bool, len(queries)*10)

	for i, query := range queries {
		if err := ctx.Err(); err != nil {
			result.setElapsed(time.Since(started))
			return result, err
		}

		if s.queryDelay > 0 && i > 0 {
			select {
			case <-ctx.Done():
				result.setElapsed(time.Since(started))
				return result, ctx.Err()
			case <-time.After(s.queryDelay):
			}
		}

		candidates, provider, err := s.discoverOne(ctx, query)
		if err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s @%d%%: %v", query.Category, query.MinSavingPct, err))
			log.Printf("[WARN] Discovery: %s at %d%% failed: %v", query.Category, query.MinSavingPct, err)
			continue
		}

		result.Candidates += len(candidates)

		for _, candidate := range candidates {
			if candidate.ASIN == "" || seen[candidate.ASIN] {
				continue
			}
			seen[candidate.ASIN] = true

			created, err := s.storeCandidate(ctx, candidate, traceID)
			if err != nil {
				result.Failed++
				result.Errors = append(result.Errors, fmt.Sprintf("storing %s: %v", candidate.ASIN, err))
				continue
			}

			if created {
				result.New++
			} else {
				result.Updated++
			}
			result.ByProvider[provider]++
		}
	}

	result.setElapsed(time.Since(started))

	// A run where every query failed is a failure worth surfacing; one where
	// some succeeded is a normal partial result.
	if result.Failed > 0 && result.New == 0 && result.Updated == 0 {
		s.emit(events.Event{
			Type:     events.DiscoveryFailed,
			TraceID:  traceID,
			Message:  fmt.Sprintf("Discovery found nothing: all %d queries failed", result.Failed),
			Duration: result.Elapsed,
		})
		return result, fmt.Errorf("discovery failed: %s", result.Errors[0])
	}

	s.emit(events.Event{
		Type:     events.DiscoverySuccess,
		TraceID:  traceID,
		Message:  fmt.Sprintf("Discovery finished: %d new, %d updated, %d failed queries", result.New, result.Updated, result.Failed),
		Duration: result.Elapsed,
		Metadata: map[string]any{
			"queries":     result.Queries,
			"candidates":  result.Candidates,
			"new":         result.New,
			"updated":     result.Updated,
			"by_provider": result.ByProvider,
		},
	})

	return result, nil
}

// setElapsed records the run duration in both representations.
func (r *Result) setElapsed(d time.Duration) {
	r.Elapsed = d
	r.ElapsedMS = d.Milliseconds()
}

// discoverOne runs one query against the providers in order, returning the
// first non-empty result and the provider that served it.
//
// A provider that errors or returns nothing falls through to the next, which is
// what makes the HTML lister a fallback rather than a second source: it only
// runs when the API could not answer.
func (s *Service) discoverOne(ctx context.Context, query models.DealQuery) ([]models.DealCandidate, string, error) {
	var lastErr error

	for _, provider := range s.providers {
		candidates, err := provider.Discover(ctx, query)
		if err != nil {
			lastErr = err
			continue
		}
		if len(candidates) == 0 {
			continue
		}
		return candidates, provider.Name(), nil
	}

	if lastErr != nil {
		return nil, "", lastErr
	}

	// Every provider answered, none had anything. That is a normal result for a
	// steep saving tier in a thin category, not an error.
	return nil, "", nil
}

// storeCandidate scores and persists one candidate.
//
// A deal that clears the auto-queue threshold is stored as approved rather than
// queued: queueing re-fetches the product to collect the feature bullets the
// templates need, and doing that inline would turn a paced discovery run into
// dozens of extra lookups. Approved is the standing instruction to queue it;
// QueueApprovedDeals carries it out at its own pace.
func (s *Service) storeCandidate(ctx context.Context, candidate models.DealCandidate, traceID string) (bool, error) {
	deal := candidate.Deal()

	// Record the price before scoring, so a deal seen for the first time still
	// contributes the observation that will ground its next score.
	observedHigh := s.observePrice(ctx, deal)

	if s.scorer != nil {
		deal.Score = s.scorer(deal)
		// A scorer that can use observed prices supersedes the plain one. The
		// injected Scorer stays for callers that supply their own.
		if observedHigh > 0 {
			deal.Score = ScoreAgainst(deal, observedHigh)
		}
		if Decide(deal.Score) == DecisionQueue {
			deal.Status = models.DealApproved
		}
	}

	created, err := s.store.UpsertDeal(ctx, deal)
	if err != nil {
		return false, err
	}

	eventType := events.DealUpdated
	if created {
		eventType = events.DealDiscovered
	}

	s.emit(events.Event{
		Type:       eventType,
		TraceID:    traceID,
		ProductURL: deal.URL,
		Message:    fmt.Sprintf("%s (%d%% off, score %d)", deal.Title, deal.DiscountPercent, deal.Score),
		Metadata: map[string]any{
			"asin":     deal.ASIN,
			"provider": deal.Provider,
			"category": deal.Category,
			"score":    deal.Score,
			"discount": deal.DiscountPercent,
		},
	})

	return created, nil
}

// observePrice records the current price and returns the highest price seen in
// the trust window, or zero when there is no usable history.
func (s *Service) observePrice(ctx context.Context, deal models.Deal) float64 {
	if s.prices == nil || deal.Price <= 0 {
		return 0
	}

	if _, err := s.prices.RecordPriceObservation(ctx, deal.ASIN, deal.Price); err != nil {
		// Losing one observation costs a little accuracy later, not this run.
		log.Printf("[WARN] Discovery: could not record price for %s: %v", deal.ASIN, err)
	}

	highs, err := s.prices.ObservedHighs(ctx, []string{deal.ASIN}, time.Now().Add(-s.priceWindow))
	if err != nil {
		log.Printf("[WARN] Discovery: could not read price history for %s: %v", deal.ASIN, err)
		return 0
	}

	return highs[deal.ASIN]
}

// emit sends an event, defaulting the source to discovery.
func (s *Service) emit(event events.Event) {
	if s.events == nil {
		return
	}
	if event.Source == "" {
		event.Source = events.SourceDiscovery
	}
	s.events.Emit(event)
}
