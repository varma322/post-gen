package models

import "time"

// QueuedProduct represents a product added to the publishing pool.
type QueuedProduct struct {
	ID          int       `json:"id"`
	URL         string    `json:"url"`
	Title       string    `json:"title"`
	Price       string    `json:"price"`
	ImageURL    string    `json:"image_url"`
	ScrapedData Product   `json:"scraped_data"`
	Status      string    `json:"status"` // "queued", "archived"
	CreatedAt   time.Time `json:"created_at"`
}

// PublicationJob represents a multi-page posting schedule.
type PublicationJob struct {
	ID        int       `json:"id"`
	Status    string    `json:"status"` // "pending", "running", "completed", "failed", "cancelled"
	CreatedAt time.Time `json:"created_at"`
	Items     []JobItem `json:"items,omitempty"`
}

// JobItem represents a specific post task inside a PublicationJob.
type JobItem struct {
	ID           int        `json:"id"`
	JobID        int        `json:"job_id"`
	AccountName  string     `json:"account_name"`
	ProductURL   string     `json:"product_url"`
	Status       string     `json:"status"` // "pending", "publishing", "published", "failed", "skipped"
	ErrorMessage string     `json:"error_message,omitempty"`
	PublishedAt  *time.Time `json:"published_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}
