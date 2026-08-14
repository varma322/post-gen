package models

import "time"

// Event is a stored pipeline event as served by the API.
//
// It is deliberately separate from events.Event, which is the write-side
// shape: this one carries the database id and a millisecond duration, and
// omits the buffering concerns the emitter cares about.
type Event struct {
	ID          int64          `json:"id"`
	EventType   string         `json:"event_type"`
	Level       string         `json:"level"`
	Source      string         `json:"source"`
	TraceID     string         `json:"trace_id"`
	AccountName string         `json:"account_name,omitempty"`
	ProductURL  string         `json:"product_url,omitempty"`
	JobID       *int           `json:"job_id,omitempty"`
	JobItemID   *int           `json:"job_item_id,omitempty"`
	Message     string         `json:"message,omitempty"`
	DurationMS  *int           `json:"duration_ms,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
}

// EventFilter narrows an event query. Zero values mean "no constraint", so a
// bare filter returns the most recent events across the whole pipeline.
type EventFilter struct {
	Limit   int
	Level   string
	Source  string
	Account string
	Type    string
	Since   *time.Time
	// Search matches against the message and product URL.
	Search string
}
