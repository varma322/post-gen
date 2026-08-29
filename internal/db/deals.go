package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"post-gen/internal/models"
)

const dealColumns = `id, asin, title, url, category, image_url, price, old_price,
	discount_percent, score, provider, status, first_seen, last_seen, created_at`

// UpsertDeal stores a discovered deal, or refreshes one already known.
//
// created reports whether this was the first sighting, which is what separates
// a DEAL_DISCOVERED event from a DEAL_UPDATED one.
//
// first_seen is preserved across a re-sighting so the age of a deal stays true.
// Everything price-shaped is refreshed, because that is the part that moves.
//
// Status is preserved only where a decision has already been made about the
// deal: ignored, queued and posted all stand, so re-discovery cannot revive a
// rejected deal or rewrite the record of one that reached a page. A deal still
// sitting at new or approved is allowed to move between those two, so that a
// price drop can promote it and a price rise can demote it.
func (p *Pool) UpsertDeal(ctx context.Context, deal models.Deal) (created bool, err error) {
	if err := deal.Validate(); err != nil {
		return false, err
	}

	err = p.pool.QueryRow(ctx, `
		INSERT INTO deals (asin, title, url, category, image_url, price, old_price,
			discount_percent, score, provider, status, first_seen, last_seen)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT (asin) DO UPDATE SET
			title            = EXCLUDED.title,
			url              = EXCLUDED.url,
			category         = COALESCE(NULLIF(EXCLUDED.category, ''), deals.category),
			image_url        = COALESCE(NULLIF(EXCLUDED.image_url, ''), deals.image_url),
			price            = EXCLUDED.price,
			old_price        = EXCLUDED.old_price,
			discount_percent = EXCLUDED.discount_percent,
			score            = EXCLUDED.score,
			provider         = EXCLUDED.provider,
			status           = CASE
				WHEN deals.status IN ('ignored', 'queued', 'posted') THEN deals.status
				ELSE EXCLUDED.status
			END,
			last_seen        = CURRENT_TIMESTAMP
		RETURNING (xmax = 0)
	`,
		deal.ASIN, deal.Title, deal.URL, nullIfEmpty(deal.Category), nullIfEmpty(deal.ImageURL),
		deal.Price, deal.OldPrice, deal.DiscountPercent, deal.Score,
		deal.Provider, deal.Status,
	).Scan(&created)
	if err != nil {
		return false, fmt.Errorf("upserting deal %s: %w", deal.ASIN, err)
	}

	return created, nil
}

// ListDeals returns deals matching filter, best-scoring first.
func (p *Pool) ListDeals(ctx context.Context, filter models.DealFilter) ([]models.Deal, error) {
	var (
		conditions []string
		args       []any
	)

	add := func(clause string, value any) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(clause, len(args)))
	}

	if filter.Status != "" {
		add("status = $%d", filter.Status)
	}
	if filter.Category != "" {
		add("category = $%d", filter.Category)
	}
	if filter.Provider != "" {
		add("provider = $%d", filter.Provider)
	}
	if filter.MinScore > 0 {
		add("score >= $%d", filter.MinScore)
	}

	query := `SELECT ` + dealColumns + ` FROM deals`
	if len(conditions) > 0 {
		query += ` WHERE ` + strings.Join(conditions, " AND ")
	}
	query += ` ORDER BY score DESC, last_seen DESC`

	if filter.Limit > 0 {
		args = append(args, filter.Limit)
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}

	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying deals: %w", err)
	}
	defer rows.Close()

	deals := make([]models.Deal, 0, 32)
	for rows.Next() {
		deal, err := scanDeal(rows)
		if err != nil {
			return nil, err
		}
		deals = append(deals, deal)
	}

	return deals, rows.Err()
}

// GetDeal returns one deal by ASIN, or nil when it is not stored.
func (p *Pool) GetDeal(ctx context.Context, asin string) (*models.Deal, error) {
	row := p.pool.QueryRow(ctx, `SELECT `+dealColumns+` FROM deals WHERE asin = $1`, asin)

	deal, err := scanDeal(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &deal, nil
}

// SetDealStatus moves a deal through the pipeline. It reports whether a row
// actually changed, so a caller acting on a stale ASIN finds out.
func (p *Pool) SetDealStatus(ctx context.Context, asin, status string) (bool, error) {
	if !models.ValidDealStatus(status) {
		return false, fmt.Errorf("unknown deal status %q", status)
	}

	tag, err := p.pool.Exec(ctx, `UPDATE deals SET status = $1 WHERE asin = $2`, status, asin)
	if err != nil {
		return false, fmt.Errorf("setting status of deal %s: %w", asin, err)
	}

	return tag.RowsAffected() > 0, nil
}

// SetDealScore records a recomputed score without disturbing last_seen, so
// rescoring an existing catalog does not make stale deals look freshly seen.
func (p *Pool) SetDealScore(ctx context.Context, asin string, score int) (bool, error) {
	tag, err := p.pool.Exec(ctx, `UPDATE deals SET score = $1 WHERE asin = $2`, score, asin)
	if err != nil {
		return false, fmt.Errorf("setting score of deal %s: %w", asin, err)
	}

	return tag.RowsAffected() > 0, nil
}

// KnownASINs reports which of the given ASINs are already stored.
//
// The Best Sellers fallback calls this before fetching product pages: bestseller
// lists barely move between runs, so most candidates are already known and need
// no fetch at all.
func (p *Pool) KnownASINs(ctx context.Context, asins []string) (map[string]bool, error) {
	known := make(map[string]bool, len(asins))
	if len(asins) == 0 {
		return known, nil
	}

	rows, err := p.pool.Query(ctx, `SELECT asin FROM deals WHERE asin = ANY($1)`, asins)
	if err != nil {
		return nil, fmt.Errorf("querying known asins: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var asin string
		if err := rows.Scan(&asin); err != nil {
			return nil, fmt.Errorf("scanning known asin: %w", err)
		}
		known[asin] = true
	}

	return known, rows.Err()
}

// ExpireDealsNotSeenSince marks deals that have dropped out of discovery.
//
// Only deals still waiting to be acted on are expired. One already queued or
// posted has left discovery's hands, and rewriting its status would lose the
// record of what actually happened to it.
func (p *Pool) ExpireDealsNotSeenSince(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := p.pool.Exec(ctx, `
		UPDATE deals SET status = $1
		WHERE last_seen < $2 AND status IN ($3, $4)
	`, models.DealExpired, cutoff, models.DealNew, models.DealApproved)
	if err != nil {
		return 0, fmt.Errorf("expiring deals: %w", err)
	}

	return tag.RowsAffected(), nil
}

// DealCountsByProvider returns how many deals each discovery provider found,
// which is the Creators API versus scraper split the analytics page reports.
func (p *Pool) DealCountsByProvider(ctx context.Context) (map[string]int, error) {
	rows, err := p.pool.Query(ctx, `SELECT provider, COUNT(*) FROM deals GROUP BY provider`)
	if err != nil {
		return nil, fmt.Errorf("counting deals by provider: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int, 2)
	for rows.Next() {
		var provider string
		var count int
		if err := rows.Scan(&provider, &count); err != nil {
			return nil, fmt.Errorf("scanning provider count: %w", err)
		}
		counts[provider] = count
	}

	return counts, rows.Err()
}

// DealCountsByStatus returns how many deals sit at each pipeline stage.
func (p *Pool) DealCountsByStatus(ctx context.Context) (map[string]int, error) {
	rows, err := p.pool.Query(ctx, `SELECT status, COUNT(*) FROM deals GROUP BY status`)
	if err != nil {
		return nil, fmt.Errorf("counting deals by status: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int, 6)
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("scanning status count: %w", err)
		}
		counts[status] = count
	}

	return counts, rows.Err()
}

func scanDeal(row rowScanner) (models.Deal, error) {
	var (
		deal     models.Deal
		category *string
		imageURL *string
		price    *float64
		oldPrice *float64
	)

	err := row.Scan(
		&deal.ID, &deal.ASIN, &deal.Title, &deal.URL, &category, &imageURL,
		&price, &oldPrice, &deal.DiscountPercent, &deal.Score,
		&deal.Provider, &deal.Status, &deal.FirstSeen, &deal.LastSeen, &deal.CreatedAt,
	)
	if err != nil {
		return models.Deal{}, err
	}

	if category != nil {
		deal.Category = *category
	}
	if imageURL != nil {
		deal.ImageURL = *imageURL
	}
	if price != nil {
		deal.Price = *price
	}
	if oldPrice != nil {
		deal.OldPrice = *oldPrice
	}

	return deal, nil
}

// DealCategoryStats returns per-category counts and average scores, richest
// first, for the analytics panel.
func (p *Pool) DealCategoryStats(ctx context.Context) ([]models.CategoryDealStats, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT COALESCE(category, 'Uncategorised'),
		       COUNT(*),
		       COALESCE(AVG(score), 0),
		       COUNT(*) FILTER (WHERE status IN ($1, $2))
		FROM deals
		GROUP BY 1
		ORDER BY 2 DESC
	`, models.DealQueued, models.DealPosted)
	if err != nil {
		return nil, fmt.Errorf("querying deal category stats: %w", err)
	}
	defer rows.Close()

	stats := make([]models.CategoryDealStats, 0, 8)
	for rows.Next() {
		var row models.CategoryDealStats
		if err := rows.Scan(&row.Category, &row.Deals, &row.AverageScore, &row.Queued); err != nil {
			return nil, fmt.Errorf("scanning category stats: %w", err)
		}
		stats = append(stats, row)
	}

	return stats, rows.Err()
}

// RecordPriceObservation appends to a deal's price history, but only when the
// price actually moved.
//
// Discovery sees the same deal several times an hour, and a row per sighting
// would bury the handful that say something under thousands that repeat the
// last one. Recording only changes means the history is a list of moves, which
// is what reading it back needs it to be.
func (p *Pool) RecordPriceObservation(ctx context.Context, asin string, price float64) (recorded bool, err error) {
	if price <= 0 {
		return false, nil
	}

	// IS DISTINCT FROM does the work of both cases at once: no prior row yields
	// NULL, which is distinct from any price, so the first observation inserts
	// and an unchanged one does not. The casts are required because $1 appears
	// both as an inserted value and in a comparison, which Postgres will not
	// otherwise type.
	tag, err := p.pool.Exec(ctx, `
		INSERT INTO deal_price_history (asin, price)
		SELECT $1::varchar, $2::numeric
		WHERE (
			SELECT price FROM deal_price_history
			WHERE asin = $1::varchar
			ORDER BY observed_at DESC
			LIMIT 1
		) IS DISTINCT FROM $2::numeric
	`, asin, price)
	if err != nil {
		return false, fmt.Errorf("recording price for %s: %w", asin, err)
	}

	return tag.RowsAffected() > 0, nil
}

// PriceHistory returns a deal's observed prices since cutoff, oldest first.
func (p *Pool) PriceHistory(ctx context.Context, asin string, since time.Time) ([]models.PricePoint, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT price, observed_at
		FROM deal_price_history
		WHERE asin = $1 AND observed_at >= $2
		ORDER BY observed_at ASC
	`, asin, since)
	if err != nil {
		return nil, fmt.Errorf("querying price history for %s: %w", asin, err)
	}
	defer rows.Close()

	points := make([]models.PricePoint, 0, 8)
	for rows.Next() {
		var point models.PricePoint
		if err := rows.Scan(&point.Price, &point.ObservedAt); err != nil {
			return nil, fmt.Errorf("scanning price point: %w", err)
		}
		points = append(points, point)
	}

	return points, rows.Err()
}

// ObservedHighs returns the highest price observed for each of the given ASINs
// since cutoff.
//
// This is the honest reference price: it is what the product was actually seen
// selling for, as opposed to savingBasis, which is Amazon's list price and is
// frequently inflated well above anything anyone ever paid.
func (p *Pool) ObservedHighs(ctx context.Context, asins []string, since time.Time) (map[string]float64, error) {
	highs := make(map[string]float64, len(asins))
	if len(asins) == 0 {
		return highs, nil
	}

	rows, err := p.pool.Query(ctx, `
		SELECT asin, MAX(price), COUNT(*)
		FROM deal_price_history
		WHERE asin = ANY($1) AND observed_at >= $2
		GROUP BY asin
	`, asins, since)
	if err != nil {
		return nil, fmt.Errorf("querying observed highs: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			asin        string
			high        float64
			observations int
		)
		if err := rows.Scan(&asin, &high, &observations); err != nil {
			return nil, fmt.Errorf("scanning observed high: %w", err)
		}
		// One observation is the current price and says nothing about movement.
		if observations > 1 {
			highs[asin] = high
		}
	}

	return highs, rows.Err()
}
