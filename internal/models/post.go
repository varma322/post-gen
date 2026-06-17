package models

import "time"

// PublishedPost represents a post published to Facebook.
type PublishedPost struct {
	ID             int       `json:"id,omitempty"`
	AccountName    string    `json:"account_name"`
	FacebookPageID string    `json:"facebook_page_id"`
	FacebookPostID string    `json:"facebook_post_id"`
	ProductTitle   string    `json:"product_title"`
	ProductURL     string    `json:"product_url"`
	Content        string    `json:"content"`
	CreatedAt      time.Time `json:"created_at"`
}

// Stats holds aggregated statistics for the dashboard.
type Stats struct {
	TotalPosts   int             `json:"total_posts"`
	PostsToday   int             `json:"posts_today"`
	AccountStats []AccountStats  `json:"account_stats"`
	RecentPosts  []PublishedPost `json:"recent_posts"`
}

// AccountStats holds stats for a single page account.
type AccountStats struct {
	AccountName string `json:"account_name"`
	TotalPosts  int    `json:"total_posts"`
	PostsToday  int    `json:"posts_today"`
}
