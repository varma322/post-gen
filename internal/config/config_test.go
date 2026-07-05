package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"post-gen/internal/models"
)

// TestLoadSaveAccountsJSON tests account JSON persistence with optional fields
func TestLoadSaveAccountsJSON(t *testing.T) {
	tmpDir := t.TempDir()
	accountsPath := filepath.Join(tmpDir, "accounts.json")

	// Create test accounts with all fields including optional ones
	active := true
	extraParams := map[string]string{"utm_source": "test"}

	accounts := []models.Account{
		{
			Name:                "account1",
			TemplatePath:        "templates/test.tmpl",
			AffiliateTag:        "test-21",
			FacebookPageID:      "12345",
			FacebookAccessToken: "token1",
			UseAI:               true,
			Active:              &active,
			ExtraParams:         extraParams,
		},
		{
			Name:                "account2",
			TemplatePath:        "templates/test2.tmpl",
			AffiliateTag:        "test2-21",
			FacebookPageID:      "67890",
			FacebookAccessToken: "token2",
			UseAI:               false,
			// Active: nil (omitted) - should default to active on load
			ExtraParams: nil,
		},
	}

	// Save
	err := SaveAccounts(accountsPath, accounts)
	if err != nil {
		t.Fatalf("SaveAccounts failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(accountsPath); os.IsNotExist(err) {
		t.Fatal("accounts.json was not created")
	}

	// Load
	loaded, err := LoadAccounts(accountsPath)
	if err != nil {
		t.Fatalf("LoadAccounts failed: %v", err)
	}

	if len(loaded) != len(accounts) {
		t.Fatalf("expected %d accounts, got %d", len(accounts), len(loaded))
	}

	// Verify account1 (all fields set)
	if loaded[0].Name != "account1" {
		t.Errorf("account1 name mismatch")
	}
	if loaded[0].Active == nil || !*loaded[0].Active {
		t.Error("account1 Active not preserved")
	}
	if loaded[0].ExtraParams == nil || loaded[0].ExtraParams["utm_source"] != "test" {
		t.Error("account1 ExtraParams not preserved")
	}

	// Verify account2 (optional fields not set)
	if loaded[1].Name != "account2" {
		t.Errorf("account2 name mismatch")
	}
	if !loaded[1].IsActive() {
		t.Error("account2 should default to active when Active is nil")
	}
	// ExtraParams may be nil or empty map, both are acceptable
}

// TestBackwardCompatibilityOldJSON tests loading accounts.json without Active/ExtraParams fields
func TestBackwardCompatibilityOldJSON(t *testing.T) {
	tmpDir := t.TempDir()
	accountsPath := filepath.Join(tmpDir, "old_accounts.json")

	// Create old-format JSON (no Active, no ExtraParams)
	oldData := []map[string]interface{}{
		{
			"name":                    "legacy",
			"template_path":           "templates/test.tmpl",
			"affiliate_tag":           "legacy-21",
			"facebook_page_id":        "123",
			"facebook_access_token":   "token",
			"use_ai":                  true,
			"ai_prompt":               "",
		},
	}

	rawBytes, _ := json.MarshalIndent(oldData, "", "  ")
	err := os.WriteFile(accountsPath, rawBytes, 0644)
	if err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Load and verify backward compatibility
	accounts, err := LoadAccounts(accountsPath)
	if err != nil {
		t.Fatalf("LoadAccounts with old JSON failed: %v", err)
	}

	if len(accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accounts))
	}

	// Verify the account loaded correctly despite missing fields
	acc := accounts[0]
	if acc.Name != "legacy" {
		t.Errorf("name mismatch: expected 'legacy', got %s", acc.Name)
	}
	if acc.AffiliateTag != "legacy-21" {
		t.Errorf("tag mismatch: expected 'legacy-21', got %s", acc.AffiliateTag)
	}

	// Verify defaults work
	if !acc.IsActive() {
		t.Error("legacy account should default to active")
	}
}

// TestLoadSelectorsJSON tests selector loading and platform support
func TestLoadSelectorsJSON(t *testing.T) {
	tmpDir := t.TempDir()
	selectorsPath := filepath.Join(tmpDir, "selectors.json")

	// Create test selectors JSON
	selectors := map[string]interface{}{
		"amazon": map[string]string{
			"title":    "#productTitle",
			"price":    "#corePriceDisplay_desktop_feature_div .a-price-whole",
			"mrp":      ".basisPrice .a-offscreen",
			"features": "#feature-bullets li span.a-list-item",
			"image":    "#landingImage, #imgBlkFront, #main-image",
		},
	}

	rawBytes, _ := json.MarshalIndent(selectors, "", "  ")
	err := os.WriteFile(selectorsPath, rawBytes, 0644)
	if err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Load
	loaded, err := LoadSelectors(selectorsPath)
	if err != nil {
		t.Fatalf("LoadSelectors failed: %v", err)
	}

	// Verify Amazon platform loaded
	amazon, ok := loaded["amazon"]
	if !ok {
		t.Fatal("amazon platform not found in selectors")
	}

	if amazon.Title != "#productTitle" {
		t.Errorf("title selector mismatch: expected '#productTitle', got %s", amazon.Title)
	}

	if amazon.Image != "#landingImage, #imgBlkFront, #main-image" {
		t.Errorf("image selector mismatch")
	}
}

// TestSelectorsWithMissingFields tests graceful handling of missing optional selector fields
func TestSelectorsWithMissingFields(t *testing.T) {
	tmpDir := t.TempDir()
	selectorsPath := filepath.Join(tmpDir, "minimal_selectors.json")

	// Minimal selectors (image field missing)
	selectors := map[string]interface{}{
		"amazon": map[string]string{
			"title":    "#productTitle",
			"price":    "#price",
			// "image" and other fields omitted
		},
	}

	rawBytes, _ := json.MarshalIndent(selectors, "", "  ")
	err := os.WriteFile(selectorsPath, rawBytes, 0644)
	if err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Load should succeed even with missing fields
	loaded, err := LoadSelectors(selectorsPath)
	if err != nil {
		t.Fatalf("LoadSelectors with missing fields failed: %v", err)
	}

	amazon := loaded["amazon"]
	if amazon.Title != "#productTitle" {
		t.Error("title not loaded correctly")
	}
	if amazon.Image != "" {
		t.Errorf("image should be empty string when not provided, got %s", amazon.Image)
	}
}

// TestAccountRoundTripWithAllFields tests full round-trip with maximal field set
func TestAccountRoundTripWithAllFields(t *testing.T) {
	tmpDir := t.TempDir()
	accountsPath := filepath.Join(tmpDir, "full_accounts.json")

	active := true
	extraParams := map[string]string{
		"utm_source":   "affiliate",
		"utm_medium":   "cpa",
		"utm_campaign": "summer_sale",
	}

	original := []models.Account{
		{
			Name:                "complete-account",
			TemplatePath:        "templates/amazon.tmpl",
			AffiliateTag:        "mysite-21",
			FacebookPageID:      "1234567890",
			FacebookAccessToken: "EAAT1234...",
			UseAI:               true,
			AIPrompt:            "Custom AI prompt",
			Active:              &active,
			ExtraParams:         extraParams,
			MaxPostsPerDay:      5,
			ActiveHoursStart:    "09:00",
			ActiveHoursEnd:      "21:00",
			MinDelayMinutes:     60,
		},
	}

	// Save
	err := SaveAccounts(accountsPath, original)
	if err != nil {
		t.Fatalf("SaveAccounts failed: %v", err)
	}

	// Load
	loaded, err := LoadAccounts(accountsPath)
	if err != nil {
		t.Fatalf("LoadAccounts failed: %v", err)
	}

	if len(loaded) != 1 {
		t.Fatalf("expected 1 account, got %d", len(loaded))
	}

	acc := loaded[0]

	// Verify all fields
	if acc.Name != original[0].Name {
		t.Errorf("name mismatch")
	}
	if acc.TemplatePath != original[0].TemplatePath {
		t.Errorf("template_path mismatch")
	}
	if acc.AffiliateTag != original[0].AffiliateTag {
		t.Errorf("affiliate_tag mismatch")
	}
	if acc.FacebookPageID != original[0].FacebookPageID {
		t.Errorf("facebook_page_id mismatch")
	}
	if acc.FacebookAccessToken != original[0].FacebookAccessToken {
		t.Errorf("facebook_access_token mismatch")
	}
	if acc.UseAI != original[0].UseAI {
		t.Errorf("use_ai mismatch")
	}
	if acc.AIPrompt != original[0].AIPrompt {
		t.Errorf("ai_prompt mismatch")
	}

	// Verify optional fields
	if acc.Active == nil || *acc.Active != *original[0].Active {
		t.Errorf("active mismatch")
	}

	if len(acc.ExtraParams) != len(original[0].ExtraParams) {
		t.Errorf("extra_params length mismatch: expected %d, got %d", len(original[0].ExtraParams), len(acc.ExtraParams))
	}
	for k, v := range original[0].ExtraParams {
		if acc.ExtraParams[k] != v {
			t.Errorf("extra_params[%s] mismatch: expected %s, got %s", k, v, acc.ExtraParams[k])
		}
	}

	// Verify scheduling/rate-limit fields
	if acc.MaxPostsPerDay != original[0].MaxPostsPerDay {
		t.Errorf("max_posts_per_day mismatch: expected %d, got %d", original[0].MaxPostsPerDay, acc.MaxPostsPerDay)
	}
	if acc.ActiveHoursStart != original[0].ActiveHoursStart {
		t.Errorf("active_hours_start mismatch: expected %s, got %s", original[0].ActiveHoursStart, acc.ActiveHoursStart)
	}
	if acc.ActiveHoursEnd != original[0].ActiveHoursEnd {
		t.Errorf("active_hours_end mismatch: expected %s, got %s", original[0].ActiveHoursEnd, acc.ActiveHoursEnd)
	}
	if acc.MinDelayMinutes != original[0].MinDelayMinutes {
		t.Errorf("min_delay_minutes mismatch: expected %d, got %d", original[0].MinDelayMinutes, acc.MinDelayMinutes)
	}
}
