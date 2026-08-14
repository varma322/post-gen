package core

import (
	"context"
	"errors"
	"fmt"
	"log"
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
	// mu guards accounts, which the background Worker reads continuously for
	// the life of the process while HTTP account create/update/delete requests
	// write it via ReloadAccounts.
	mu             sync.RWMutex
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
	e.mu.RLock()
	defer e.mu.RUnlock()
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
		e.mu.Lock()
		e.accounts = accounts
		e.mu.Unlock()
		return nil
	}
	accounts, err := config.LoadAccounts(e.paths.AccountsPath)
	if err != nil {
		return fmt.Errorf("reloading accounts: %w", err)
	}
	e.mu.Lock()
	e.accounts = accounts
	e.mu.Unlock()
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
	e.mu.RLock()
	accounts := make([]models.Account, len(e.accounts))
	copy(accounts, e.accounts)
	e.mu.RUnlock()

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
				affiliateLink := utils.AddAffiliateTag(url, acc.AffiliateTag, acc.ExtraParams)
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
	e.mu.RLock()
	snapshot := make([]models.Account, len(e.accounts))
	copy(snapshot, e.accounts)
	e.mu.RUnlock()

	if len(accountNames) == 0 {
		// "All accounts" (no explicit selection) implicitly means all active ones.
		// Explicit by-name resolution below is intentionally not filtered by
		// Active, so a caller that already knows a specific account name (e.g.
		// the worker resolving an existing job item's account, or a manual
		// override) can still resolve it even if it was deactivated afterward.
		active := make([]models.Account, 0, len(snapshot))
		for _, a := range snapshot {
			if a.IsActive() {
				active = append(active, a)
			}
		}
		return active, nil
	}

	available := make(map[string]models.Account, len(snapshot))
	for _, account := range snapshot {
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

// AddQueuedProduct scrapes the URL, fetches its metadata, and queues it.
func (e *Engine) AddQueuedProduct(ctx context.Context, url string) error {
	if e.db == nil {
		return fmt.Errorf("database required for product queue")
	}

	urlNormalized := utils.NormalizeURL(url)
	if !scraper.IsValidURL(urlNormalized) {
		return fmt.Errorf("invalid URL format: %s", urlNormalized)
	}

	s, err := e.scraperFactory(urlNormalized, e.selectors)
	if err != nil {
		return fmt.Errorf("creating scraper: %w", err)
	}

	product, err := s.Scrape(ctx, urlNormalized)
	if err != nil {
		return fmt.Errorf("scraping product: %w", err)
	}

	// Queue it in the database
	return e.db.AddQueuedProduct(ctx, urlNormalized, product.Title, product.DealPrice, product.ImageURL, *product)
}

// GetQueuedProducts retrieves all active queued products.
func (e *Engine) GetQueuedProducts(ctx context.Context) ([]models.QueuedProduct, error) {
	if e.db == nil {
		return nil, fmt.Errorf("database required for product queue")
	}
	return e.db.GetQueuedProducts(ctx)
}

// DeleteQueuedProduct deletes a product from the queue.
func (e *Engine) DeleteQueuedProduct(ctx context.Context, id int) error {
	if e.db == nil {
		return fmt.Errorf("database required for product queue")
	}
	return e.db.DeleteQueuedProduct(ctx, id)
}

// AddAccountLink validates a URL and adds it to the given account's dedicated
// link pool. Unlike AddQueuedProduct, it does not eagerly scrape the URL:
// account pools are meant for pasting many links at once, and the worker
// already scrapes each URL right before it publishes it.
func (e *Engine) AddAccountLink(ctx context.Context, accountName, rawURL string) error {
	if e.db == nil {
		return fmt.Errorf("database required for account link pools")
	}
	if _, err := e.resolveAccounts([]string{accountName}); err != nil {
		return err
	}

	urlNormalized := utils.NormalizeURL(rawURL)
	if !scraper.IsValidURL(urlNormalized) {
		return fmt.Errorf("invalid URL format: %s", urlNormalized)
	}

	return e.db.AddAccountLink(ctx, accountName, urlNormalized)
}

// GetAccountLinks retrieves every link in the given account's dedicated pool.
func (e *Engine) GetAccountLinks(ctx context.Context, accountName string) ([]models.AccountLink, error) {
	if e.db == nil {
		return nil, fmt.Errorf("database required for account link pools")
	}
	return e.db.GetAccountLinks(ctx, accountName)
}

// DeleteAccountLink removes a single link from an account's pool by id.
func (e *Engine) DeleteAccountLink(ctx context.Context, id int) error {
	if e.db == nil {
		return fmt.Errorf("database required for account link pools")
	}
	return e.db.DeleteAccountLink(ctx, id)
}

// TriggerAutoPostJob builds a job that, for each eligible active account,
// draws links preferentially from that account's own dedicated link pool
// (falling back to the shared product queue for any shortfall) up to however
// many posts it still has left in its daily quota, and creates a job.
//
// If rotateOldLinks is true, an account that still has no candidates after
// both fresh sources are exhausted falls back further to reposting its own
// least-recently-used links (from its pool, then the shared queue) instead of
// being left out of the job entirely.
func (e *Engine) TriggerAutoPostJob(ctx context.Context, rotateOldLinks bool) (int, error) {
	if e.db == nil {
		return 0, fmt.Errorf("database required for auto post jobs")
	}

	// 1. Fast-path check: reject early with a specific message if a job is
	// already active. This alone can't close the check-then-act race between
	// two concurrent requests - the idx_publication_jobs_single_active unique
	// index enforced by CreatePublicationJob below is the authoritative guard.
	activeJob, err := e.db.GetActiveJob(ctx)
	if err != nil {
		return 0, fmt.Errorf("checking active job: %w", err)
	}
	if activeJob != nil {
		return 0, fmt.Errorf("an active auto-post job (ID: %d) is already running", activeJob.ID)
	}

	// 2. Fetch all configured, active accounts
	accounts := e.Accounts()
	activeAccounts := make([]models.Account, 0, len(accounts))
	for _, acc := range accounts {
		if acc.IsActive() {
			activeAccounts = append(activeAccounts, acc)
		}
	}
	if len(activeAccounts) == 0 {
		return 0, fmt.Errorf("no active configured accounts found")
	}

	// 3. Build job items
	var jobItems []models.JobItem
	usedURLs := make(map[string]bool)

	for _, acc := range activeAccounts {
		if eligible, reason, _ := e.checkAccountEligibility(ctx, acc, time.Now()); !eligible {
			log.Printf("[INFO] Skipping account %s: %s", acc.Name, reason)
			continue
		}

		batchSize := e.accountBatchSize(ctx, acc)
		if batchSize == 0 {
			log.Printf("[INFO] Skipping account %s: daily post quota already filled", acc.Name)
			continue
		}

		assignedForAccount := make(map[string]bool, batchSize)
		added := 0

		// Prefer the account's own dedicated link pool.
		poolLinks, err := e.db.GetCandidateAccountLinks(ctx, acc.Name, batchSize)
		if err != nil {
			log.Printf("[WARN] Failed to get pool links for account %s: %v", acc.Name, err)
		}
		for _, link := range poolLinks {
			assignedForAccount[link.URL] = true
			usedURLs[link.URL] = true
			jobItems = append(jobItems, models.JobItem{AccountName: acc.Name, ProductURL: link.URL})
			added++
		}

		// Fall back to the shared product queue for any remaining shortfall.
		if added < batchSize {
			candidates, err := e.db.GetCandidateProductsForAccount(ctx, acc.Name)
			if err != nil {
				log.Printf("[WARN] Failed to get candidates for account %s: %v", acc.Name, err)
				candidates = nil
			}

			// First pass: prefer URLs not already claimed by another account this run.
			for i := range candidates {
				if added >= batchSize {
					break
				}
				url := candidates[i].URL
				if assignedForAccount[url] || usedURLs[url] {
					continue
				}
				assignedForAccount[url] = true
				usedURLs[url] = true
				jobItems = append(jobItems, models.JobItem{AccountName: acc.Name, ProductURL: url})
				added++
			}

			// Second pass: rather than leave the account short a post, allow
			// reusing a URL already claimed by a different account this run.
			for i := range candidates {
				if added >= batchSize {
					break
				}
				url := candidates[i].URL
				if assignedForAccount[url] {
					continue
				}
				assignedForAccount[url] = true
				jobItems = append(jobItems, models.JobItem{AccountName: acc.Name, ProductURL: url})
				added++
			}
		}

		// Last resort: both fresh sources are exhausted for this account.
		// Rather than skip it, repost its own least-recently-used links so the
		// pipeline keeps running instead of stalling until new links are added.
		if added < batchSize && rotateOldLinks {
			rotationLinks, err := e.db.GetRotationCandidateAccountLinks(ctx, acc.Name, batchSize)
			if err != nil {
				log.Printf("[WARN] Failed to get rotation pool links for account %s: %v", acc.Name, err)
			}
			for _, link := range rotationLinks {
				if added >= batchSize {
					break
				}
				if assignedForAccount[link.URL] {
					continue
				}
				assignedForAccount[link.URL] = true
				jobItems = append(jobItems, models.JobItem{AccountName: acc.Name, ProductURL: link.URL})
				added++
			}

			if added < batchSize {
				rotationCandidates, err := e.db.GetRotationCandidateProductsForAccount(ctx, acc.Name, batchSize)
				if err != nil {
					log.Printf("[WARN] Failed to get rotation candidates for account %s: %v", acc.Name, err)
					rotationCandidates = nil
				}
				for i := range rotationCandidates {
					if added >= batchSize {
						break
					}
					url := rotationCandidates[i].URL
					if assignedForAccount[url] {
						continue
					}
					assignedForAccount[url] = true
					jobItems = append(jobItems, models.JobItem{AccountName: acc.Name, ProductURL: url})
					added++
				}
			}
		}

		if added == 0 {
			log.Printf("[INFO] Skipping account %s: no unposted links available in its pool or the shared queue", acc.Name)
		}
	}

	if len(jobItems) == 0 {
		return 0, fmt.Errorf("no unposted links available for any active account")
	}

	// 4. Create the job in the database
	jobID, err := e.db.CreatePublicationJob(ctx, jobItems)
	if err != nil {
		if errors.Is(err, db.ErrJobAlreadyActive) {
			return 0, fmt.Errorf("an active auto-post job is already running")
		}
		return 0, err
	}
	return jobID, nil
}

// accountBatchSize returns how many links TriggerAutoPostJob should try to
// assign to acc in this run: its full remaining daily quota if MaxPostsPerDay
// is set, or 1 (the pre-existing behavior) for uncapped accounts, so an
// unlimited account doesn't get an unbounded number of items queued at once.
// On a transient error counting today's posts, it fails open to 1.
func (e *Engine) accountBatchSize(ctx context.Context, acc models.Account) int {
	if acc.MaxPostsPerDay <= 0 {
		return 1
	}

	todayCount, err := e.db.CountPostsTodayForAccount(ctx, acc.Name)
	if err != nil {
		log.Printf("[WARN] Failed to count today's posts for account %s: %v", acc.Name, err)
		return 1
	}

	remaining := acc.MaxPostsPerDay - todayCount
	if remaining < 0 {
		return 0
	}
	return remaining
}

// checkAccountEligibility evaluates an account's per-account scheduling and
// rate-limit rules for the auto-post pipeline. It is only called from the
// DB-backed auto-post job flow (TriggerAutoPostJob, Worker), so e.db is
// assumed non-nil. On a transient error fetching post history, it logs and
// fails open (treats the account as eligible) rather than blocking the run.
// The retryable return value is meaningless when eligible is true.
func (e *Engine) checkAccountEligibility(ctx context.Context, acc models.Account, now time.Time) (eligible bool, reason string, retryable bool) {
	todayCount, err := e.db.CountPostsTodayForAccount(ctx, acc.Name)
	if err != nil {
		log.Printf("[WARN] Failed to count today's posts for account %s: %v", acc.Name, err)
		return true, "", true
	}

	lastPostTime, err := e.db.GetLastPublishedAtForAccount(ctx, acc.Name)
	if err != nil {
		log.Printf("[WARN] Failed to get last published time for account %s: %v", acc.Name, err)
		return true, "", true
	}

	return acc.IsEligibleToPost(now, todayCount, lastPostTime)
}

// GetActiveJob retrieves current active job status
func (e *Engine) GetActiveJob(ctx context.Context) (*models.PublicationJob, error) {
	if e.db == nil {
		return nil, fmt.Errorf("database required for auto post jobs")
	}
	return e.db.GetActiveJob(ctx)
}

// CancelActiveJobs cancels the currently running job
func (e *Engine) CancelActiveJobs(ctx context.Context) error {
	if e.db == nil {
		return fmt.Errorf("database required for auto post jobs")
	}
	return e.db.CancelActiveJobs(ctx)
}
