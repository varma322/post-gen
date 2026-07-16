package core

import (
	"context"
	"fmt"
	"log"
	"post-gen/internal/models"
	"post-gen/internal/utils"
	"strings"
	"sync"
	"time"
)

// Worker orchestrates stateful background publishing tasks from the database pool.
type Worker struct {
	engine       *Engine
	stopChan     chan struct{}
	wg           sync.WaitGroup
	cooldown     time.Duration
	isProcessing bool
	mu           sync.Mutex
}

// NewWorker creates a new background worker.
func NewWorker(engine *Engine, defaultCooldown time.Duration) *Worker {
	return &Worker{
		engine:   engine,
		stopChan: make(chan struct{}),
		cooldown: defaultCooldown,
	}
}

// Start spawns the background worker run loop.
func (w *Worker) Start() {
	w.wg.Add(1)
	go w.run()
}

// Stop signals the worker to exit and waits for it to finish.
func (w *Worker) Stop() {
	close(w.stopChan)
	w.wg.Wait()
}

// staleJobItemTimeout bounds how long an item may sit in 'publishing' before
// the worker assumes it was orphaned by a crash/restart and marks it 'failed'
// rather than leaving it stuck forever while its job is reported 'completed'.
const staleJobItemTimeout = 10 * time.Minute

func (w *Worker) run() {
	defer w.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	log.Println("[INFO] Auto-Post background worker started.")

	if w.engine.db != nil {
		if n, err := w.engine.db.RecoverStaleJobItems(context.Background(), staleJobItemTimeout); err != nil {
			log.Printf("[ERR] Failed to recover stale job items on startup: %v", err)
		} else if n > 0 {
			log.Printf("[WARN] Recovered %d job item(s) stuck in 'publishing' from a prior run; marked failed.", n)
		}
	}

	for {
		select {
		case <-w.stopChan:
			log.Println("[INFO] Auto-Post background worker stopping.")
			return
		case <-ticker.C:
			w.processNextJobItem()
		}
	}
}

func (w *Worker) processNextJobItem() {
	w.mu.Lock()
	if w.isProcessing {
		w.mu.Unlock()
		return
	}
	w.isProcessing = true
	w.mu.Unlock()

	defer func() {
		w.mu.Lock()
		w.isProcessing = false
		w.mu.Unlock()
	}()

	if w.engine.db == nil {
		return
	}

	ctx := context.Background()
	job, err := w.engine.db.GetActiveJob(ctx)
	if err != nil {
		log.Printf("[ERR] Worker: failed to query active job: %v", err)
		return
	}
	if job == nil {
		return // no active job
	}

	if job.Status == "pending" {
		if err := w.engine.db.UpdateJobStatus(ctx, job.ID, "running"); err != nil {
			log.Printf("[ERR] Failed to update job %d status to running: %v", job.ID, err)
			return
		}
		job.Status = "running"
	}

	// Scan pending items for the first one whose account is eligible to post
	// right now, rather than always taking the first pending item in the job.
	// An item blocked by a retryable reason (outside active hours, minimum
	// delay not yet elapsed) is left pending for a later tick instead of
	// stalling every other account's items behind it; an item whose account
	// has permanently exhausted its daily quota is given up on immediately.
	now := time.Now()
	var nextItem *models.JobItem
	var nextAcc models.Account
	anyPending := false

	for i := range job.Items {
		item := &job.Items[i]
		if item.Status != "pending" {
			continue
		}

		resolvedAccounts, err := w.engine.resolveAccounts([]string{item.AccountName})
		if err != nil || len(resolvedAccounts) == 0 {
			errMsg := fmt.Sprintf("Account resolution error: %v", err)
			_ = w.engine.db.UpdateJobItemStatus(ctx, item.ID, "failed", errMsg, nil)
			item.Status = "failed"
			continue
		}
		acc := resolvedAccounts[0]

		eligible, reason, retryable := w.engine.checkAccountEligibility(ctx, acc, now)
		if eligible {
			if nextItem == nil {
				nextItem = item
				nextAcc = acc
			}
			anyPending = true
			continue
		}
		if !retryable {
			log.Printf("[INFO] Item %d permanently skipped: %s", item.ID, reason)
			_ = w.engine.db.UpdateJobItemStatus(ctx, item.ID, "skipped", reason, nil)
			item.Status = "skipped"
			continue
		}
		// Retryable block - leave pending and revisit on a later tick.
		anyPending = true
	}

	if nextItem == nil {
		if !anyPending {
			log.Printf("[INFO] Publication job %d completed successfully.", job.ID)
			if err := w.engine.db.UpdateJobStatus(ctx, job.ID, "completed"); err != nil {
				log.Printf("[ERR] Failed to complete job %d: %v", job.ID, err)
			}
		}
		return
	}
	acc := nextAcc

	log.Printf("[INFO] Worker publishing item %d (Account: %s, URL: %s)", nextItem.ID, nextItem.AccountName, nextItem.ProductURL)

	if err := w.engine.db.UpdateJobItemStatus(ctx, nextItem.ID, "publishing", "", nil); err != nil {
		log.Printf("[ERR] Failed to set status to publishing for item %d: %v", nextItem.ID, err)
		return
	}

	scraperInstance, err := w.engine.scraperFactory(nextItem.ProductURL, w.engine.selectors)
	var product *models.Product
	if err == nil {
		product, err = scraperInstance.Scrape(ctx, nextItem.ProductURL)
	}

	if err != nil {
		errMsg := fmt.Sprintf("Scrape error: %v", err)
		log.Printf("[WARN] Item %d failed: %s", nextItem.ID, errMsg)
		_ = w.engine.db.UpdateJobItemStatus(ctx, nextItem.ID, "failed", errMsg, nil)
		return
	}

	priceCleaned := strings.ToLower(strings.TrimSpace(product.DealPrice))
	if priceCleaned == "" || priceCleaned == "out of stock" || strings.Contains(priceCleaned, "unavailable") {
		msg := "Product is out of stock or price is empty; skipped publication"
		log.Printf("[INFO] Item %d skipped: %s", nextItem.ID, msg)
		_ = w.engine.db.UpdateJobItemStatus(ctx, nextItem.ID, "skipped", msg, nil)
		return
	}

	enrichBaseProduct(product)

	productForAccount := *product
	affiliateLink := utils.AddAffiliateTag(nextItem.ProductURL, acc.AffiliateTag, acc.ExtraParams)
	productForAccount.Link = affiliateLink

	if acc.UseAI {
		enrichCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
		productForAccount = w.engine.aiEnricher.Enrich(enrichCtx, productForAccount, acc)
		cancel()
	}

	postText, err := w.engine.postGenerator(productForAccount, acc.TemplatePath)
	if err != nil {
		errMsg := fmt.Sprintf("Generation error: %v", err)
		_ = w.engine.db.UpdateJobItemStatus(ctx, nextItem.ID, "failed", errMsg, nil)
		return
	}

	if acc.FacebookPageID == "" || acc.FacebookAccessToken == "" {
		errMsg := "Facebook credentials not configured"
		_ = w.engine.db.UpdateJobItemStatus(ctx, nextItem.ID, "failed", errMsg, nil)
		return
	}

	// Re-check for cancellation right before the point of no return: scraping,
	// AI enrichment, and template generation above can take long enough for a
	// user to cancel the job in the meantime. CancelActiveJobs flips this item's
	// DB status but can't interrupt this goroutine directly, so this is the
	// actual enforcement point - skip the Facebook call entirely if the item
	// is no longer 'publishing'.
	if status, statusErr := w.engine.db.GetJobItemStatus(ctx, nextItem.ID); statusErr == nil && status != "publishing" {
		log.Printf("[INFO] Item %d no longer publishing (status=%s); skipping Facebook publish.", nextItem.ID, status)
		return
	}

	pubID, pubErr := w.engine.fbPublisher.PublishPagePost(acc.FacebookPageID, acc.FacebookAccessToken, postText)
	if pubErr != nil {
		errMsg := fmt.Sprintf("Facebook publish error: %v", pubErr)
		log.Printf("[ERR] Item %d Facebook API error: %s", nextItem.ID, errMsg)
		_ = w.engine.db.UpdateJobItemStatus(ctx, nextItem.ID, "failed", errMsg, nil)
		return
	}

	publishedAt := time.Now()
	if err := w.engine.db.UpdateJobItemStatus(ctx, nextItem.ID, "published", "", &publishedAt); err != nil {
		log.Printf("[ERR] Failed to update job item to published: %v", err)
	}

	_ = w.engine.RecordPublishedPost(ctx, models.PublishedPost{
		AccountName:    acc.Name,
		FacebookPageID: acc.FacebookPageID,
		FacebookPostID: pubID,
		ProductTitle:   productForAccount.Title,
		ProductURL:     nextItem.ProductURL,
		Content:        postText,
	})

	log.Printf("[INFO] Item %d published successfully to page %s. PostID: %s", nextItem.ID, acc.FacebookPageID, pubID)

	select {
	case <-w.stopChan:
		return
	case <-time.After(w.cooldown):
	}
}
