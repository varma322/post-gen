package core

import (
	"fmt"

	"post-gen/internal/models"
)

// Paths centralizes runtime asset locations shared by CLI and future server code.
type Paths struct {
	AccountsPath  string
	SelectorsPath string
	PostsPath     string
}

// DefaultPaths returns the on-disk fallback layout used when PostgreSQL is
// unavailable: accounts, scraper selectors, and the local published-post log.
func DefaultPaths() Paths {
	return Paths{
		AccountsPath:  "accounts.json",
		SelectorsPath: "selectors.json",
		PostsPath:     "posts.json",
	}
}

// Result captures the generated content or error for a URL/account pair.
type Result struct {
	URL     string `json:"url"`
	Account string `json:"account"`
	Output  string `json:"output"`
	Error   string `json:"error"`
	// TraceID ties this result to its pipeline events, so a caller can look up
	// which AI provider actually produced the copy, how long each stage took,
	// and why a stage failed - without any of that being duplicated here.
	TraceID      string         `json:"trace_id,omitempty"`
	PublishID    string         `json:"publish_id,omitempty"`
	PublishError string         `json:"publish_error,omitempty"`
	ProductTitle string         `json:"-"`
	Product      models.Product `json:"-"`
}

// AccountNotFoundError indicates that a requested account does not exist.
type AccountNotFoundError struct {
	Name string
}

func (e AccountNotFoundError) Error() string {
	return fmt.Sprintf("account %q not found", e.Name)
}
