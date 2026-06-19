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

func (w *Worker) run() {
	defer w.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	log.Println("[INFO] Auto-Post background worker started.")

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
		return
	}

	if job.Status == "pending" {
		if err := w.engine.db.UpdateJobStatus(ctx, job.ID, "running"); err != nil {
			log.Printf("[ERR] Failed to update job %d status to running: %v", job.ID, err)
			return
		}
		job.Status = "running"
	}

	var nextItem *models.JobItem
	for i := range job.Items {
		if job.Items[i].Status == "pending" {
			nextItem = &job.Items[i]
			break
		}
	}

	if nextItem == nil {
		log.Printf("[INFO] Publication job %d completed successfully.", job.ID)
		if err := w.engine.db.UpdateJobStatus(ctx, job.ID, "completed"); err != nil {
			log.Printf("[ERR] Failed to complete job %d: %v", job.ID, err)
		}
		return
	}

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

	resolvedAccounts, err := w.engine.resolveAccounts([]string{nextItem.AccountName})
	if err != nil || len(resolvedAccounts) == 0 {
		errMsg := fmt.Sprintf("Account resolution error: %v", err)
		_ = w.engine.db.UpdateJobItemStatus(ctx, nextItem.ID, "failed", errMsg, nil)
		return
	}
	acc := resolvedAccounts[0]

	productForAccount := *product
	affiliateLink := utils.AddAffiliateTag(nextItem.ProductURL, acc.AffiliateTag)
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

	pubID, pubErr := w.engine.fbPublisher.PublishPagePost(acc.FacebookPageID, acc.FacebookAccessToken, postText)
	if pubErr != nil {
		errMsg := fmt.Sprintf("Facebook publish error: %v", pubErr)
		log.Printf("[ERR] Item %d Facebook API error: %s", nextItem.ID, errMsg)
		_ = w.engine.db.UpdateJobItemStatus(ctx, nextItem.ID, "failed", errMsg, nil)
		return
	}

	now := time.Now()
	if err := w.engine.db.UpdateJobItemStatus(ctx, nextItem.ID, "published", "", &now); err != nil {
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
