package core

import (
	"context"
	"fmt"
	"post-gen/internal/ai"
	"post-gen/internal/config"
	"post-gen/internal/db"
	"post-gen/internal/generator"
	"post-gen/internal/models"
	"post-gen/internal/publisher"
	"post-gen/internal/scraper"
	"post-gen/internal/utils"
	"time"
)

const (
	defaultTagline  = "Don't miss out on this amazing deal!"
	defaultHashtags = "#AmazonDeals #Offers #MustHave"
)

// Engine orchestrates config loading, scraping, AI enrichment, and template generation.
type Engine struct {
	accounts       []models.Account
	selectors      config.Selectors
	paths          Paths
	scraperFactory func(string, config.Selectors) (scraper.Scraper, error)
	postGenerator  func(models.Product, string) (string, error)
	fbPublisher    *publisher.FacebookPublisher
	aiEnricher     *ai.Enricher
	db             *db.Pool
}

// NewEngine loads the required configuration files and prepares an Engine.
// If a DB pool is provided, accounts are loaded from PostgreSQL (with JSON fallback
// for first-time migration). Pass nil to use the legacy JSON-only mode.
func NewEngine(paths Paths, dbPool *db.Pool) (*Engine, error) {
	selectors, err := config.LoadSelectors(paths.SelectorsPath)
	if err != nil {
		return nil, fmt.Errorf("loading selectors: %w", err)
	}

	var accounts []models.Account

	if dbPool != nil {
		// Load from DB; auto-migrate from JSON if the table is empty
		ctx := context.Background()
		count, err := dbPool.Count(ctx)
		if err != nil {
			return nil, fmt.Errorf("counting db accounts: %w", err)
		}

		if count == 0 {
			// First run: seed the DB from the JSON file
			jsonAccounts, jsonErr := config.LoadAccounts(paths.AccountsPath)
			if jsonErr == nil && len(jsonAccounts) > 0 {
				// Default UseAI to true for all migrated accounts
				for i := range jsonAccounts {
					jsonAccounts[i].UseAI = true
				}
				if seedErr := dbPool.SaveAccounts(ctx, jsonAccounts); seedErr != nil {
					return nil, fmt.Errorf("seeding accounts from JSON: %w", seedErr)
				}
				accounts = jsonAccounts
				fmt.Println("[INFO] Migrated accounts from accounts.json to PostgreSQL.")
			}
		} else {
			accounts, err = dbPool.LoadAccounts(ctx)
			if err != nil {
				return nil, fmt.Errorf("loading accounts from db: %w", err)
			}
		}
	} else {
		// Legacy JSON-only mode
		accounts, err = config.LoadAccounts(paths.AccountsPath)
		if err != nil {
			return nil, fmt.Errorf("loading accounts: %w", err)
		}
	}

	return &Engine{
		accounts:       accounts,
		selectors:      selectors,
		paths:          paths,
		scraperFactory: scraper.GetScraper,
		postGenerator:  generator.GeneratePost,
		fbPublisher:    publisher.NewFacebookPublisher(),
		aiEnricher:     ai.New(),
		db:             dbPool,
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

// ReloadAccounts re-reads accounts from DB (or JSON in legacy mode) and updates the engine in-place.
func (e *Engine) ReloadAccounts() error {
	ctx := context.Background()
	if e.db != nil {
		accounts, err := e.db.LoadAccounts(ctx)
		if err != nil {
			return fmt.Errorf("reloading accounts from db: %w", err)
		}
		e.accounts = accounts
		return nil
	}
	accounts, err := config.LoadAccounts(e.paths.AccountsPath)
	if err != nil {
		return fmt.Errorf("reloading accounts: %w", err)
	}
	e.accounts = accounts
	return nil
}

// SaveAccounts persists account changes to DB or JSON depending on mode.
func (e *Engine) SaveAccounts(accounts []models.Account) error {
	ctx := context.Background()
	if e.db != nil {
		return e.db.SaveAccounts(ctx, accounts)
	}
	return config.SaveAccounts(e.paths.AccountsPath, accounts)
}

// DeleteAccount removes an account from DB or JSON.
func (e *Engine) DeleteAccount(name string) error {
	ctx := context.Background()
	if e.db != nil {
		return e.db.DeleteAccount(ctx, name)
	}
	// JSON fallback: reload, filter, save
	accounts := e.accounts
	filtered := make([]models.Account, 0, len(accounts))
	found := false
	for _, a := range accounts {
		if a.Name == name {
			found = true
			continue
		}
		filtered = append(filtered, a)
	}
	if !found {
		return fmt.Errorf("account %q not found", name)
	}
	return config.SaveAccounts(e.paths.AccountsPath, filtered)
}

// GeneratePosts processes each URL for the requested accounts.
// If accountNames is empty, all configured accounts are used.
func (e *Engine) GeneratePosts(urls []string, accountNames []string) ([]Result, error) {
	return e.GeneratePostsWithPublish(urls, accountNames, false, 0, nil)
}

// GeneratePostsWithPublish processes each URL, enriches with AI, generates posts,
// and optionally publishes them to Facebook Pages.
// If accountNames is empty, all configured accounts are used.
func (e *Engine) GeneratePostsWithPublish(urls []string, accountNames []string, publish bool, delayBetweenPosts time.Duration, onCooldown func(time.Duration)) ([]Result, error) {
	targetAccounts, err := e.resolveAccounts(accountNames)
	if err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(urls)*len(targetAccounts))
	publishCount := 0

	for _, rawURL := range urls {
		url := utils.NormalizeURL(rawURL)
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

		enrichBaseProduct(product)

		for _, account := range targetAccounts {
			productForAccount := *product
			affiliateLink := utils.AddAffiliateTag(url, account.AffiliateTag)
			productForAccount.Link = affiliateLink

			// AI enrichment: polishes Title, Features, Tagline, Hashtags etc.
			// The enriched fields are then fed into each account's unique .tmpl template.
			// UseAI defaults to true; accounts can opt out by setting UseAI=false.
			if account.UseAI {
				ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
				productForAccount = e.aiEnricher.Enrich(ctx, productForAccount, account)
				cancel()
			}

			post, err := e.postGenerator(productForAccount, account.TemplatePath)
			result := Result{
				URL:          url,
				Account:      account.Name,
				Output:       post,
				ProductTitle: productForAccount.Title,
				Product:      productForAccount,
			}
			if err != nil {
				result.Output = ""
				result.Error = fmt.Sprintf("generating post for %s: %v", account.Name, err)
				results = append(results, result)
				continue
			}

			// Core Facebook publishing integration
			if publish && account.FacebookPageID != "" && account.FacebookAccessToken != "" {
				if publishCount > 0 && delayBetweenPosts > 0 {
					if onCooldown != nil {
						onCooldown(delayBetweenPosts)
					}
					time.Sleep(delayBetweenPosts)
				}

				pubID, pubErr := e.fbPublisher.PublishPagePost(
					account.FacebookPageID,
					account.FacebookAccessToken,
					post,
				)

				if pubErr != nil {
					result.PublishError = pubErr.Error()
				} else {
					result.PublishID = pubID
					publishCount++
				}
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

// enrichBaseProduct applies default fallback values to fields not set by the scraper.
func enrichBaseProduct(product *models.Product) {
	if product.Tagline == "" {
		product.Tagline = defaultTagline
	}
	if product.Hashtags == "" {
		product.Hashtags = defaultHashtags
	}
}
