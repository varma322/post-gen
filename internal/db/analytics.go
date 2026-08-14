package db

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"post-gen/internal/models"
)

// maxEventLimit bounds a single event query so a missing or absurd limit can't
// pull the whole log into memory.
const maxEventLimit = 500

// QueryEvents returns stored events newest-first, narrowed by filter.
func (p *Pool) QueryEvents(ctx context.Context, filter models.EventFilter) ([]models.Event, error) {
	limit := filter.Limit
	if limit <= 0 || limit > maxEventLimit {
		limit = 50
	}

	// Conditions are accumulated with positional parameters so no user input
	// is ever concatenated into the statement.
	var (
		conditions []string
		args       []any
	)
	add := func(clause string, value any) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(clause, len(args)))
	}

	if filter.Level != "" {
		add("level = $%d", strings.ToUpper(filter.Level))
	}
	if filter.Source != "" {
		add("source = $%d", filter.Source)
	}
	if filter.Account != "" {
		add("account_name = $%d", filter.Account)
	}
	if filter.Type != "" {
		add("event_type = $%d", strings.ToUpper(filter.Type))
	}
	if filter.Since != nil {
		add("created_at >= $%d", *filter.Since)
	}
	if filter.Search != "" {
		args = append(args, "%"+filter.Search+"%")
		conditions = append(conditions, fmt.Sprintf("(message ILIKE $%d OR product_url ILIKE $%d)", len(args), len(args)))
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	args = append(args, limit)
	query := fmt.Sprintf(`
		SELECT id, event_type, level, source, trace_id, account_name, product_url,
		       job_id, job_item_id, message, duration_ms, metadata, created_at
		FROM events
		%s
		ORDER BY id DESC
		LIMIT $%d
	`, where, len(args))

	return p.scanEvents(ctx, query, args...)
}

// EventsByTrace returns every event for one pipeline run, oldest first so the
// sequence reads in the order it happened.
func (p *Pool) EventsByTrace(ctx context.Context, traceID string) ([]models.Event, error) {
	return p.scanEvents(ctx, `
		SELECT id, event_type, level, source, trace_id, account_name, product_url,
		       job_id, job_item_id, message, duration_ms, metadata, created_at
		FROM events
		WHERE trace_id = $1
		ORDER BY id ASC
	`, traceID)
}

func (p *Pool) scanEvents(ctx context.Context, query string, args ...any) ([]models.Event, error) {
	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying events: %w", err)
	}
	defer rows.Close()

	events := make([]models.Event, 0, 50)
	for rows.Next() {
		var (
			e           models.Event
			account     *string
			productURL  *string
			message     *string
			metadataRaw []byte
		)

		if err := rows.Scan(&e.ID, &e.EventType, &e.Level, &e.Source, &e.TraceID,
			&account, &productURL, &e.JobID, &e.JobItemID, &message,
			&e.DurationMS, &metadataRaw, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning event: %w", err)
		}

		if account != nil {
			e.AccountName = *account
		}
		if productURL != nil {
			e.ProductURL = *productURL
		}
		if message != nil {
			e.Message = *message
		}
		if len(metadataRaw) > 0 {
			// Unreadable metadata shouldn't cost the whole row - the event's
			// type, timing, and message are the parts the UI depends on.
			_ = json.Unmarshal(metadataRaw, &e.Metadata)
		}

		events = append(events, e)
	}

	return events, rows.Err()
}

// DailyPublishCounts returns published-post totals per day over the window,
// with days that saw no posts present as explicit zeroes so the chart keeps a
// continuous x-axis instead of collapsing gaps.
func (p *Pool) DailyPublishCounts(ctx context.Context, days int) ([]models.DailyCount, error) {
	return p.dailySeries(ctx, days, `
		SELECT to_char(d.day, 'YYYY-MM-DD'), COALESCE(count(pp.id), 0)
		FROM generate_series(CURRENT_DATE - ($1::int - 1), CURRENT_DATE, '1 day') AS d(day)
		LEFT JOIN published_posts pp ON pp.created_at::date = d.day
		GROUP BY d.day ORDER BY d.day
	`)
}

// DailyFailureCounts is the failure counterpart, read from the event log since
// a failed publish never reaches published_posts.
func (p *Pool) DailyFailureCounts(ctx context.Context, days int) ([]models.DailyCount, error) {
	return p.dailySeries(ctx, days, `
		SELECT to_char(d.day, 'YYYY-MM-DD'), COALESCE(count(e.id), 0)
		FROM generate_series(CURRENT_DATE - ($1::int - 1), CURRENT_DATE, '1 day') AS d(day)
		LEFT JOIN events e ON e.created_at::date = d.day AND e.event_type = 'POST_FAILED'
		GROUP BY d.day ORDER BY d.day
	`)
}

func (p *Pool) dailySeries(ctx context.Context, days int, query string) ([]models.DailyCount, error) {
	if days <= 0 {
		days = 7
	}

	rows, err := p.pool.Query(ctx, query, days)
	if err != nil {
		return nil, fmt.Errorf("querying daily series: %w", err)
	}
	defer rows.Close()

	series := make([]models.DailyCount, 0, days)
	for rows.Next() {
		var d models.DailyCount
		if err := rows.Scan(&d.Date, &d.Count); err != nil {
			return nil, fmt.Errorf("scanning daily series: %w", err)
		}
		series = append(series, d)
	}
	return series, rows.Err()
}

// QueueHealth counts job items by state. Published is scoped to the last 24
// hours; the others are lifetime, matching how the panel reads.
func (p *Pool) QueueHealth(ctx context.Context) (models.QueueHealth, error) {
	var health models.QueueHealth
	err := p.pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE status = 'pending'),
			count(*) FILTER (WHERE status = 'publishing'),
			count(*) FILTER (WHERE status = 'published' AND published_at >= NOW() - INTERVAL '24 hours'),
			count(*) FILTER (WHERE status = 'failed'),
			count(*) FILTER (WHERE status = 'skipped')
		FROM job_items
	`).Scan(&health.Pending, &health.Publishing, &health.Published, &health.Failed, &health.Skipped)
	if err != nil {
		return health, fmt.Errorf("querying queue health: %w", err)
	}
	return health, nil
}

// AIStatsForWindow summarises enrichment outcomes from the event log.
func (p *Pool) AIStatsForWindow(ctx context.Context, days int) (models.AIStats, error) {
	var stats models.AIStats

	err := p.pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE event_type = 'AI_GENERATION_SUCCESS'),
			count(*) FILTER (WHERE event_type = 'AI_GENERATION_FAILED'),
			COALESCE(round(avg(duration_ms) FILTER (WHERE event_type = 'AI_GENERATION_SUCCESS')), 0)
		FROM events
		WHERE event_type IN ('AI_GENERATION_SUCCESS','AI_GENERATION_FAILED')
		  AND created_at >= CURRENT_DATE - ($1::int - 1)
	`, days).Scan(&stats.Success, &stats.Failed, &stats.AvgMS)
	if err != nil {
		return stats, fmt.Errorf("querying ai stats: %w", err)
	}
	stats.SuccessRate = successRate(stats.Success, stats.Failed)

	rows, err := p.pool.Query(ctx, `
		SELECT source,
		       count(*) FILTER (WHERE event_type = 'AI_GENERATION_SUCCESS'),
		       count(*) FILTER (WHERE event_type = 'AI_GENERATION_FAILED')
		FROM events
		WHERE event_type IN ('AI_GENERATION_SUCCESS','AI_GENERATION_FAILED')
		  AND created_at >= CURRENT_DATE - ($1::int - 1)
		GROUP BY source ORDER BY source
	`, days)
	if err != nil {
		return stats, fmt.Errorf("querying ai stats by provider: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var outcome models.ProviderOutcome
		if err := rows.Scan(&outcome.Provider, &outcome.Success, &outcome.Failed); err != nil {
			return stats, fmt.Errorf("scanning provider stats: %w", err)
		}
		stats.ByProvider = append(stats.ByProvider, outcome)
	}

	return stats, rows.Err()
}

// ScraperStatsForWindow summarises scrape outcomes, including how many
// successes came via the HTML fallback rather than the Creators API.
func (p *Pool) ScraperStatsForWindow(ctx context.Context, days int) (models.ScraperStats, error) {
	var stats models.ScraperStats

	err := p.pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE event_type = 'SCRAPE_SUCCESS'),
			count(*) FILTER (WHERE event_type = 'SCRAPE_FAILED'),
			count(*) FILTER (WHERE event_type = 'SCRAPE_SUCCESS' AND metadata->>'fallback_used' = 'true'),
			COALESCE(round(avg(duration_ms) FILTER (WHERE event_type = 'SCRAPE_SUCCESS')), 0)
		FROM events
		WHERE event_type IN ('SCRAPE_SUCCESS','SCRAPE_FAILED')
		  AND created_at >= CURRENT_DATE - ($1::int - 1)
	`, days).Scan(&stats.Success, &stats.Failed, &stats.FallbackUsed, &stats.AvgMS)
	if err != nil {
		return stats, fmt.Errorf("querying scraper stats: %w", err)
	}

	stats.SuccessRate = successRate(stats.Success, stats.Failed)
	return stats, nil
}

// ChannelStatsForWindow builds one row per configured account, joining publish
// history, pool size, and publish success rate.
func (p *Pool) ChannelStatsForWindow(ctx context.Context, days int) ([]models.ChannelStats, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT
			a.name,
			COALESCE(a.facebook_page_id, ''),
			a.active,
			a.max_posts_per_day,
			COALESCE(total.count, 0),
			COALESCE(today.count, 0),
			COALESCE(win.count, 0),
			COALESCE(prev.count, 0),
			COALESCE(pool.count, 0),
			COALESCE(ok.count, 0),
			COALESCE(bad.count, 0),
			last.published_at
		FROM accounts a
		LEFT JOIN (
			SELECT account_name, count(*) AS count FROM published_posts GROUP BY 1
		) total ON total.account_name = a.name
		LEFT JOIN (
			SELECT account_name, count(*) AS count FROM published_posts
			WHERE created_at >= CURRENT_DATE GROUP BY 1
		) today ON today.account_name = a.name
		LEFT JOIN (
			SELECT account_name, count(*) AS count FROM published_posts
			WHERE created_at >= CURRENT_DATE - ($1::int - 1) GROUP BY 1
		) win ON win.account_name = a.name
		LEFT JOIN (
			SELECT account_name, count(*) AS count FROM published_posts
			WHERE created_at >= CURRENT_DATE - ($1::int * 2 - 1)
			  AND created_at <  CURRENT_DATE - ($1::int - 1) GROUP BY 1
		) prev ON prev.account_name = a.name
		LEFT JOIN (
			SELECT al.account_name, count(*) AS count
			FROM account_links al
			WHERE NOT EXISTS (
				SELECT 1 FROM published_posts pp
				WHERE pp.account_name = al.account_name AND pp.product_url = al.url
			)
			GROUP BY 1
		) pool ON pool.account_name = a.name
		LEFT JOIN (
			SELECT account_name, count(*) AS count FROM events
			WHERE event_type = 'POST_SUCCESS' AND created_at >= CURRENT_DATE - ($1::int - 1)
			GROUP BY 1
		) ok ON ok.account_name = a.name
		LEFT JOIN (
			SELECT account_name, count(*) AS count FROM events
			WHERE event_type = 'POST_FAILED' AND created_at >= CURRENT_DATE - ($1::int - 1)
			GROUP BY 1
		) bad ON bad.account_name = a.name
		LEFT JOIN (
			SELECT DISTINCT ON (account_name) account_name, created_at AS published_at
			FROM published_posts ORDER BY account_name, created_at DESC
		) last ON last.account_name = a.name
		ORDER BY win.count DESC NULLS LAST, a.name ASC
	`, days)
	if err != nil {
		return nil, fmt.Errorf("querying channel stats: %w", err)
	}
	defer rows.Close()

	var channels []models.ChannelStats
	for rows.Next() {
		var (
			c         models.ChannelStats
			succeeded int
			failed    int
		)
		if err := rows.Scan(&c.AccountName, &c.FacebookPageID, &c.Active, &c.MaxPostsPerDay,
			&c.TotalPosts, &c.PostsToday, &c.PostsInWindow, &c.PreviousWindow,
			&c.QueueSize, &succeeded, &failed, &c.LastPublishAt); err != nil {
			return nil, fmt.Errorf("scanning channel stats: %w", err)
		}

		// With no publish attempts in the window there is no rate to report.
		// Showing 0% would read as "everything failed"; 100% is the honest
		// reading of "nothing has gone wrong".
		c.SuccessRate = 100
		if succeeded+failed > 0 {
			c.SuccessRate = successRate(succeeded, failed)
		}

		channels = append(channels, c)
	}

	return channels, rows.Err()
}

// DailyPublishCountsByAccount returns a per-account series for the heatmap and
// the per-channel sparklines, keyed by account name.
func (p *Pool) DailyPublishCountsByAccount(ctx context.Context, days int) (map[string][]models.DailyCount, error) {
	if days <= 0 {
		days = 7
	}

	rows, err := p.pool.Query(ctx, `
		SELECT a.name, to_char(d.day, 'YYYY-MM-DD'), COALESCE(count(pp.id), 0)
		FROM accounts a
		CROSS JOIN generate_series(CURRENT_DATE - ($1::int - 1), CURRENT_DATE, '1 day') AS d(day)
		LEFT JOIN published_posts pp
		       ON pp.account_name = a.name AND pp.created_at::date = d.day
		GROUP BY a.name, d.day
		ORDER BY a.name, d.day
	`, days)
	if err != nil {
		return nil, fmt.Errorf("querying per-account series: %w", err)
	}
	defer rows.Close()

	series := make(map[string][]models.DailyCount)
	for rows.Next() {
		var (
			account string
			day     models.DailyCount
		)
		if err := rows.Scan(&account, &day.Date, &day.Count); err != nil {
			return nil, fmt.Errorf("scanning per-account series: %w", err)
		}
		series[account] = append(series[account], day)
	}

	return series, rows.Err()
}

// CountsForSummary gathers the scalar KPIs in one round trip.
func (p *Pool) CountsForSummary(ctx context.Context, days int) (postsToday, postsPrevDay, postsWindow, postsPrevWindow, queueSize, failedPosts, activeItems, activeChannels int, err error) {
	err = p.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM published_posts WHERE created_at >= CURRENT_DATE),
			(SELECT count(*) FROM published_posts
			   WHERE created_at >= CURRENT_DATE - 1 AND created_at < CURRENT_DATE),
			(SELECT count(*) FROM published_posts WHERE created_at >= CURRENT_DATE - ($1::int - 1)),
			(SELECT count(*) FROM published_posts
			   WHERE created_at >= CURRENT_DATE - ($1::int * 2 - 1)
			     AND created_at <  CURRENT_DATE - ($1::int - 1)),
			(SELECT count(*) FROM queued_products WHERE status = 'queued'),
			(SELECT count(*) FROM job_items WHERE status = 'failed'),
			(SELECT count(*) FROM job_items WHERE status IN ('pending','publishing')),
			(SELECT count(*) FROM accounts WHERE active)
	`, days).Scan(&postsToday, &postsPrevDay, &postsWindow, &postsPrevWindow,
		&queueSize, &failedPosts, &activeItems, &activeChannels)
	if err != nil {
		err = fmt.Errorf("querying summary counts: %w", err)
	}
	return
}

// round1 rounds to one decimal place. math.Round is used rather than the
// truncating int(x*10+0.5) trick, which is wrong for negative values: a -25%
// change came out as -24.9 because int() truncates toward zero.
func round1(v float64) float64 {
	return math.Round(v*10) / 10
}

// successRate returns a percentage rounded to one decimal, and 0 when nothing
// has been attempted.
func successRate(success, failed int) float64 {
	total := success + failed
	if total == 0 {
		return 0
	}
	return round1(float64(success) / float64(total) * 100)
}

// NewDelta builds a period-over-period comparison, leaving PctChange nil when
// the previous window was empty - "up from zero" has no meaningful percentage.
func NewDelta(current, previous int) models.Delta {
	delta := models.Delta{Current: current, Previous: previous}
	if previous > 0 {
		pct := round1((float64(current) - float64(previous)) / float64(previous) * 100)
		delta.PctChange = &pct
	}
	return delta
}
