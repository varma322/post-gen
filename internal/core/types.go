package core

import (
	"fmt"

	"post-gen/internal/models"
)

// Paths centralizes runtime asset locations shared by CLI and future server code.
type Paths struct {
	AccountsPath  string
	SelectorsPath string
	OutputDir     string
}

// DefaultPaths returns the current filesystem layout used by the CLI.
func DefaultPaths() Paths {
	return Paths{
		AccountsPath:  "accounts.json",
		SelectorsPath: "selectors.json",
		OutputDir:     "output",
	}
}

// Result captures the generated content or error for a URL/account pair.
type Result struct {
	URL          string         `json:"url"`
	Account      string         `json:"account"`
	Output       string         `json:"output"`
	Error        string         `json:"error"`
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
