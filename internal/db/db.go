package db

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"post-gen/internal/models"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

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
		created_at           TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at           TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

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
		created_at           TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_queued_products_status ON queued_products(status);
	CREATE INDEX IF NOT EXISTS idx_job_items_job_id ON job_items(job_id);
	CREATE INDEX IF NOT EXISTS idx_job_items_status ON job_items(status);
	`
	_, err := p.pool.Exec(ctx, schema)
	return err
}

// LoadAccounts retrieves all accounts from the database.
func (p *Pool) LoadAccounts(ctx context.Context) ([]models.Account, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT name, template_path, affiliate_tag, facebook_page_id, facebook_access_token, use_ai, ai_prompt
		FROM accounts ORDER BY id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("querying accounts: %w", err)
	}
	defer rows.Close()

	var accounts []models.Account
	for rows.Next() {
		var a models.Account
		if err := rows.Scan(
			&a.Name,
			&a.TemplatePath,
			&a.AffiliateTag,
			&a.FacebookPageID,
			&a.FacebookAccessToken,
			&a.UseAI,
			&a.AIPrompt,
		); err != nil {
			return nil, fmt.Errorf("scanning account row: %w", err)
		}
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}

// UpsertAccount inserts or updates an account by name.
func (p *Pool) UpsertAccount(ctx context.Context, a models.Account) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO accounts (name, template_path, affiliate_tag, facebook_page_id, facebook_access_token, use_ai, ai_prompt)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (name) DO UPDATE SET
			template_path        = EXCLUDED.template_path,
			affiliate_tag        = EXCLUDED.affiliate_tag,
			facebook_page_id     = EXCLUDED.facebook_page_id,
			facebook_access_token = EXCLUDED.facebook_access_token,
			use_ai               = EXCLUDED.use_ai,
			ai_prompt            = EXCLUDED.ai_prompt
	`,
		a.Name, a.TemplatePath, a.AffiliateTag,
		a.FacebookPageID, a.FacebookAccessToken,
		a.UseAI, a.AIPrompt,
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
		INSERT INTO published_posts (account_name, facebook_page_id, facebook_post_id, product_title, product_url, content, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`,
		post.AccountName,
		post.FacebookPageID,
		post.FacebookPostID,
		post.ProductTitle,
		post.ProductURL,
		post.Content,
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
func (p *Pool) GetActiveJob(ctx context.Context) (*models.PublicationJob, error) {
	var job models.PublicationJob
	err := p.pool.QueryRow(ctx, `
		SELECT id, status, created_at FROM publication_jobs
		WHERE status IN ('pending', 'running')
		ORDER BY id DESC LIMIT 1
	`).Scan(&job.ID, &job.Status, &job.CreatedAt)
	if err != nil {
		return nil, err // Can be pgx.ErrNoRows
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

	// Update pending items to skipped
	_, err = tx.Exec(ctx, `
		UPDATE job_items SET status = 'skipped', error_message = 'Job cancelled by user'
		WHERE status = 'pending'
	`)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// GetCandidateProductsForAccount finds all queued products that have not been posted to the given account.
func (p *Pool) GetCandidateProductsForAccount(ctx context.Context, accountName string) ([]models.QueuedProduct, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, url, title, price, image_url, scraped_data, status, created_at
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
