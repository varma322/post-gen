package core

import (
	"context"
	"fmt"
	"log"
	"post-gen/internal/ai"
	"post-gen/internal/events"
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

	// statusMu guards the live status snapshot, which the HTTP layer reads
	// concurrently with the worker goroutine writing it.
	statusMu sync.RWMutex
	status   models.WorkerStatus
}

// Status returns a snapshot of what the worker is doing, for the dashboard's
// Worker Status panel.
func (w *Worker) Status() models.WorkerStatus {
	w.statusMu.RLock()
	defer w.statusMu.RUnlock()

	snapshot := w.status
	snapshot.CooldownSecs = int(w.cooldown.Seconds())

	// A cooldown that has already elapsed is stale - report idle rather than a
	// countdown the UI would render as a negative number.
	if snapshot.CooldownUntil != nil && time.Now().After(*snapshot.CooldownUntil) {
		snapshot.CooldownUntil = nil
		if snapshot.Phase == phaseCooldown {
			snapshot.Phase = phaseIdle
		}
	}

	return snapshot
}

// Worker phases, as reported by Status.
const (
	phaseIdle       = "idle"
	phaseScraping   = "scraping"
	phaseEnriching  = "enriching"
	phasePublishing = "publishing"
	phaseCooldown   = "cooldown"
)

// setPhase records what the worker is currently doing.
func (w *Worker) setPhase(phase, account, url string, jobID *int) {
	w.statusMu.Lock()
	defer w.statusMu.Unlock()

	w.status.Phase = phase
	w.status.CurrentAccount = account
	w.status.CurrentURL = url
	w.status.CurrentJobID = jobID
}

// setIdle clears the current item, keeping the running flag and history.
func (w *Worker) setIdle() {
	w.statusMu.Lock()
	defer w.statusMu.Unlock()

	w.status.Phase = phaseIdle
	w.status.CurrentAccount = ""
	w.status.CurrentURL = ""
	w.status.CurrentJobID = nil
}

// notePublished records a successful publish and the cooldown it starts.
func (w *Worker) notePublished(at time.Time) {
	w.statusMu.Lock()
	defer w.statusMu.Unlock()

	until := at.Add(w.cooldown)
	w.status.LastPublishAt = &at
	w.status.CooldownUntil = &until
	w.status.Phase = phaseCooldown
	w.status.LastError = ""
}

// noteError records the most recent failure so the panel can surface it.
func (w *Worker) noteError(message string) {
	w.statusMu.Lock()
	defer w.statusMu.Unlock()
	w.status.LastError = message
}

// NewWorker creates a new background worker and registers it with the engine,
// so the HTTP layer can report worker state without being handed the worker
// separately at every construction site.
func NewWorker(engine *Engine, defaultCooldown time.Duration) *Worker {
	worker := &Worker{
		engine:   engine,
		stopChan: make(chan struct{}),
		cooldown: defaultCooldown,
	}

	engine.mu.Lock()
	engine.worker = worker
	engine.mu.Unlock()

	return worker
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

	w.statusMu.Lock()
	w.status.Running = true
	w.status.Phase = phaseIdle
	w.statusMu.Unlock()

	defer func() {
		w.statusMu.Lock()
		w.status.Running = false
		w.status.Phase = phaseIdle
		w.statusMu.Unlock()
	}()

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

// tallyItems counts a finished job's outcomes for the JOB_COMPLETED summary.
func tallyItems(items []models.JobItem) (published, failed, skipped int) {
	for _, item := range items {
		switch item.Status {
		case "published":
			published++
		case "failed":
			failed++
		case "skipped":
			skipped++
		}
	}
	return published, failed, skipped
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

		w.engine.events.Emit(events.Event{
			Type:     events.JobStarted,
			Source:   events.SourceWorker,
			TraceID:  events.NewTraceID(),
			JobID:    &job.ID,
			Message:  fmt.Sprintf("Worker picked up job %d (%d items)", job.ID, len(job.Items)),
			Metadata: map[string]any{"item_count": len(job.Items)},
		})
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

			w.engine.events.Emit(events.Event{
				Type:       events.JobSkipped,
				Source:     events.SourceWorker,
				TraceID:    events.NewTraceID(),
				Account:    acc.Name,
				ProductURL: item.ProductURL,
				JobID:      &job.ID,
				JobItemID:  &item.ID,
				Message:    reason,
				Metadata:   map[string]any{"reason": "quota_exhausted"},
			})
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

			published, failed, skipped := tallyItems(job.Items)
			w.engine.events.Emit(events.Event{
				Type:    events.JobCompleted,
				Source:  events.SourceWorker,
				TraceID: events.NewTraceID(),
				JobID:   &job.ID,
				Message: fmt.Sprintf("Job %d finished: %d published, %d failed, %d skipped", job.ID, published, failed, skipped),
				Metadata: map[string]any{
					"published": published,
					"failed":    failed,
					"skipped":   skipped,
				},
			})
		}
		return
	}
	acc := nextAcc

	// One trace per item: scrape, enrichment, and publish for this item all
	// carry it, so the Activity Log can show the item's full story in order.
	traceID := events.NewTraceID()

	log.Printf("[INFO] Worker publishing item %d (Account: %s, URL: %s)", nextItem.ID, nextItem.AccountName, nextItem.ProductURL)

	if err := w.engine.db.UpdateJobItemStatus(ctx, nextItem.ID, "publishing", "", nil); err != nil {
		log.Printf("[ERR] Failed to set status to publishing for item %d: %v", nextItem.ID, err)
		return
	}

	w.setPhase(phaseScraping, acc.Name, nextItem.ProductURL, &job.ID)
	defer w.setIdle()

	product, err := w.engine.scrapeWithEvents(ctx, traceID, acc.Name, nextItem.ProductURL)
	if err != nil {
		errMsg := fmt.Sprintf("Scrape error: %v", err)
		log.Printf("[WARN] Item %d failed: %s", nextItem.ID, errMsg)
		_ = w.engine.db.UpdateJobItemStatus(ctx, nextItem.ID, "failed", errMsg, nil)
		w.noteError(errMsg)
		return
	}

	priceCleaned := strings.ToLower(strings.TrimSpace(product.DealPrice))
	if priceCleaned == "" || priceCleaned == "out of stock" || strings.Contains(priceCleaned, "unavailable") {
		msg := "Product is out of stock or price is empty; skipped publication"
		log.Printf("[INFO] Item %d skipped: %s", nextItem.ID, msg)
		_ = w.engine.db.UpdateJobItemStatus(ctx, nextItem.ID, "skipped", msg, nil)

		w.engine.events.Emit(events.Event{
			Type:       events.JobSkipped,
			Source:     events.SourceWorker,
			TraceID:    traceID,
			Account:    acc.Name,
			ProductURL: nextItem.ProductURL,
			JobID:      &job.ID,
			JobItemID:  &nextItem.ID,
			Message:    msg,
			Metadata:   map[string]any{"reason": "out_of_stock"},
		})
		return
	}

	enrichBaseProduct(product)

	productForAccount := *product
	affiliateLink := utils.AddAffiliateTag(nextItem.ProductURL, acc.AffiliateTag, acc.ExtraParams)
	productForAccount.Link = affiliateLink

	if acc.UseAI {
		w.setPhase(phaseEnriching, acc.Name, nextItem.ProductURL, &job.ID)
		enrichCtx, cancel := context.WithTimeout(ai.WithTrace(ctx, traceID), enrichTimeout)
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

	w.setPhase(phasePublishing, acc.Name, nextItem.ProductURL, &job.ID)

	published, pubErr := w.engine.publishWithEvents(ctx, traceID, acc, models.PublishedPost{
		ProductTitle: productForAccount.Title,
		ProductURL:   nextItem.ProductURL,
		Content:      postText,
	}, &nextItem.ID)
	if pubErr != nil {
		errMsg := fmt.Sprintf("Facebook publish error: %v", pubErr)
		log.Printf("[ERR] Item %d Facebook API error: %s", nextItem.ID, errMsg)
		_ = w.engine.db.UpdateJobItemStatus(ctx, nextItem.ID, "failed", errMsg, nil)
		w.noteError(errMsg)
		return
	}

	publishedAt := time.Now()
	w.notePublished(publishedAt)
	if err := w.engine.db.UpdateJobItemStatus(ctx, nextItem.ID, "published", "", &publishedAt); err != nil {
		log.Printf("[ERR] Failed to update job item to published: %v", err)
	}

	log.Printf("[INFO] Item %d published successfully to page %s. PostID: %s", nextItem.ID, acc.FacebookPageID, published.PostID)

	select {
	case <-w.stopChan:
		return
	case <-time.After(w.cooldown):
	}
}
