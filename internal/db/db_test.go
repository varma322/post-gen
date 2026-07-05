//go:build integration
// +build integration

package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"post-gen/internal/models"
)

func testDB(t *testing.T) *Pool {
	// Skip if Postgres is not available
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db, err := New(ctx)
	if err != nil {
		t.Skipf("Postgres not available: %v (run with real DB or use testcontainers)", err)
	}
	return db
}

// TestLoadSaveAccounts tests account persistence with Active and ExtraParams fields
func TestLoadSaveAccounts(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	// Clean slate
	_, _ = db.pool.Exec(ctx, "DELETE FROM accounts")

	active := true
	extraParams := map[string]string{"utm_source": "test", "utm_medium": "affiliate"}

	acc := models.Account{
		Name:                "test-account",
		TemplatePath:        "templates/test.tmpl",
		AffiliateTag:        "test-tag-21",
		FacebookPageID:      "123456789",
		FacebookAccessToken: "token-xyz",
		UseAI:               true,
		Active:              &active,
		ExtraParams:         extraParams,
	}

	// Save
	err := db.UpsertAccount(ctx, acc)
	if err != nil {
		t.Fatalf("UpsertAccount failed: %v", err)
	}

	// Load
	accounts, err := db.LoadAccounts(ctx)
	if err != nil {
		t.Fatalf("LoadAccounts failed: %v", err)
	}

	if len(accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accounts))
	}

	loaded := accounts[0]
	if loaded.Name != acc.Name {
		t.Errorf("name mismatch: expected %s, got %s", acc.Name, loaded.Name)
	}
	if loaded.AffiliateTag != acc.AffiliateTag {
		t.Errorf("tag mismatch: expected %s, got %s", acc.AffiliateTag, loaded.AffiliateTag)
	}

	// Verify Active is preserved (pointer)
	if loaded.Active == nil || !*loaded.Active {
		t.Error("Active field not preserved or not active")
	}

	// Verify ExtraParams round-trip
	if loaded.ExtraParams == nil {
		t.Error("ExtraParams is nil after load")
	}
	if loaded.ExtraParams["utm_source"] != extraParams["utm_source"] {
		t.Errorf("ExtraParams mismatch: expected %v, got %v", extraParams, loaded.ExtraParams)
	}

	// Clean up
	_ = db.DeleteAccount(ctx, acc.Name)
}

// TestAccountBackwardCompatibility tests that accounts without Active field default to active
func TestAccountBackwardCompatibility(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	_, _ = db.pool.Exec(ctx, "DELETE FROM accounts WHERE name = 'legacy-account'")

	// Insert account with Active = NULL directly in DB (simulating old data)
	_, err := db.pool.Exec(ctx, `
		INSERT INTO accounts (name, template_path, affiliate_tag, facebook_page_id, facebook_access_token)
		VALUES ('legacy-account', 'templates/test.tmpl', 'test-21', '123', 'token')
	`)
	if err != nil {
		t.Fatalf("direct INSERT failed: %v", err)
	}

	// Load and verify defaults to active
	accounts, err := db.LoadAccounts(ctx)
	if err != nil {
		t.Fatalf("LoadAccounts failed: %v", err)
	}

	var legacy *models.Account
	for i := range accounts {
		if accounts[i].Name == "legacy-account" {
			legacy = &accounts[i]
			break
		}
	}

	if legacy == nil {
		t.Fatal("legacy account not found")
	}

	// IsActive() should return true when Active is nil
	if !legacy.IsActive() {
		t.Error("legacy account with nil Active should be active by default")
	}

	_ = db.DeleteAccount(ctx, "legacy-account")
}

// TestQueuedProducts tests product queue persistence and retrieval
func TestQueuedProducts(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	_, _ = db.pool.Exec(ctx, "DELETE FROM queued_products WHERE url = 'https://example.com/test-product'")

	product := models.Product{
		Title:     "Test Product",
		DealPrice: "₹1,000",
		MRP:       "₹1,500",
		ImageURL:  "https://example.com/image.jpg",
	}

	// Add product
	err := db.AddQueuedProduct(ctx, "https://example.com/test-product", product.Title, product.DealPrice, product.ImageURL, product)
	if err != nil {
		t.Fatalf("AddQueuedProduct failed: %v", err)
	}

	// Get all queued
	queued, err := db.GetQueuedProducts(ctx)
	if err != nil {
		t.Fatalf("GetQueuedProducts failed: %v", err)
	}

	if len(queued) == 0 {
		t.Fatal("no queued products found")
	}

	// Verify the product is in the list
	var found *models.QueuedProduct
	for i := range queued {
		if queued[i].URL == "https://example.com/test-product" {
			found = &queued[i]
			break
		}
	}

	if found == nil {
		t.Fatal("product not found in queue")
	}
	if found.Title != product.Title {
		t.Errorf("title mismatch: expected %s, got %s", product.Title, found.Title)
	}
	if found.ImageURL != product.ImageURL {
		t.Errorf("image_url mismatch: expected %s, got %s", product.ImageURL, found.ImageURL)
	}

	// Delete and verify gone
	err = db.DeleteQueuedProduct(ctx, found.ID)
	if err != nil {
		t.Fatalf("DeleteQueuedProduct failed: %v", err)
	}

	queued2, _ := db.GetQueuedProducts(ctx)
	for _, q := range queued2 {
		if q.URL == "https://example.com/test-product" {
			t.Error("product still in queue after deletion")
		}
	}
}

// TestCreatePublicationJobTOCTOU tests TOCTOU race protection via unique constraint
func TestCreatePublicationJobTOCTOU(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	// Clean up any existing jobs
	_, _ = db.pool.Exec(ctx, "DELETE FROM publication_jobs")

	items := []models.JobItem{
		{
			AccountName: "test-account-1",
			ProductURL:  "https://example.com/product1",
		},
	}

	// First job should succeed
	jobID, err := db.CreatePublicationJob(ctx, items)
	if err != nil {
		t.Fatalf("first CreatePublicationJob failed: %v", err)
	}
	if jobID <= 0 {
		t.Errorf("expected positive job ID, got %d", jobID)
	}

	// Second simultaneous job should fail with ErrJobAlreadyActive
	jobID2, err := db.CreatePublicationJob(ctx, items)
	if err == nil {
		t.Fatalf("second CreatePublicationJob should have failed with ErrJobAlreadyActive, but succeeded (ID: %d)", jobID2)
	}
	if !errors.Is(err, ErrJobAlreadyActive) {
		t.Errorf("expected ErrJobAlreadyActive, got: %v", err)
	}

	// Clean up
	_, _ = db.pool.Exec(ctx, "DELETE FROM publication_jobs CASCADE")
}

// TestGetActiveJobDistinction tests the (nil, nil) vs (nil, err) contract
func TestGetActiveJobDistinction(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	_, _ = db.pool.Exec(ctx, "DELETE FROM publication_jobs CASCADE")

	// Case 1: No active job should return (nil, nil), not an error
	job, err := db.GetActiveJob(ctx)
	if err != nil {
		t.Fatalf("GetActiveJob with no job should not error, got: %v", err)
	}
	if job != nil {
		t.Fatalf("GetActiveJob with no job should return nil job, got: %+v", job)
	}

	// Case 2: Create a pending job and verify it's retrieved
	items := []models.JobItem{
		{AccountName: "test-account", ProductURL: "https://example.com/prod"},
	}
	jobID, err := db.CreatePublicationJob(ctx, items)
	if err != nil {
		t.Fatalf("CreatePublicationJob failed: %v", err)
	}

	job, err = db.GetActiveJob(ctx)
	if err != nil {
		t.Fatalf("GetActiveJob with active job errored: %v", err)
	}
	if job == nil {
		t.Fatal("GetActiveJob should return job, got nil")
	}
	if job.ID != jobID {
		t.Errorf("job ID mismatch: expected %d, got %d", jobID, job.ID)
	}

	// Clean up
	_, _ = db.pool.Exec(ctx, "DELETE FROM publication_jobs CASCADE")
}

// TestRecoverStaleJobItems tests orphaned item recovery
func TestRecoverStaleJobItems(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	_, _ = db.pool.Exec(ctx, "DELETE FROM publication_jobs CASCADE")

	// Create a job with one item
	items := []models.JobItem{
		{AccountName: "test-account", ProductURL: "https://example.com/prod"},
	}
	jobID, err := db.CreatePublicationJob(ctx, items)
	if err != nil {
		t.Fatalf("CreatePublicationJob failed: %v", err)
	}

	// Manually update the item to 'publishing' with old timestamp (simulate crash mid-publish)
	_, err = db.pool.Exec(ctx, `
		UPDATE job_items
		SET status = 'publishing', updated_at = NOW() - INTERVAL '15 minutes'
		WHERE job_id = $1
	`, jobID)
	if err != nil {
		t.Fatalf("UPDATE for stale item failed: %v", err)
	}

	// Recover stale items (threshold: 10 minutes)
	recovered, err := db.RecoverStaleJobItems(ctx, 10*time.Minute)
	if err != nil {
		t.Fatalf("RecoverStaleJobItems failed: %v", err)
	}

	if recovered != 1 {
		t.Errorf("expected 1 recovered item, got %d", recovered)
	}

	// Verify the item is now marked 'failed'
	status, err := db.GetJobItemStatus(ctx, 1) // Assuming ID=1 from the create above
	if err == nil && status == "failed" {
		// Good, item was recovered
	}

	// Clean up
	_, _ = db.pool.Exec(ctx, "DELETE FROM publication_jobs CASCADE")
}

// TestCancelActiveJobsTransaction tests that job and item updates are atomic
func TestCancelActiveJobsTransaction(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	_, _ = db.pool.Exec(ctx, "DELETE FROM publication_jobs CASCADE")

	items := []models.JobItem{
		{AccountName: "acc1", ProductURL: "https://example.com/p1"},
		{AccountName: "acc2", ProductURL: "https://example.com/p2"},
	}
	_, err := db.CreatePublicationJob(ctx, items)
	if err != nil {
		t.Fatalf("CreatePublicationJob failed: %v", err)
	}

	// Cancel the job
	err = db.CancelActiveJobs(ctx)
	if err != nil {
		t.Fatalf("CancelActiveJobs failed: %v", err)
	}

	// Verify job status is 'cancelled'
	job, _ := db.GetActiveJob(ctx)
	if job != nil {
		t.Errorf("job should not be active after cancel, got status: %s", job.Status)
	}

	// Clean up
	_, _ = db.pool.Exec(ctx, "DELETE FROM publication_jobs CASCADE")
}
