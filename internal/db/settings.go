package db

import (
	"context"
	"encoding/json"
	"fmt"
)

// LoadSettings returns every stored override, keyed by setting name.
func (p *Pool) LoadSettings(ctx context.Context) (map[string]json.RawMessage, error) {
	rows, err := p.pool.Query(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, fmt.Errorf("querying settings: %w", err)
	}
	defer rows.Close()

	settings := make(map[string]json.RawMessage)
	for rows.Next() {
		var (
			key   string
			value []byte
		)
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("scanning setting: %w", err)
		}
		settings[key] = json.RawMessage(value)
	}

	return settings, rows.Err()
}

// SaveSettings upserts a batch of overrides in one transaction, so a partial
// failure can't leave the configuration half-applied.
func (p *Pool) SaveSettings(ctx context.Context, values map[string]json.RawMessage) error {
	if len(values) == 0 {
		return nil
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning settings transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for key, value := range values {
		if _, err := tx.Exec(ctx, `
			INSERT INTO settings (key, value, updated_at)
			VALUES ($1, $2, CURRENT_TIMESTAMP)
			ON CONFLICT (key) DO UPDATE SET
				value = EXCLUDED.value,
				updated_at = CURRENT_TIMESTAMP
		`, key, []byte(value)); err != nil {
			return fmt.Errorf("saving setting %q: %w", key, err)
		}
	}

	return tx.Commit(ctx)
}

// DeleteSetting removes an override, returning the value to its environment or
// built-in default.
func (p *Pool) DeleteSetting(ctx context.Context, key string) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM settings WHERE key = $1`, key)
	return err
}
