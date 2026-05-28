package db

import (
	"context"
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
