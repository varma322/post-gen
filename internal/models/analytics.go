package models

import "time"

// DailyCount is one bar in a time series.
type DailyCount struct {
	Date  string `json:"date"` // YYYY-MM-DD
	Count int    `json:"count"`
}

// Delta expresses a metric against the equivalent preceding window, which is
// what the dashboard's "↑12%" indicators render.
type Delta struct {
	Current  int      `json:"current"`
	Previous int      `json:"previous"`
	PctChange *float64 `json:"pct_change,omitempty"` // nil when previous is 0
}

// QueueHealth counts job items by state, mirroring the worker's own vocabulary.
type QueueHealth struct {
	Pending    int `json:"pending"`
	Publishing int `json:"publishing"`
	Published  int `json:"published_24h"`
	Failed     int `json:"failed"`
	Skipped    int `json:"skipped"`
}

// ProviderOutcome is a success/failure tally for one AI provider.
type ProviderOutcome struct {
	Provider string `json:"provider"`
	Success  int    `json:"success"`
	Failed   int    `json:"failed"`
}

// AIStats summarises enrichment health over a window.
type AIStats struct {
	Success     int               `json:"success"`
	Failed      int               `json:"failed"`
	SuccessRate float64           `json:"success_rate"`
	AvgMS       int               `json:"avg_ms"`
	ByProvider  []ProviderOutcome `json:"by_provider"`
}

// ScraperStats summarises scrape health, including how often the Creators API
// had to hand off to the HTML scraper.
type ScraperStats struct {
	Success      int     `json:"success"`
	Failed       int     `json:"failed"`
	FallbackUsed int     `json:"fallback_used"`
	SuccessRate  float64 `json:"success_rate"`
	AvgMS        int     `json:"avg_ms"`
}

// ChannelStats is one row of the Channel Performance table and one card on the
// Channels screen.
type ChannelStats struct {
	AccountName    string       `json:"account_name"`
	FacebookPageID string       `json:"facebook_page_id,omitempty"`
	Active         bool         `json:"active"`
	TotalPosts     int          `json:"total_posts"`
	PostsToday     int          `json:"posts_today"`
	PostsInWindow  int          `json:"posts_in_window"`
	PreviousWindow int          `json:"previous_window"`
	QueueSize      int          `json:"queue_size"`
	SuccessRate    float64      `json:"success_rate"`
	MaxPostsPerDay int          `json:"max_posts_per_day"`
	LastPublishAt  *time.Time   `json:"last_publish_at,omitempty"`
	Daily          []DailyCount `json:"daily"`
}

// WorkerStatus is a snapshot of what the background worker is doing, for the
// Dashboard's Worker Status panel.
type WorkerStatus struct {
	Running        bool       `json:"running"`
	Phase          string     `json:"phase"` // idle | scraping | enriching | publishing | cooldown
	CurrentJobID   *int       `json:"current_job_id,omitempty"`
	CurrentAccount string     `json:"current_account,omitempty"`
	CurrentURL     string     `json:"current_url,omitempty"`
	CooldownUntil  *time.Time `json:"cooldown_until,omitempty"`
	LastPublishAt  *time.Time `json:"last_publish_at,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
	CooldownSecs   int        `json:"cooldown_seconds"`
}

// AnalyticsSummary is the single payload the Dashboard loads on mount.
type AnalyticsSummary struct {
	Days           int            `json:"days"`
	PostsToday     Delta          `json:"posts_today"`
	PostsInWindow  Delta          `json:"posts_in_window"`
	ActiveChannels int            `json:"active_channels"`
	QueueSize      int            `json:"queue_size"`
	FailedPosts    int            `json:"failed_posts"`
	ActiveJobItems int            `json:"active_job_items"`
	Publishing     []DailyCount   `json:"publishing"`
	Failures       []DailyCount   `json:"failures"`
	QueueHealth    QueueHealth    `json:"queue_health"`
	AI             AIStats        `json:"ai"`
	Scraper        ScraperStats   `json:"scraper"`
	Channels       []ChannelStats `json:"channels"`
}
