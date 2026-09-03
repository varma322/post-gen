package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"post-gen/internal/events"
	"post-gen/internal/models"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrJobAlreadyActive is returned by CreatePublicationJob when a partial unique
// index rejects the insert because another job is already pending/running.
// This closes the check-then-act race that an application-level check alone
// (GetActiveJob then CreatePublicationJob) cannot close.
var ErrJobAlreadyActive = errors.New("an auto-post job is already active")

// Pool is the shared database connection pool.
type Pool struct {
	pool *pgxpool.Pool
}

// New creates a connection pool from environment variables and runs migrations.
func New(ctx context.Context) (*Pool, error) {
	dsn := buildDSN()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parsing db config: %w", err)
	}

	config.MaxConns = 10
	config.MinConns = 2
	config.MaxConnLifetime = time.Hour
	config.AfterConnect = registerLocalTimestampCodec

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database ping failed: %w", err)
	}

	p := &Pool{pool: pool}
	if err := p.migrate(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return p, nil
}

// registerLocalTimestampCodec makes 'timestamp without time zone' columns decode
// into the server's local zone rather than being relabelled UTC.
//
// Every timestamp column here is 'without time zone', and both writers agree on
// what goes in: CURRENT_TIMESTAMP defaults store the Postgres session's local
// wall clock, and pgx discards the zone on a Go time.Time, storing that same
// local wall clock. Reading it back is where they diverged - pgx's default
// ScanLocation is nil, which stamps the decoded value UTC, so a post published
// at 12:02 IST was served as "12:02Z" and rendered by the browser as 17:32.
// The instant was wrong by exactly the local offset on every DB-sourced
// timestamp in the API, while the worker's in-memory status - marshalled
// straight from time.Now() with its real +05:30 - stayed correct, so the two
// disagreed on the same dashboard.
//
// This assumes the app and Postgres share a timezone, which is the same
// assumption the stored wall clocks already encode. Moving either one alone
// breaks that; the durable fix is timestamptz columns.
func registerLocalTimestampCodec(_ context.Context, conn *pgx.Conn) error {
	conn.TypeMap().RegisterType(&pgtype.Type{
		Name:  "timestamp",
		OID:   pgtype.TimestampOID,
		Codec: &pgtype.TimestampCodec{ScanLocation: time.Local},
	})
	return nil
}

// Close releases the connection pool.
func (p *Pool) Close() {
	p.pool.Close()
}

// migrate runs all schema setup DDL statements idempotently.
func (p *Pool) migrate(ctx context.Context) error {
	const schema = `
	CREATE TABLE IF NOT EXISTS accounts (
		id                   SERIAL PRIMARY KEY,
		name                 VARCHAR(255) UNIQUE NOT NULL,
		template_path        VARCHAR(255) NOT NULL,
		affiliate_tag        VARCHAR(255),
		facebook_page_id     VARCHAR(255),
		facebook_access_token TEXT,
		use_ai               BOOLEAN DEFAULT TRUE,
		ai_prompt            TEXT,
		active               BOOLEAN NOT NULL DEFAULT TRUE,
		extra_params         JSONB,
		max_posts_per_day    INT NOT NULL DEFAULT 0,
		active_hours_start   TEXT NOT NULL DEFAULT '',
		active_hours_end     TEXT NOT NULL DEFAULT '',
		min_delay_minutes    INT NOT NULL DEFAULT 0,
		created_at           TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at           TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- Additive migrations for columns introduced after the initial table creation,
	-- so existing installs pick them up without losing data.
	ALTER TABLE accounts ADD COLUMN IF NOT EXISTS active BOOLEAN NOT NULL DEFAULT TRUE;
	ALTER TABLE accounts ADD COLUMN IF NOT EXISTS extra_params JSONB;
	ALTER TABLE accounts ADD COLUMN IF NOT EXISTS max_posts_per_day INT NOT NULL DEFAULT 0;
	ALTER TABLE accounts ADD COLUMN IF NOT EXISTS active_hours_start TEXT NOT NULL DEFAULT '';
	ALTER TABLE accounts ADD COLUMN IF NOT EXISTS active_hours_end TEXT NOT NULL DEFAULT '';
	ALTER TABLE accounts ADD COLUMN IF NOT EXISTS min_delay_minutes INT NOT NULL DEFAULT 0;

	CREATE OR REPLACE FUNCTION update_updated_at_column()
	RETURNS TRIGGER AS $$
	BEGIN
		NEW.updated_at = CURRENT_TIMESTAMP;
		RETURN NEW;
	END;
	$$ language 'plpgsql';

	DROP TRIGGER IF EXISTS update_accounts_updated_at ON accounts;
	CREATE TRIGGER update_accounts_updated_at
		BEFORE UPDATE ON accounts
		FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

	CREATE TABLE IF NOT EXISTS published_posts (
		id                   SERIAL PRIMARY KEY,
		account_name         VARCHAR(255) NOT NULL,
		facebook_page_id     VARCHAR(255) NOT NULL,
		facebook_post_id     VARCHAR(255) NOT NULL,
		product_title        TEXT,
		product_url          TEXT,
		content              TEXT,
		created_at           TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_published_posts_account_name ON published_posts(account_name);
	CREATE INDEX IF NOT EXISTS idx_published_posts_created_at ON published_posts(created_at);

	CREATE TABLE IF NOT EXISTS queued_products (
		id                   SERIAL PRIMARY KEY,
		url                  TEXT UNIQUE NOT NULL,
		title                TEXT NOT NULL,
		price                VARCHAR(255),
		image_url            TEXT,
		scraped_data         JSONB NOT NULL,
		status               VARCHAR(50) DEFAULT 'queued',
		created_at           TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at           TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS publication_jobs (
		id                   SERIAL PRIMARY KEY,
		status               VARCHAR(50) DEFAULT 'pending',
		created_at           TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at           TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS job_items (
		id                   SERIAL PRIMARY KEY,
		job_id               INT REFERENCES publication_jobs(id) ON DELETE CASCADE,
		account_name         VARCHAR(255) NOT NULL,
		product_url          TEXT NOT NULL,
		status               VARCHAR(50) DEFAULT 'pending',
		error_message        TEXT,
		published_at         TIMESTAMP,
		created_at           TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at           TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	ALTER TABLE job_items ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP;

	DROP TRIGGER IF EXISTS update_job_items_updated_at ON job_items;
	CREATE TRIGGER update_job_items_updated_at
		BEFORE UPDATE ON job_items
		FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

	-- At most one publication_jobs row may be pending/running at a time. This is
	-- the real fix for the TriggerAutoPostJob check-then-act race: an in-process
	-- check (GetActiveJob then INSERT) can't close the window between two
	-- concurrent requests, but a database constraint is atomic regardless of
	-- timing or how many server processes share this database.
	CREATE UNIQUE INDEX IF NOT EXISTS idx_publication_jobs_single_active
		ON publication_jobs ((1))
		WHERE status IN ('pending', 'running');

	CREATE INDEX IF NOT EXISTS idx_queued_products_status ON queued_products(status);
	CREATE INDEX IF NOT EXISTS idx_job_items_job_id ON job_items(job_id);
	CREATE INDEX IF NOT EXISTS idx_job_items_status ON job_items(status);

	-- Each account can maintain its own dedicated pool of links, separate from
	-- the shared queued_products pool. A link's availability as a candidate is
	-- derived from published_posts (has it ever been posted for this account?)
	-- rather than a stored status flag, so it never needs to be reconciled with
	-- job outcomes - the same pattern queued_products/CandidateProductURLsForAccount
	-- already uses.
	CREATE TABLE IF NOT EXISTS account_links (
		id                   SERIAL PRIMARY KEY,
		account_name         VARCHAR(255) NOT NULL,
		url                  TEXT NOT NULL,
		created_at           TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE (account_name, url)
	);

	CREATE INDEX IF NOT EXISTS idx_account_links_account_name ON account_links(account_name);

	-- Append-only pipeline event log. Every stage writes here; nothing ever
	-- updates a row. Events sharing a trace_id belong to one pipeline run, so
	-- the full history of a single post is one indexed lookup.
	--
	-- Queue history and generation history are deliberately NOT separate
	-- tables: both are derivable by joining this log against job_items and
	-- published_posts, and a derived view can never drift from what actually
	-- happened the way a stored status column can.
	CREATE TABLE IF NOT EXISTS events (
		id                   BIGSERIAL PRIMARY KEY,
		event_type           VARCHAR(40) NOT NULL,
		level                VARCHAR(8)  NOT NULL,
		source               VARCHAR(30) NOT NULL,
		trace_id             TEXT        NOT NULL,
		account_name         VARCHAR(255),
		product_url          TEXT,
		job_id               INT,
		job_item_id          INT,
		message              TEXT,
		duration_ms          INT,
		metadata             JSONB,
		created_at           TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_events_created_at      ON events(created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_events_level_created   ON events(level, created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_events_type_created    ON events(event_type, created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_events_account_created ON events(account_name, created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_events_trace           ON events(trace_id);

	-- Facebook's own permalink for a published post. Reconstructing it from
	-- facebook_post_id produces a dead URL for pages on the New Pages
	-- Experience, whose permalinks use a different actor id than the page id.
	ALTER TABLE published_posts ADD COLUMN IF NOT EXISTS permalink TEXT;

	-- Runtime configuration editable from the Settings screen. Values here
	-- override the equivalent environment variable; anything absent falls back
	-- to the environment and then to a built-in default, so an install that
	-- never touches this table behaves exactly as it did before.
	CREATE TABLE IF NOT EXISTS settings (
		key                  VARCHAR(64) PRIMARY KEY,
		value                JSONB NOT NULL,
		updated_at           TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- Named recurring triggers for the auto-post pipeline.
	--
	-- Deliberately not cron: two shapes cover what this pipeline actually
	-- needs - "every N minutes" and "daily at HH:MM" - and they can be
	-- rendered and edited in a form without teaching anyone cron syntax or
	-- pulling in an expression parser.
	CREATE TABLE IF NOT EXISTS job_schedules (
		id                   SERIAL PRIMARY KEY,
		name                 VARCHAR(255) NOT NULL,
		kind                 VARCHAR(16) NOT NULL,
		interval_minutes     INT NOT NULL DEFAULT 0,
		daily_at             TEXT NOT NULL DEFAULT '',
		rotate_old_links     BOOLEAN NOT NULL DEFAULT FALSE,
		enabled              BOOLEAN NOT NULL DEFAULT TRUE,
		next_run_at          TIMESTAMP,
		last_run_at          TIMESTAMP,
		last_job_id          INT,
		last_error           TEXT,
		created_at           TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at           TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_job_schedules_due ON job_schedules(enabled, next_run_at);

	DROP TRIGGER IF EXISTS update_job_schedules_updated_at ON job_schedules;
	CREATE TRIGGER update_job_schedules_updated_at
		BEFORE UPDATE ON job_schedules
		FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

	-- A schedule's kind says when it fires; task says what it does. Existing
	-- rows all predate discovery, so they default to the auto-post pipeline.
	ALTER TABLE job_schedules ADD COLUMN IF NOT EXISTS task VARCHAR(32) NOT NULL DEFAULT 'auto_post';

	-- Jobs gain a name so a run is identifiable in the scheduler, and record
	-- which schedule produced them.
	ALTER TABLE publication_jobs ADD COLUMN IF NOT EXISTS name TEXT NOT NULL DEFAULT '';
	ALTER TABLE publication_jobs ADD COLUMN IF NOT EXISTS schedule_id INT;

	-- Deals discovered by the Creators API searchItems path, or by the Best
	-- Sellers HTML listing fallback. asin is UNIQUE because discovery upserts
	-- on it: a deal seen again refreshes its price and last_seen rather than
	-- being stored twice.
	CREATE TABLE IF NOT EXISTS deals (
		id                   SERIAL PRIMARY KEY,
		asin                 VARCHAR(16) UNIQUE NOT NULL,
		title                TEXT NOT NULL,
		url                  TEXT NOT NULL,
		category             VARCHAR(64),
		image_url            TEXT,
		price                NUMERIC(12,2),
		old_price            NUMERIC(12,2),
		discount_percent     INT NOT NULL DEFAULT 0,
		score                INT NOT NULL DEFAULT 0,
		provider             VARCHAR(32) NOT NULL,
		status               VARCHAR(32) NOT NULL DEFAULT 'new',
		first_seen           TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		last_seen            TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		created_at           TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at           TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_deals_status_score ON deals(status, score DESC);
	CREATE INDEX IF NOT EXISTS idx_deals_last_seen ON deals(last_seen);

	-- One row per observed price change, not per sighting: discovery sees the
	-- same deal several times an hour and storing every look would bury the
	-- handful of rows that actually say something.
	CREATE TABLE IF NOT EXISTS deal_price_history (
		id                   SERIAL PRIMARY KEY,
		asin                 VARCHAR(16) NOT NULL,
		price                NUMERIC(12,2) NOT NULL,
		observed_at          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_deal_price_history_asin
		ON deal_price_history(asin, observed_at DESC);

	DROP TRIGGER IF EXISTS update_deals_updated_at ON deals;
	CREATE TRIGGER update_deals_updated_at
		BEFORE UPDATE ON deals
		FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
	`
	_, err := p.pool.Exec(ctx, schema)
	return err
}

// LoadAccounts retrieves all accounts from the database.
func (p *Pool) LoadAccounts(ctx context.Context) ([]models.Account, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT name, template_path, affiliate_tag, facebook_page_id, facebook_access_token, use_ai, ai_prompt, active, extra_params,
			max_posts_per_day, active_hours_start, active_hours_end, min_delay_minutes
		FROM accounts ORDER BY id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("querying accounts: %w", err)
	}
	defer rows.Close()

	var accounts []models.Account
	for rows.Next() {
		var a models.Account
		var active bool
		var extraParamsJSON []byte
		if err := rows.Scan(
			&a.Name,
			&a.TemplatePath,
			&a.AffiliateTag,
			&a.FacebookPageID,
			&a.FacebookAccessToken,
			&a.UseAI,
			&a.AIPrompt,
			&active,
			&extraParamsJSON,
			&a.MaxPostsPerDay,
			&a.ActiveHoursStart,
			&a.ActiveHoursEnd,
			&a.MinDelayMinutes,
		); err != nil {
			return nil, fmt.Errorf("scanning account row: %w", err)
		}
		a.Active = &active
		if len(extraParamsJSON) > 0 {
			if err := json.Unmarshal(extraParamsJSON, &a.ExtraParams); err != nil {
				return nil, fmt.Errorf("unmarshaling extra_params for account %q: %w", a.Name, err)
			}
		}
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}

// UpsertAccount inserts or updates an account by name.
func (p *Pool) UpsertAccount(ctx context.Context, a models.Account) error {
	var extraParamsJSON []byte
	if len(a.ExtraParams) > 0 {
		var err error
		extraParamsJSON, err = json.Marshal(a.ExtraParams)
		if err != nil {
			return fmt.Errorf("marshaling extra_params for account %q: %w", a.Name, err)
		}
	}

	_, err := p.pool.Exec(ctx, `
		INSERT INTO accounts (name, template_path, affiliate_tag, facebook_page_id, facebook_access_token, use_ai, ai_prompt, active, extra_params,
			max_posts_per_day, active_hours_start, active_hours_end, min_delay_minutes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (name) DO UPDATE SET
			template_path        = EXCLUDED.template_path,
			affiliate_tag        = EXCLUDED.affiliate_tag,
			facebook_page_id     = EXCLUDED.facebook_page_id,
			facebook_access_token = EXCLUDED.facebook_access_token,
			use_ai               = EXCLUDED.use_ai,
			ai_prompt            = EXCLUDED.ai_prompt,
			active               = EXCLUDED.active,
			extra_params         = EXCLUDED.extra_params,
			max_posts_per_day    = EXCLUDED.max_posts_per_day,
			active_hours_start   = EXCLUDED.active_hours_start,
			active_hours_end     = EXCLUDED.active_hours_end,
			min_delay_minutes    = EXCLUDED.min_delay_minutes
	`,
		a.Name, a.TemplatePath, a.AffiliateTag,
		a.FacebookPageID, a.FacebookAccessToken,
		a.UseAI, a.AIPrompt, a.IsActive(), extraParamsJSON,
		a.MaxPostsPerDay, a.ActiveHoursStart, a.ActiveHoursEnd, a.MinDelayMinutes,
	)
	return err
}

// SaveAccounts replaces all accounts by upserting each one.
// Legacy accounts no longer in the list are NOT deleted to preserve safety.
func (p *Pool) SaveAccounts(ctx context.Context, accounts []models.Account) error {
	for _, a := range accounts {
		if err := p.UpsertAccount(ctx, a); err != nil {
			return fmt.Errorf("upserting account %q: %w", a.Name, err)
		}
	}
	return nil
}

// DeleteAccount removes an account by name.
func (p *Pool) DeleteAccount(ctx context.Context, name string) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM accounts WHERE name = $1`, name)
	if err != nil {
		return fmt.Errorf("deleting account %q: %w", name, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("account %q not found", name)
	}
	return nil
}

// Count returns the number of accounts stored.
func (p *Pool) Count(ctx context.Context) (int, error) {
	var n int
	err := p.pool.QueryRow(ctx, `SELECT COUNT(*) FROM accounts`).Scan(&n)
	return n, err
}

// buildDSN constructs a PostgreSQL DSN from environment variables.
func buildDSN() string {
	host := getenv("DB_HOST", "127.0.0.1")
	port := getenv("DB_PORT", "5432")
	user := getenv("DB_USER", "postgres")
	pass := getenv("DB_PASSWORD", "")
	name := getenv("DB_NAME", "postgen")
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", user, pass, host, port, name)
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// RecordPublishedPost saves a post publication record.
func (p *Pool) RecordPublishedPost(ctx context.Context, post models.PublishedPost) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO published_posts (account_name, facebook_page_id, facebook_post_id, product_title, product_url, content, permalink, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`,
		post.AccountName,
		post.FacebookPageID,
		post.FacebookPostID,
		post.ProductTitle,
		post.ProductURL,
		post.Content,
		nullIfEmpty(post.Permalink),
		post.CreatedAt,
	)
	return err
}

// GetStats returns aggregated stats for the dashboard.
func (p *Pool) GetStats(ctx context.Context, limit int) (*models.Stats, error) {
	stats := &models.Stats{
		AccountStats: []models.AccountStats{},
		RecentPosts:  []models.PublishedPost{},
	}

	// 1. Total Posts
	err := p.pool.QueryRow(ctx, `SELECT COUNT(*) FROM published_posts`).Scan(&stats.TotalPosts)
	if err != nil {
		return nil, fmt.Errorf("querying total posts: %w", err)
	}

	// 2. Posts Today
	err = p.pool.QueryRow(ctx, `SELECT COUNT(*) FROM published_posts WHERE created_at >= CURRENT_DATE`).Scan(&stats.PostsToday)
	if err != nil {
		return nil, fmt.Errorf("querying posts today: %w", err)
	}

	// 3. Per Account Stats
	rows, err := p.pool.Query(ctx, `
		SELECT account_name, COUNT(*), COUNT(*) FILTER (WHERE created_at >= CURRENT_DATE)
		FROM published_posts
		GROUP BY account_name
		ORDER BY account_name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("querying account stats: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var ast models.AccountStats
		if err := rows.Scan(&ast.AccountName, &ast.TotalPosts, &ast.PostsToday); err != nil {
			return nil, fmt.Errorf("scanning account stats: %w", err)
		}
		stats.AccountStats = append(stats.AccountStats, ast)
	}

	// 4. Recent Posts
	rows2, err := p.pool.Query(ctx, `
		SELECT id, account_name, facebook_page_id, facebook_post_id, product_title, product_url, content, created_at
		FROM published_posts
		ORDER BY created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("querying recent posts: %w", err)
	}
	defer rows2.Close()

	for rows2.Next() {
		var pst models.PublishedPost
		if err := rows2.Scan(&pst.ID, &pst.AccountName, &pst.FacebookPageID, &pst.FacebookPostID, &pst.ProductTitle, &pst.ProductURL, &pst.Content, &pst.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning recent post: %w", err)
		}
		stats.RecentPosts = append(stats.RecentPosts, pst)
	}

	return stats, nil
}

// AddQueuedProduct inserts a new product into the queue.
func (p *Pool) AddQueuedProduct(ctx context.Context, url, title, price, imgURL string, scrapedData models.Product) error {
	scrapedJSON, err := json.Marshal(scrapedData)
	if err != nil {
		return fmt.Errorf("marshaling scraped data: %w", err)
	}

	_, err = p.pool.Exec(ctx, `
		INSERT INTO queued_products (url, title, price, image_url, scraped_data)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (url) DO UPDATE SET
			title = EXCLUDED.title,
			price = EXCLUDED.price,
			image_url = EXCLUDED.image_url,
			scraped_data = EXCLUDED.scraped_data,
			status = 'queued',
			updated_at = CURRENT_TIMESTAMP
	`, url, title, price, imgURL, scrapedJSON)
	return err
}

// GetQueuedProducts retrieves all active queued products.
func (p *Pool) GetQueuedProducts(ctx context.Context) ([]models.QueuedProduct, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, url, title, price, image_url, scraped_data, status, created_at
		FROM queued_products
		WHERE status = 'queued'
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var products []models.QueuedProduct
	for rows.Next() {
		var qp models.QueuedProduct
		var scrapedJSON []byte
		if err := rows.Scan(&qp.ID, &qp.URL, &qp.Title, &qp.Price, &qp.ImageURL, &scrapedJSON, &qp.Status, &qp.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(scrapedJSON, &qp.ScrapedData); err != nil {
			return nil, err
		}
		products = append(products, qp)
	}
	return products, nil
}

// DeleteQueuedProduct deletes a product from the queue by ID.
func (p *Pool) DeleteQueuedProduct(ctx context.Context, id int) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM queued_products WHERE id = $1`, id)
	return err
}

// CreatePublicationJob creates a new publication job and inserts pending job items.
// Returns ErrJobAlreadyActive if the idx_publication_jobs_single_active partial
// unique index rejects the insert because another job is already active - this
// is the authoritative check; any prior application-level GetActiveJob check is
// just a fast-path for a friendlier error message.
func (p *Pool) CreatePublicationJob(ctx context.Context, items []models.JobItem) (int, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var jobID int
	err = tx.QueryRow(ctx, `
		INSERT INTO publication_jobs (status) VALUES ('pending') RETURNING id
	`).Scan(&jobID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return 0, ErrJobAlreadyActive
		}
		return 0, err
	}

	for _, item := range items {
		_, err = tx.Exec(ctx, `
			INSERT INTO job_items (job_id, account_name, product_url, status)
			VALUES ($1, $2, $3, 'pending')
		`, jobID, item.AccountName, item.ProductURL)
		if err != nil {
			return 0, err
		}
	}

	return jobID, tx.Commit(ctx)
}

// GetActiveJob retrieves the running or pending publication job with its items.
// It returns (nil, nil) when no job is active - distinct from (nil, err), which
// signals a genuine database error. Callers must not conflate the two.
func (p *Pool) GetActiveJob(ctx context.Context) (*models.PublicationJob, error) {
	var job models.PublicationJob
	err := p.pool.QueryRow(ctx, `
		SELECT id, status, created_at FROM publication_jobs
		WHERE status IN ('pending', 'running')
		ORDER BY id DESC LIMIT 1
	`).Scan(&job.ID, &job.Status, &job.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	rows, err := p.pool.Query(ctx, `
		SELECT id, job_id, account_name, product_url, status, COALESCE(error_message, ''), published_at, created_at
		FROM job_items
		WHERE job_id = $1
		ORDER BY id ASC
	`, job.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var item models.JobItem
		if err := rows.Scan(&item.ID, &item.JobID, &item.AccountName, &item.ProductURL, &item.Status, &item.ErrorMessage, &item.PublishedAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		job.Items = append(job.Items, item)
	}

	return &job, nil
}

// UpdateJobStatus updates the general status of a publication job.
func (p *Pool) UpdateJobStatus(ctx context.Context, jobID int, status string) error {
	_, err := p.pool.Exec(ctx, `
		UPDATE publication_jobs SET status = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2
	`, status, jobID)
	return err
}

// UpdateJobItemStatus updates a single job item status, timestamp, and optional error.
func (p *Pool) UpdateJobItemStatus(ctx context.Context, itemID int, status, errorMsg string, publishedAt *time.Time) error {
	_, err := p.pool.Exec(ctx, `
		UPDATE job_items
		SET status = $1, error_message = $2, published_at = $3
		WHERE id = $4
	`, status, errorMsg, publishedAt, itemID)
	return err
}

// CancelActiveJobs cancels all currently active (pending or running) jobs.
// Items already in 'publishing' are also marked 'skipped' here, but that alone
// doesn't stop a publish already in flight in the worker goroutine - the worker
// re-checks GetJobItemStatus immediately before its point of no return (the
// actual Facebook API call) and aborts if it sees this cancellation.
func (p *Pool) CancelActiveJobs(ctx context.Context) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Update active jobs to cancelled
	_, err = tx.Exec(ctx, `
		UPDATE publication_jobs SET status = 'cancelled', updated_at = CURRENT_TIMESTAMP
		WHERE status IN ('pending', 'running')
	`)
	if err != nil {
		return err
	}

	// Update pending and in-flight items to skipped.
	_, err = tx.Exec(ctx, `
		UPDATE job_items SET status = 'skipped', error_message = 'Job cancelled by user'
		WHERE status IN ('pending', 'publishing')
	`)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// GetJobItemStatus returns the current status of a job item. The worker uses
// this to detect if an item was cancelled while its publish was in flight,
// immediately before committing to the actual Facebook API call.
func (p *Pool) GetJobItemStatus(ctx context.Context, itemID int) (string, error) {
	var status string
	err := p.pool.QueryRow(ctx, `SELECT status FROM job_items WHERE id = $1`, itemID).Scan(&status)
	return status, err
}

// RecoverStaleJobItems marks job items stuck in 'publishing' for longer than
// staleAfter as 'failed'. This handles a worker crash/restart between marking
// an item 'publishing' and recording its terminal outcome: without this, such
// an item is never revisited, yet the job it belongs to still gets marked
// 'completed' once no 'pending' items remain, silently masking whether that
// item was ever actually posted. Returns the number of items recovered.
func (p *Pool) RecoverStaleJobItems(ctx context.Context, staleAfter time.Duration) (int64, error) {
	tag, err := p.pool.Exec(ctx, `
		UPDATE job_items
		SET status = 'failed', error_message = 'Recovered: stuck in publishing status after a worker restart; outcome unknown'
		WHERE status = 'publishing' AND updated_at < $1
	`, time.Now().Add(-staleAfter))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// CandidateProductURLsForAccount returns the URLs of queued products the given
// account has not published yet, newest first.
//
// URLs only, deliberately. The caller builds job items keyed by URL and reads
// nothing else off the row, but this used to select the whole record - including
// scraped_data, a full product JSON document - and unmarshal every one of them
// to reach a single string. That cost ran per row, per active account, on every
// job trigger.
func (p *Pool) CandidateProductURLsForAccount(ctx context.Context, accountName string) ([]string, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT url
		FROM queued_products
		WHERE status = 'queued'
		  AND url NOT IN (
		      SELECT product_url FROM published_posts WHERE account_name = $1
		  )
		ORDER BY created_at DESC
	`, accountName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var urls []string
	for rows.Next() {
		var url string
		if err := rows.Scan(&url); err != nil {
			return nil, err
		}
		urls = append(urls, url)
	}
	return urls, rows.Err()
}

// AddAccountLink adds a URL to an account's dedicated link pool. A duplicate
// (account_name, url) pair is silently ignored rather than treated as an error,
// since re-pasting the same batch of links is a normal, harmless user action.
func (p *Pool) AddAccountLink(ctx context.Context, accountName, url string) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO account_links (account_name, url) VALUES ($1, $2)
		ON CONFLICT (account_name, url) DO NOTHING
	`, accountName, url)
	return err
}

// GetAccountLinks returns every link in an account's pool, newest first, each
// flagged with whether it has already been published for that account.
func (p *Pool) GetAccountLinks(ctx context.Context, accountName string) ([]models.AccountLink, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT al.id, al.account_name, al.url, al.created_at,
		       EXISTS (
		           SELECT 1 FROM published_posts pp
		           WHERE pp.account_name = al.account_name AND pp.product_url = al.url
		       ) AS posted
		FROM account_links al
		WHERE al.account_name = $1
		ORDER BY al.created_at DESC
	`, accountName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var links []models.AccountLink
	for rows.Next() {
		var link models.AccountLink
		if err := rows.Scan(&link.ID, &link.AccountName, &link.URL, &link.CreatedAt, &link.Posted); err != nil {
			return nil, err
		}
		links = append(links, link)
	}
	return links, nil
}

// DeleteAccountLink removes a single link from an account's pool by id.
func (p *Pool) DeleteAccountLink(ctx context.Context, id int) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM account_links WHERE id = $1`, id)
	return err
}

// GetCandidateAccountLinks returns up to limit links from the account's
// dedicated pool that have not yet been published for that account, oldest
// first (FIFO), so TriggerAutoPostJob works through a pool in submission order.
func (p *Pool) GetCandidateAccountLinks(ctx context.Context, accountName string, limit int) ([]models.AccountLink, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, account_name, url, created_at
		FROM account_links
		WHERE account_name = $1
		  AND url NOT IN (
		      SELECT product_url FROM published_posts WHERE account_name = $1
		  )
		ORDER BY created_at ASC
		LIMIT $2
	`, accountName, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var links []models.AccountLink
	for rows.Next() {
		var link models.AccountLink
		if err := rows.Scan(&link.ID, &link.AccountName, &link.URL, &link.CreatedAt); err != nil {
			return nil, err
		}
		links = append(links, link)
	}
	return links, nil
}

// GetRotationCandidateAccountLinks returns up to limit links from the
// account's dedicated pool regardless of whether they've already been
// published for that account, ordered by the last time (if any) they were
// posted for it, oldest/never-posted first. Used by TriggerAutoPostJob as a
// last-resort fallback (rotateOldLinks=true) so an account with a fully
// exhausted pool can repost its least-recently-used links instead of being
// skipped entirely.
func (p *Pool) GetRotationCandidateAccountLinks(ctx context.Context, accountName string, limit int) ([]models.AccountLink, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT al.id, al.account_name, al.url, al.created_at
		FROM account_links al
		LEFT JOIN (
		    SELECT product_url, MAX(created_at) AS last_posted_at
		    FROM published_posts
		    WHERE account_name = $1
		    GROUP BY product_url
		) pp ON pp.product_url = al.url
		WHERE al.account_name = $1
		ORDER BY pp.last_posted_at ASC NULLS FIRST
		LIMIT $2
	`, accountName, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var links []models.AccountLink
	for rows.Next() {
		var link models.AccountLink
		if err := rows.Scan(&link.ID, &link.AccountName, &link.URL, &link.CreatedAt); err != nil {
			return nil, err
		}
		links = append(links, link)
	}
	return links, nil
}

// GetRotationCandidateProductsForAccount is the shared-queue counterpart to
// GetRotationCandidateAccountLinks: it returns queued products regardless of
// whether the account already posted them, ordered oldest/never-posted-by-
// this-account first.
func (p *Pool) GetRotationCandidateProductsForAccount(ctx context.Context, accountName string, limit int) ([]models.QueuedProduct, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT qp.id, qp.url, qp.title, qp.price, qp.image_url, qp.scraped_data, qp.status, qp.created_at
		FROM queued_products qp
		LEFT JOIN (
		    SELECT product_url, MAX(created_at) AS last_posted_at
		    FROM published_posts
		    WHERE account_name = $1
		    GROUP BY product_url
		) pp ON pp.product_url = qp.url
		WHERE qp.status = 'queued'
		ORDER BY pp.last_posted_at ASC NULLS FIRST
		LIMIT $2
	`, accountName, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var products []models.QueuedProduct
	for rows.Next() {
		var qp models.QueuedProduct
		var scrapedJSON []byte
		if err := rows.Scan(&qp.ID, &qp.URL, &qp.Title, &qp.Price, &qp.ImageURL, &scrapedJSON, &qp.Status, &qp.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(scrapedJSON, &qp.ScrapedData); err != nil {
			return nil, err
		}
		products = append(products, qp)
	}
	return products, nil
}

// CountPostsTodayForAccount returns how many posts the given account has
// published since the start of the current calendar day.
func (p *Pool) CountPostsTodayForAccount(ctx context.Context, accountName string) (int, error) {
	var count int
	err := p.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM published_posts WHERE account_name = $1 AND created_at >= CURRENT_DATE
	`, accountName).Scan(&count)
	return count, err
}

// GetLastPublishedAtForAccount returns the timestamp of the most recent post
// published by the given account, or nil if it has never published.
func (p *Pool) GetLastPublishedAtForAccount(ctx context.Context, accountName string) (*time.Time, error) {
	var createdAt time.Time
	err := p.pool.QueryRow(ctx, `
		SELECT created_at FROM published_posts WHERE account_name = $1 ORDER BY created_at DESC LIMIT 1
	`, accountName).Scan(&createdAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &createdAt, nil
}

// InsertEvents writes a batch of pipeline events. It satisfies events.Sink.
//
// Failures here are reported to the caller but are never allowed to propagate
// into the pipeline - events.Logger logs and discards them, because telemetry
// must not be able to fail a publish.
func (p *Pool) InsertEvents(ctx context.Context, batch []events.Event) error {
	if len(batch) == 0 {
		return nil
	}

	b := &pgx.Batch{}
	for _, e := range batch {
		var metadata []byte
		if len(e.Metadata) > 0 {
			encoded, err := json.Marshal(e.Metadata)
			if err != nil {
				// A single unencodable metadata map shouldn't cost us the
				// event itself - drop the metadata and keep the record.
				log.Printf("[WARN] Dropping metadata for %s event: %v", e.Type, err)
			} else {
				metadata = encoded
			}
		}

		var durationMS *int
		if e.Duration > 0 {
			ms := int(e.Duration.Milliseconds())
			durationMS = &ms
		}

		b.Queue(`
			INSERT INTO events (
				event_type, level, source, trace_id, account_name, product_url,
				job_id, job_item_id, message, duration_ms, metadata, created_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		`,
			string(e.Type), string(e.Level), e.Source, e.TraceID,
			nullIfEmpty(e.Account), nullIfEmpty(e.ProductURL),
			e.JobID, e.JobItemID, nullIfEmpty(e.Message), durationMS,
			metadata, e.CreatedAt,
		)
	}

	results := p.pool.SendBatch(ctx, b)
	defer results.Close()

	for range batch {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("inserting event batch: %w", err)
		}
	}
	return nil
}

// nullIfEmpty maps an empty string to a SQL NULL, so optional columns stay
// genuinely empty rather than holding zero-length strings that then have to be
// special-cased in every filter and aggregate.
func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
