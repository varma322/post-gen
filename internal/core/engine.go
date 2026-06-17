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
	"os"
	"strings"
	"sync"
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
func (e *Engine) GeneratePosts(ctx context.Context, urls []string, accountNames []string) ([]Result, error) {
	return e.GeneratePostsWithPublish(ctx, urls, accountNames, false, 0, nil)
}

// GeneratePostsWithPublish processes each URL, enriches with AI, generates posts,
// and optionally publishes them to Facebook Pages.
// If accountNames is empty, all configured accounts are used.
func (e *Engine) GeneratePostsWithPublish(ctx context.Context, urls []string, accountNames []string, publish bool, delayBetweenPosts time.Duration, onCooldown func(time.Duration)) ([]Result, error) {
	targetAccounts, err := e.resolveAccounts(accountNames)
	if err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(urls)*len(targetAccounts))
	publishAttempts := 0

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

		product, err := s.Scrape(ctx, url)
		if err != nil {
			results = append(results, Result{
				URL:   url,
				Error: err.Error(),
			})
			continue
		}

		enrichBaseProduct(product)

		priceCleaned := strings.ToLower(strings.TrimSpace(product.DealPrice))
		if priceCleaned == "" || priceCleaned == "out of stock" || strings.Contains(priceCleaned, "unavailable") {
			results = append(results, Result{
				URL:          url,
				ProductTitle: product.Title,
				Product:      *product,
				Error:        "Product is out of stock or price is empty; skipping post generation",
			})
			continue
		}

		type tempResult struct {
			index  int
			result Result
		}
		ch := make(chan tempResult, len(targetAccounts))
		var wg sync.WaitGroup

		for i, account := range targetAccounts {
			wg.Add(1)
			go func(index int, acc models.Account) {
				defer wg.Done()
				productForAccount := *product
				affiliateLink := utils.AddAffiliateTag(url, acc.AffiliateTag)
				productForAccount.Link = affiliateLink

				// AI enrichment: polishes Title, Features, Tagline, Hashtags etc.
				// The enriched fields are then fed into each account's unique .tmpl template.
				// UseAI defaults to true; accounts can opt out by setting UseAI=false.
				if acc.UseAI {
					enrichCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
					productForAccount = e.aiEnricher.Enrich(enrichCtx, productForAccount, acc)
					cancel()
				}

				post, err := e.postGenerator(productForAccount, acc.TemplatePath)
				result := Result{
					URL:          url,
					Account:      acc.Name,
					Output:       post,
					ProductTitle: productForAccount.Title,
					Product:      productForAccount,
				}
				if err != nil {
					result.Output = ""
					result.Error = fmt.Sprintf("generating post for %s: %v", acc.Name, err)
				}
				ch <- tempResult{index: index, result: result}
			}(i, account)
		}

		wg.Wait()
		close(ch)

		orderedResults := make([]Result, len(targetAccounts))
		for item := range ch {
			orderedResults[item.index] = item.result
		}

		for _, result := range orderedResults {
			if result.Error != "" {
				results = append(results, result)
				continue
			}

			// Core Facebook publishing integration
			if publish {
				var targetAccount models.Account
				for _, acc := range targetAccounts {
					if acc.Name == result.Account {
						targetAccount = acc
						break
					}
				}

				if targetAccount.FacebookPageID != "" && targetAccount.FacebookAccessToken != "" {
					if publishAttempts > 0 && delayBetweenPosts > 0 {
						if onCooldown != nil {
							onCooldown(delayBetweenPosts)
						}
						time.Sleep(delayBetweenPosts)
					}
					publishAttempts++

					pubID, pubErr := e.fbPublisher.PublishPagePost(
						targetAccount.FacebookPageID,
						targetAccount.FacebookAccessToken,
						result.Output,
					)

					if pubErr != nil {
						result.PublishError = pubErr.Error()
					} else {
						result.PublishID = pubID
						_ = e.RecordPublishedPost(ctx, models.PublishedPost{
							AccountName:    targetAccount.Name,
							FacebookPageID: targetAccount.FacebookPageID,
							FacebookPostID: pubID,
							ProductTitle:   result.ProductTitle,
							ProductURL:     result.URL,
							Content:        result.Output,
						})
					}
				}
			}

			results = append(results, result)
		}
	}

	return results, nil
}

// PublishPost publishes a pre-generated post directly to Facebook for the given account.
func (e *Engine) PublishPost(accountName, postText string) (string, error) {
	targetAccounts, err := e.resolveAccounts([]string{accountName})
	if err != nil {
		return "", err
	}
	if len(targetAccounts) == 0 {
		return "", fmt.Errorf("account %q not found", accountName)
	}
	account := targetAccounts[0]
	if account.FacebookPageID == "" || account.FacebookAccessToken == "" {
		return "", fmt.Errorf("facebook credentials not configured for account %q", accountName)
	}
	
	pubID, err := e.fbPublisher.PublishPagePost(account.FacebookPageID, account.FacebookAccessToken, postText)
	if err != nil {
		return "", err
	}

	var productURL string
	var productTitle string
	words := strings.Fields(postText)
	for _, word := range words {
		if strings.HasPrefix(word, "http://") || strings.HasPrefix(word, "https://") {
			productURL = word
			break
		}
	}
	lines := strings.Split(postText, "\n")
	if len(lines) > 0 {
		productTitle = strings.TrimSpace(lines[0])
		if len(productTitle) > 100 {
			productTitle = productTitle[:100] + "..."
		}
	}

	_ = e.RecordPublishedPost(context.Background(), models.PublishedPost{
		AccountName:    account.Name,
		FacebookPageID: account.FacebookPageID,
		FacebookPostID: pubID,
		ProductTitle:   productTitle,
		ProductURL:     productURL,
		Content:        postText,
	})

	return pubID, nil
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

// RecordPublishedPost logs a successful publish to the database or JSON fallback.
func (e *Engine) RecordPublishedPost(ctx context.Context, post models.PublishedPost) error {
	post.CreatedAt = time.Now()
	if e.db != nil {
		return e.db.RecordPublishedPost(ctx, post)
	}

	// JSON fallback
	posts, err := config.LoadPosts(e.paths.PostsPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("loading posts: %w", err)
	}
	posts = append(posts, post)
	if err := config.SavePosts(e.paths.PostsPath, posts); err != nil {
		return fmt.Errorf("saving posts: %w", err)
	}
	return nil
}

// GetStats retrieves the aggregated statistics and recent posts log.
func (e *Engine) GetStats(ctx context.Context, limit int) (*models.Stats, error) {
	if e.db != nil {
		return e.db.GetStats(ctx, limit)
	}

	// JSON fallback
	posts, err := config.LoadPosts(e.paths.PostsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &models.Stats{AccountStats: []models.AccountStats{}, RecentPosts: []models.PublishedPost{}}, nil
		}
		return nil, fmt.Errorf("loading posts: %w", err)
	}

	stats := &models.Stats{
		AccountStats: []models.AccountStats{},
		RecentPosts:  []models.PublishedPost{},
	}

	stats.TotalPosts = len(posts)

	today := time.Now().Truncate(24 * time.Hour)
	postsTodayCount := 0

	accTotal := make(map[string]int)
	accToday := make(map[string]int)

	for _, p := range posts {
		if p.CreatedAt.After(today) || p.CreatedAt.Equal(today) {
			postsTodayCount++
			accToday[p.AccountName]++
		}
		accTotal[p.AccountName]++
	}

	stats.PostsToday = postsTodayCount

	for name, total := range accTotal {
		stats.AccountStats = append(stats.AccountStats, models.AccountStats{
			AccountName: name,
			TotalPosts:  total,
			PostsToday:  accToday[name],
		})
	}

	recentLimit := limit
	if len(posts) < recentLimit {
		recentLimit = len(posts)
	}
	for i := 0; i < recentLimit; i++ {
		stats.RecentPosts = append(stats.RecentPosts, posts[len(posts)-1-i])
	}

	return stats, nil
}
