package models

import "time"

// AccountLink is a URL in an account's own dedicated posting pool, used by
// the auto-post pipeline as the preferred source of links for that account
// before it falls back to the shared product queue.
type AccountLink struct {
	ID          int       `json:"id"`
	AccountName string    `json:"account_name"`
	URL         string    `json:"url"`
	Posted      bool      `json:"posted,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}
