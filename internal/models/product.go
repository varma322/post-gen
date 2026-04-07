package models

// Product represents a product scraped from Amazon.
type Product struct {
	Title       string
	DealPrice   string
	MRP         string
	Discount    string
	Features    []string
	Link        string
	Headline    string
	Description string
	Tagline     string
	Hashtags    string
}
