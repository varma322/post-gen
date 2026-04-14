package core

import (
	"fmt"
	"post-gen/internal/config"
	"post-gen/internal/generator"
	"post-gen/internal/models"
	"post-gen/internal/scraper"
)

const (
	defaultTagline  = "Don't miss out on this amazing deal!"
	defaultHashtags = "#AmazonDeals #Offers #MustHave"
)

// Engine orchestrates config loading, scraping, and template generation.
type Engine struct {
	accounts       []models.Account
	selectors      config.Selectors
	paths          Paths
	scraperFactory func(string, config.Selectors) (scraper.Scraper, error)
	postGenerator  func(models.Product, string) (string, error)
}

// NewEngine loads the required configuration files and prepares an Engine.
func NewEngine(paths Paths) (*Engine, error) {
	accounts, err := config.LoadAccounts(paths.AccountsPath)
	if err != nil {
		return nil, fmt.Errorf("loading accounts: %w", err)
	}

	selectors, err := config.LoadSelectors(paths.SelectorsPath)
	if err != nil {
		return nil, fmt.Errorf("loading selectors: %w", err)
	}

	return &Engine{
		accounts:       accounts,
		selectors:      selectors,
		paths:          paths,
		scraperFactory: scraper.GetScraper,
		postGenerator:  generator.GeneratePost,
	}, nil
}

// Accounts exposes the configured account list for callers that need metadata.
func (e *Engine) Accounts() []models.Account {
	accounts := make([]models.Account, len(e.accounts))
	copy(accounts, e.accounts)
	return accounts
}

// Paths returns the runtime paths used by the engine.
func (e *Engine) Paths() Paths {
	return e.paths
}

// GeneratePosts processes each URL for the requested accounts.
// If accountNames is empty, all configured accounts are used.
func (e *Engine) GeneratePosts(urls []string, accountNames []string) ([]Result, error) {
	targetAccounts, err := e.resolveAccounts(accountNames)
	if err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(urls)*len(targetAccounts))
	for _, rawURL := range urls {
		url := rawURL
		if !scraper.IsValidURL(url) {
			results = append(results, Result{
				URL:   url,
				Error: fmt.Sprintf("invalid URL format: %s", url),
			})
			continue
		}

		s, err := e.scraperFactory(url, e.selectors)
		if err != nil {
			results = append(results, Result{
				URL:   url,
				Error: err.Error(),
			})
			continue
		}

		product, err := s.Scrape(url)
		if err != nil {
			results = append(results, Result{
				URL:   url,
				Error: err.Error(),
			})
			continue
		}

		enrichProduct(product)

		for _, account := range targetAccounts {
			post, err := e.postGenerator(*product, account.TemplatePath)
			result := Result{
				URL:          url,
				Account:      account.Name,
				Output:       post,
				ProductTitle: product.Title,
				Product:      *product,
			}
			if err != nil {
				result.Output = ""
				result.Error = fmt.Sprintf("generating post for %s: %v", account.Name, err)
			}
			results = append(results, result)
		}
	}

	return results, nil
}

func (e *Engine) resolveAccounts(accountNames []string) ([]models.Account, error) {
	if len(accountNames) == 0 {
		accounts := make([]models.Account, len(e.accounts))
		copy(accounts, e.accounts)
		return accounts, nil
	}

	available := make(map[string]models.Account, len(e.accounts))
	for _, account := range e.accounts {
		available[account.Name] = account
	}

	resolved := make([]models.Account, 0, len(accountNames))
	for _, name := range accountNames {
		account, ok := available[name]
		if !ok {
			return nil, AccountNotFoundError{Name: name}
		}
		resolved = append(resolved, account)
	}

	return resolved, nil
}

func enrichProduct(product *models.Product) {
	product.Tagline = defaultTagline
	product.Hashtags = defaultHashtags
}
