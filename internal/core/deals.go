package core

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"post-gen/internal/deals"
	"post-gen/internal/deals/providers"
	"post-gen/internal/events"
	"post-gen/internal/models"
	"post-gen/internal/scraper"
)

// defaultDiscoveryQueryDelay paces one matrix query against the next. The
// Creators API rates per second as well as per day, and the product-lookup path
// draws on the same budget, so a discovery run walks rather than sprints.
const defaultDiscoveryQueryDelay = 1200 * time.Millisecond

// defaultAutoQueueLimit bounds one auto-queue pass. Each queued deal costs a
// product lookup, so a run that approved forty deals should not spend forty
// lookups in one burst.
const defaultAutoQueueLimit = 10

// ErrDealsUnavailable reports that deal storage is not configured. Deals live
// only in Postgres, so the JSON-fallback mode the CLI can run in has no place
// to put them.
var ErrDealsUnavailable = errors.New("deal discovery requires a database connection")

// ErrDiscoveryUnavailable reports that no Creators API credentials are
// configured, so there is nothing to discover with.
var ErrDiscoveryUnavailable = errors.New("deal discovery requires Creators API credentials")

// discoveryOnce guards lazy construction of the discovery service. It is built
// on first use rather than in NewEngine so the CLI and bot, which never
// discover, do not pay for credential loading at startup.
type discoveryState struct {
	once    sync.Once
	service *deals.Service
	err     error
}

// Deals lists stored deals, best-scoring first.
func (e *Engine) Deals(ctx context.Context, filter models.DealFilter) ([]models.Deal, error) {
	if e.db == nil {
		return nil, ErrDealsUnavailable
	}
	return e.db.ListDeals(ctx, filter)
}

// Deal returns one deal by ASIN, or nil when it is not stored.
func (e *Engine) Deal(ctx context.Context, asin string) (*models.Deal, error) {
	if e.db == nil {
		return nil, ErrDealsUnavailable
	}
	return e.db.GetDeal(ctx, asin)
}

// SetDealStatus moves a deal through the pipeline, reporting whether the ASIN
// was found.
func (e *Engine) SetDealStatus(ctx context.Context, asin, status string) (bool, error) {
	if e.db == nil {
		return false, ErrDealsUnavailable
	}
	return e.db.SetDealStatus(ctx, asin, status)
}

// QueueDeal pushes one deal into the publishing queue.
//
// It goes through AddQueuedProduct rather than writing queued_products
// directly, which re-fetches the product. That is deliberate: templates render
// feature bullets that discovery never collects, and the extra lookup also
// refreshes a price that may have moved since the deal was found.
//
// It returns nil when the ASIN is not stored, so the caller can answer 404.
// Queueing a deal that is already queued or posted is a no-op rather than an
// error, so a double-click costs nothing.
func (e *Engine) QueueDeal(ctx context.Context, asin string) (*models.Deal, error) {
	if e.db == nil {
		return nil, ErrDealsUnavailable
	}

	deal, err := e.db.GetDeal(ctx, asin)
	if err != nil {
		return nil, err
	}
	if deal == nil {
		return nil, nil
	}

	if deal.Status == models.DealQueued || deal.Status == models.DealPosted {
		return deal, nil
	}

	if err := e.AddQueuedProduct(ctx, deal.URL); err != nil {
		return nil, fmt.Errorf("queueing deal %s: %w", asin, err)
	}

	if _, err := e.db.SetDealStatus(ctx, asin, models.DealQueued); err != nil {
		return nil, err
	}
	deal.Status = models.DealQueued

	e.events.Emit(events.Event{
		Type:       events.DealQueued,
		Source:     events.SourceDiscovery,
		TraceID:    events.NewTraceID(),
		ProductURL: deal.URL,
		Message:    fmt.Sprintf("%s queued (%d%% off, score %d)", deal.Title, deal.DiscountPercent, deal.Score),
		Metadata: map[string]any{
			"asin":     deal.ASIN,
			"provider": deal.Provider,
			"category": deal.Category,
			"score":    deal.Score,
		},
	})

	return deal, nil
}

// QueueApprovedDeals queues deals that scoring has already approved, best first.
//
// limit bounds the work: each queued deal costs a product lookup, so a run that
// found forty approved deals should not spend forty lookups in one burst.
// It returns the ASINs actually queued.
func (e *Engine) QueueApprovedDeals(ctx context.Context, limit int) ([]string, error) {
	if e.db == nil {
		return nil, ErrDealsUnavailable
	}
	if limit <= 0 {
		limit = defaultAutoQueueLimit
	}

	approved, err := e.db.ListDeals(ctx, models.DealFilter{
		Status: models.DealApproved,
		Limit:  limit,
	})
	if err != nil {
		return nil, err
	}

	queued := make([]string, 0, len(approved))
	for _, deal := range approved {
		if err := ctx.Err(); err != nil {
			return queued, err
		}

		if _, err := e.QueueDeal(ctx, deal.ASIN); err != nil {
			// One product that will not scrape should not strand the rest of
			// the batch; it stays approved and is retried next time.
			log.Printf("[WARN] Could not queue deal %s: %v", deal.ASIN, err)
			continue
		}
		queued = append(queued, deal.ASIN)
	}

	return queued, nil
}

// DiscoverDeals runs one discovery pass across the query matrix.
//
// It is safe to call while a previous run is still going - the service holds no
// per-run state - but callers should avoid overlapping runs anyway, since both
// would draw on the same API quota.
func (e *Engine) DiscoverDeals(ctx context.Context) (*deals.Result, error) {
	service, err := e.discoveryService()
	if err != nil {
		return nil, err
	}

	result, err := service.Run(ctx)
	if err != nil {
		return &result, err
	}
	return &result, nil
}

// discoveryService builds the discovery service on first use.
func (e *Engine) discoveryService() (*deals.Service, error) {
	if e.db == nil {
		return nil, ErrDealsUnavailable
	}

	e.discovery.once.Do(func() {
		// nil fallback: discovery has no HTML equivalent yet. The Best Sellers
		// lister becomes the second provider when it lands, rather than a
		// fallback inside the API client.
		client := scraper.NewCreatorAPIClient(nil)
		if client == nil {
			e.discovery.err = ErrDiscoveryUnavailable
			return
		}

		e.discovery.service = deals.NewService(
			e.db,
			e.events,
			[]deals.DiscoveryProvider{providers.NewCreatorAPI(client, "")},
			// Pace the matrix so one run does not spend the per-second budget
			// the product-lookup path also draws on.
			deals.WithQueryDelay(defaultDiscoveryQueryDelay),
			deals.WithScorer(deals.Score),
		)
	})

	if e.discovery.err != nil {
		return nil, e.discovery.err
	}
	if e.discovery.service == nil {
		return nil, ErrDiscoveryUnavailable
	}

	return e.discovery.service, nil
}

// ValidateDealCategories resolves the configured browse nodes against the live
// API, so a stale node is caught deliberately rather than silently widening
// every search that uses it.
func (e *Engine) ValidateDealCategories(ctx context.Context) error {
	client := scraper.NewCreatorAPIClient(nil)
	if client == nil {
		return ErrDiscoveryUnavailable
	}

	if err := deals.ValidateCategories(ctx, client, deals.VerifiedCategories, ""); err != nil {
		return fmt.Errorf("validating deal categories: %w", err)
	}
	return nil
}
