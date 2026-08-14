package scraper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"post-gen/internal/models"
	"post-gen/internal/utils"
)

var asinRegex = regexp.MustCompile(`(?i)/(?:dp|gp/product|gp/aw/d|product)/([a-z0-9]{10})(?:[/?]|$)`)

// Sentinel errors used to classify Creators API failures without fragile
// string-matching on wrapped error text.
var (
	errCreatorsAPIIneligible     = errors.New("creators api: associate not eligible")
	errCreatorsAPINetworkFailure = errors.New("creators api: network failure")
)

// Circuit breaker: disables Creators API calls per partner-tag/marketplace pair
// when that pair is reported ineligible, so one ineligible combination doesn't
// disable the API for every other account/marketplace sharing the process.
var (
	apiCircuitMu    sync.RWMutex
	apiCircuitUntil = make(map[string]time.Time)
)

func circuitKey(partnerTag, marketplace string) string {
	return partnerTag + "|" + marketplace
}

func creatorAPICircuitOpen(partnerTag, marketplace string) bool {
	apiCircuitMu.RLock()
	defer apiCircuitMu.RUnlock()
	until, ok := apiCircuitUntil[circuitKey(partnerTag, marketplace)]
	return ok && time.Now().Before(until)
}

func tripCreatorAPICircuit(partnerTag, marketplace string, d time.Duration) {
	apiCircuitMu.Lock()
	defer apiCircuitMu.Unlock()
	apiCircuitUntil[circuitKey(partnerTag, marketplace)] = time.Now().Add(d)
}

// GetCircuitBreakerStatus returns the current state of all open circuits as a list of (partnerTag, marketplace, until) tuples.
// Used for health check and observability endpoints.
func GetCircuitBreakerStatus() []struct {
	PartnerTag  string
	Marketplace string
	Until       time.Time
} {
	apiCircuitMu.RLock()
	defer apiCircuitMu.RUnlock()

	var result []struct {
		PartnerTag  string
		Marketplace string
		Until       time.Time
	}

	for key, until := range apiCircuitUntil {
		if time.Now().Before(until) {
			parts := strings.Split(key, "|")
			if len(parts) == 2 {
				result = append(result, struct {
					PartnerTag  string
					Marketplace string
					Until       time.Time
				}{
					PartnerTag:  parts[0],
					Marketplace: parts[1],
					Until:       until,
				})
			}
		}
	}

	return result
}

// TokenManager coordinates OAuth2 client credentials token request and thread-safe caching.
type TokenManager struct {
	clientID     string
	clientSecret string
	tokenURL     string
	mu           sync.RWMutex
	token        string
	expiresAt    time.Time
}

// NewTokenManager initializes a new TokenManager.
func NewTokenManager(clientID, clientSecret, tokenURL string) *TokenManager {
	if tokenURL == "" {
		tokenURL = "https://api.amazon.com/auth/o2/token"
	}
	return &TokenManager{
		clientID:     clientID,
		clientSecret: clientSecret,
		tokenURL:     tokenURL,
	}
}

// GetToken returns a cached valid access token, or requests a new one if expired.
func (tm *TokenManager) GetToken() (string, error) {
	tm.mu.RLock()
	// Use cached token if valid and not within 1 minute of expiring
	if tm.token != "" && time.Now().Add(1*time.Minute).Before(tm.expiresAt) {
		token := tm.token
		tm.mu.RUnlock()
		return token, nil
	}
	tm.mu.RUnlock()

	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Double-check under write lock
	if tm.token != "" && time.Now().Add(1*time.Minute).Before(tm.expiresAt) {
		return tm.token, nil
	}

	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", tm.clientID)
	data.Set("client_secret", tm.clientSecret)
	data.Set("scope", "creatorsapi::default")

	var resp *http.Response
	var err error
	client := &http.Client{Timeout: 10 * time.Second}
	maxRetries := 3

	for attempt := 1; attempt <= maxRetries; attempt++ {
		req, reqErr := http.NewRequest("POST", tm.tokenURL, strings.NewReader(data.Encode()))
		if reqErr != nil {
			return "", fmt.Errorf("creating token request: %w", reqErr)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		resp, err = client.Do(req)
		if err == nil {
			if resp.StatusCode == 429 || resp.StatusCode >= 500 {
				snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
				resp.Body.Close()
				err = fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
			} else {
				break
			}
		}

		log.Printf("[WARN] Creators API Token request attempt %d/%d failed: %v", attempt, maxRetries, err)
		if attempt < maxRetries {
			time.Sleep(time.Duration(attempt*2) * time.Second)
		}
	}

	if err != nil {
		return "", fmt.Errorf("executing token request after %d attempts: %w", maxRetries, err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading token response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("OAuth token request failed (HTTP %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var res struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(bodyBytes, &res); err != nil {
		return "", fmt.Errorf("decoding token response: %w", err)
	}

	tm.token = res.AccessToken
	tm.expiresAt = time.Now().Add(time.Duration(res.ExpiresIn) * time.Second)
	return tm.token, nil
}

// AmazonCreatorAPIScraper fetches Amazon product details using the official Creators API.
// If not configured, or if the API call fails, it gracefully falls back to the HTML scraper.
type AmazonCreatorAPIScraper struct {
	clientID          string
	clientSecret      string
	tokenURL          string
	defaultPartnerTag string
	tokenManager      *TokenManager
	fallback          Scraper
}

// NewAmazonCreatorAPIScraper initializes the Creators API client wrapper.
func NewAmazonCreatorAPIScraper(clientID, clientSecret, tokenURL, defaultPartnerTag string, fallback Scraper) *AmazonCreatorAPIScraper {
	return &AmazonCreatorAPIScraper{
		clientID:          clientID,
		clientSecret:      clientSecret,
		tokenURL:          tokenURL,
		defaultPartnerTag: defaultPartnerTag,
		tokenManager:      NewTokenManager(clientID, clientSecret, tokenURL),
		fallback:          fallback,
	}
}

// effectivePartnerTag returns the configured partner tag, or the hardcoded
// default Indian Associates tag if none was configured.
func (s *AmazonCreatorAPIScraper) effectivePartnerTag() string {
	if s.defaultPartnerTag != "" {
		return s.defaultPartnerTag
	}
	return "notyoffers-21"
}

// Scrape implements the Scraper interface.
func (s *AmazonCreatorAPIScraper) Scrape(ctx context.Context, rawURL string) (*models.Product, error) {
	product, _, err := s.ScrapeWithMeta(ctx, rawURL)
	return product, err
}

// ScrapeWithMeta implements MetaScraper, reporting whether the product came
// from the Creators API or the HTML fallback, and why it fell back.
func (s *AmazonCreatorAPIScraper) ScrapeWithMeta(ctx context.Context, rawURL string) (*models.Product, ScrapeMeta, error) {
	// Resolve short URLs first so the marketplace (used to scope the circuit
	// breaker below) can be determined.
	resolvedURL := utils.ResolveAmazonShortURL(rawURL)
	marketplace := getMarketplace(resolvedURL)
	partnerTag := s.effectivePartnerTag()

	// fellBack runs the HTML scraper and tags the result with why we're here.
	fellBack := func(reason string) (*models.Product, ScrapeMeta, error) {
		product, err := s.fallback.Scrape(ctx, rawURL)
		return product, ScrapeMeta{Source: "html", FallbackReason: reason}, err
	}

	if creatorAPICircuitOpen(partnerTag, marketplace) {
		return fellBack("circuit breaker open")
	}

	// Extract ASIN
	asin := extractASIN(resolvedURL)
	if asin == "" {
		log.Printf("[WARN] Creators API: failed to extract ASIN from %s. Falling back to HTML scraping.", rawURL)
		return fellBack("no ASIN in URL")
	}

	// Fetch from Creators API
	product, err := s.fetchFromAPI(ctx, asin, marketplace, resolvedURL)
	if err != nil {
		if errors.Is(err, errCreatorsAPIIneligible) {
			tripCreatorAPICircuit(partnerTag, marketplace, 1*time.Hour)
			log.Printf("[WARN] Creators API: account not eligible for marketplace %s (partner tag rejected by Amazon). Disabling API for this partner tag/marketplace for 1 hour. Update AMAZON_CREATOR_PARTNER_TAG env var to an active Associates account tag.", marketplace)
			return fellBack("associate not eligible")
		}
		if errors.Is(err, errCreatorsAPINetworkFailure) || strings.Contains(err.Error(), "no such host") || strings.Contains(err.Error(), "timeout") {
			log.Printf("[ERR] Creators API failed due to network error: %v. Aborting HTML fallback to prevent CAPTCHA.", err)
			return nil, ScrapeMeta{Source: "creators_api"}, err
		}
		log.Printf("[WARN] Creators API failed: %v. Falling back to HTML scraping.", err)
		return fellBack("api error")
	}

	return product, ScrapeMeta{Source: "creators_api"}, nil
}

func (s *AmazonCreatorAPIScraper) fetchFromAPI(ctx context.Context, asin, marketplace, rawURL string) (*models.Product, error) {
	// Note: token acquisition failures are intentionally NOT wrapped with
	// errCreatorsAPINetworkFailure - a token-endpoint hiccup says nothing about
	// whether the product page itself is reachable, so it should fall through
	// to the generic "fall back to HTML" branch in Scrape(), not the abort branch.
	token, err := s.tokenManager.GetToken()
	if err != nil {
		return nil, fmt.Errorf("auth token error: %w", err)
	}

	partnerTag := s.effectivePartnerTag()

	payloadMap := map[string]interface{}{
		"itemIds":     []string{asin},
		"itemIdType":  "ASIN",
		"marketplace": marketplace,
		"partnerTag":  partnerTag,
		"resources": []string{
			"itemInfo.title",
			"itemInfo.features",
			"images.primary.large",
			"offersV2.listings.price",
		},
	}

	payloadBytes, err := json.Marshal(payloadMap)
	if err != nil {
		return nil, fmt.Errorf("marshaling API request payload: %w", err)
	}

	apiURL := "https://creatorsapi.amazon/catalog/v1/getItems"
	
	var resp *http.Response
	client := &http.Client{Timeout: 15 * time.Second}
	maxRetries := 3

	for attempt := 1; attempt <= maxRetries; attempt++ {
		req, reqErr := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(payloadBytes))
		if reqErr != nil {
			return nil, fmt.Errorf("creating API request: %w", reqErr)
		}

		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-marketplace", marketplace)

		resp, err = client.Do(req)
		if err == nil {
			if resp.StatusCode == 429 || resp.StatusCode >= 500 {
				snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
				resp.Body.Close()
				err = fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
			} else {
				break
			}
		}

		log.Printf("[WARN] Creators API GetItems request attempt %d/%d failed: %v", attempt, maxRetries, err)
		if attempt < maxRetries {
			time.Sleep(time.Duration(attempt*2) * time.Second)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("%w: sending API request after %d attempts: %v", errCreatorsAPINetworkFailure, maxRetries, err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading API response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		bodyStr := string(bodyBytes)
		if strings.Contains(bodyStr, "AssociateNotEligible") {
			return nil, fmt.Errorf("%w: API request failed (HTTP %d): %s", errCreatorsAPIIneligible, resp.StatusCode, bodyStr)
		}
		return nil, fmt.Errorf("API request failed (HTTP %d): %s", resp.StatusCode, bodyStr)
	}

	var apiResp apiGetItemsResponse
	if err := json.Unmarshal(bodyBytes, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshaling API response: %w", err)
	}

	if len(apiResp.Errors) > 0 {
		apiErr := fmt.Errorf("API returned errors: %s - %s", apiResp.Errors[0].Code, apiResp.Errors[0].Message)
		if apiResp.Errors[0].Code == "AssociateNotEligible" {
			apiErr = fmt.Errorf("%w: %v", errCreatorsAPIIneligible, apiErr)
		}
		return nil, apiErr
	}

	if apiResp.ItemsResult == nil || len(apiResp.ItemsResult.Items) == 0 {
		return nil, fmt.Errorf("no items found in API response for ASIN %s", asin)
	}

	item := apiResp.ItemsResult.Items[0]

	prod := &models.Product{
		Link: rawURL,
	}

	if item.ItemInfo != nil && item.ItemInfo.Title != nil {
		prod.Title = cleanText(item.ItemInfo.Title.DisplayValue)
	}

	if item.ItemInfo != nil && item.ItemInfo.Features != nil {
		for _, f := range item.ItemInfo.Features.DisplayValues {
			cleaned := cleanText(f)
			if cleaned != "" {
				prod.Features = append(prod.Features, cleaned)
			}
		}
		if len(prod.Features) > 6 {
			prod.Features = prod.Features[:6]
		}
	}

	if item.Images != nil && item.Images.Primary != nil && item.Images.Primary.Large != nil {
		prod.ImageURL = item.Images.Primary.Large.URL
	}

	if item.OffersV2 != nil && len(item.OffersV2.Listings) > 0 {
		listing := item.OffersV2.Listings[0]
		if listing.Price != nil {
			if listing.Price.Money != nil {
				prod.DealPrice = cleanPrice(listing.Price.Money.DisplayAmount)
			}
			if listing.Price.SavingBasis != nil && listing.Price.SavingBasis.Money != nil {
				prod.MRP = cleanPrice(listing.Price.SavingBasis.Money.DisplayAmount)
			}
		}
	}

	// Price fallbacks
	if prod.DealPrice == "" && prod.MRP != "" {
		prod.DealPrice = prod.MRP
	} else if prod.MRP == "" && prod.DealPrice != "" {
		prod.MRP = prod.DealPrice
	}

	if prod.DealPrice != "" && prod.MRP != "" {
		prod.Discount = calculateDiscount(prod.DealPrice, prod.MRP)
	}

	if prod.Title == "" {
		return nil, errors.New("empty product title in API response")
	}

	return prod, nil
}

func extractASIN(rawURL string) string {
	matches := asinRegex.FindStringSubmatch(rawURL)
	if len(matches) > 1 {
		return strings.ToUpper(matches[1])
	}
	return ""
}

func getMarketplace(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "www.amazon.in"
	}
	host := strings.ToLower(parsed.Host)
	if host == "" {
		return "www.amazon.in"
	}
	if strings.Contains(host, "amzn.") {
		return "www.amazon.in"
	}
	if !strings.HasPrefix(host, "www.") && strings.HasPrefix(host, "amazon.") {
		host = "www." + host
	}
	return host
}

// API response model mapping helper structures

type apiGetItemsResponse struct {
	ItemsResult *apiItemsResult `json:"itemsResult,omitempty"`
	Errors      []apiError      `json:"errors,omitempty"`
}

type apiItemsResult struct {
	Items []apiItem `json:"items,omitempty"`
}

type apiItem struct {
	ASIN          string       `json:"asin"`
	DetailPageURL string       `json:"detailPageUrl"`
	ItemInfo      *apiItemInfo `json:"itemInfo,omitempty"`
	Images        *apiImages   `json:"images,omitempty"`
	OffersV2      *apiOffersV2 `json:"offersV2,omitempty"`
}

type apiImages struct {
	Primary *apiImagePrimary `json:"primary,omitempty"`
}

type apiImagePrimary struct {
	Large *apiImageDetail `json:"large,omitempty"`
}

type apiImageDetail struct {
	URL string `json:"url"`
}

type apiItemInfo struct {
	Title    *apiTitle    `json:"title,omitempty"`
	Features *apiFeatures `json:"features,omitempty"`
}

type apiTitle struct {
	DisplayValue string `json:"displayValue"`
}

type apiFeatures struct {
	DisplayValues []string `json:"displayValues"`
}

type apiOffersV2 struct {
	Listings []apiListing `json:"listings,omitempty"`
}

type apiListing struct {
	Price *apiListingPrice `json:"price,omitempty"`
}

type apiListingPrice struct {
	Money       *apiPriceInfo `json:"money,omitempty"`
	SavingBasis *apiSaving    `json:"savingBasis,omitempty"`
	Savings     *apiSavings   `json:"savings,omitempty"`
}

type apiSaving struct {
	Money *apiPriceInfo `json:"money,omitempty"`
}

type apiSavings struct {
	Money      *apiPriceInfo `json:"money,omitempty"`
	Percentage int           `json:"percentage,omitempty"`
}

type apiPriceInfo struct {
	DisplayAmount string  `json:"displayAmount"`
	Amount        float64 `json:"amount"`
	Currency      string  `json:"currency"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
