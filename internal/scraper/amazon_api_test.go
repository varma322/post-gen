package scraper

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"post-gen/internal/models"
)

// MockScraper is a dummy Scraper for fallback testing.
type MockScraper struct {
	Called bool
	Result *models.Product
	Err    error
}

func (m *MockScraper) Scrape(url string) (*models.Product, error) {
	m.Called = true
	return m.Result, m.Err
}

func TestExtractASIN(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{"https://www.amazon.in/dp/B08G9J44ZN", "B08G9J44ZN"},
		{"https://www.amazon.com/gp/product/B08G9J44ZN?pf_rd_r=XYZ", "B08G9J44ZN"},
		{"https://amazon.de/product/B08G9J44ZN/", "B08G9J44ZN"},
		{"https://www.amazon.in/dp/B08G9J44ZN?th=1&psc=1", "B08G9J44ZN"},
		{"https://example.com/not-amazon/B08G9J44ZN", ""},
	}

	for _, tc := range tests {
		got := extractASIN(tc.url)
		if got != tc.expected {
			t.Errorf("extractASIN(%q) = %q; want %q", tc.url, got, tc.expected)
		}
	}
}

func TestGetMarketplace(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{"https://www.amazon.in/dp/B08G9J44ZN", "www.amazon.in"},
		{"https://amazon.com/dp/B08G9J44ZN", "www.amazon.com"},
		{"https://amazon.co.uk/dp/B08G9J44ZN", "www.amazon.co.uk"},
		{"https://amzn.to/123456", "www.amazon.in"}, // Short URLs resolve, fallback/default
	}

	for _, tc := range tests {
		got := getMarketplace(tc.url)
		if got != tc.expected {
			t.Errorf("getMarketplace(%q) = %q; want %q", tc.url, got, tc.expected)
		}
	}
}

func TestTokenManager_GetToken(t *testing.T) {
	var callCount int32

	// Setup mock OAuth server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST method, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Errorf("expected urlencoded content-type, got %s", r.Header.Get("Content-Type"))
		}

		atomic.AddInt32(&callCount, 1)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "mock-token-abc",
			"expires_in":   3600,
		})
	}))
	defer server.Close()

	tm := NewTokenManager("id", "secret", server.URL)

	// Fetch token first time
	token, err := tm.GetToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "mock-token-abc" {
		t.Errorf("expected token 'mock-token-abc', got %q", token)
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}

	// Fetch token second time (should be cached)
	token, err = tm.GetToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "mock-token-abc" {
		t.Errorf("expected token 'mock-token-abc', got %q", token)
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("expected call count to stay at 1 (cached), got %d", callCount)
	}
}

func TestTokenManager_GetToken_Expired(t *testing.T) {
	var callCount int32

	// Setup mock OAuth server returning short expiration
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "mock-token-expired",
			"expires_in":   0, // immediately expired under the 1-minute buffer check
		})
	}))
	defer server.Close()

	tm := NewTokenManager("id", "secret", server.URL)

	// Fetch token first time
	_, _ = tm.GetToken()
	// Fetch token second time (should refresh because expires_in is 0)
	_, _ = tm.GetToken()

	if atomic.LoadInt32(&callCount) != 2 {
		t.Errorf("expected 2 calls (no caching due to expiration), got %d", callCount)
	}
}

func TestAmazonCreatorAPIScraper_Scrape_Success(t *testing.T) {
	// Setup Mock API server
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Check marketplace routing headers
		if r.Header.Get("x-marketplace") != "www.amazon.in" {
			t.Errorf("expected x-marketplace header 'www.amazon.in', got %q", r.Header.Get("x-marketplace"))
		}

		response := apiGetItemsResponse{
			ItemsResult: &apiItemsResult{
				Items: []apiItem{
					{
						ASIN:          "B08G9J44ZN",
						DetailPageURL: "https://www.amazon.in/dp/B08G9J44ZN",
						ItemInfo: &apiItemInfo{
							Title: &apiTitle{
								DisplayValue: "Mock Phone 128GB",
							},
							Features: &apiFeatures{
								DisplayValues: []string{
									"Feature One",
									"Feature Two",
								},
							},
						},
						OffersV2: &apiOffersV2{
							Listings: []apiListing{
								{
									Price: &apiListingPrice{
										Money: &apiPriceInfo{
											DisplayAmount: "₹49,999",
											Amount:        49999.0,
											Currency:      "INR",
										},
										SavingBasis: &apiSaving{
											Money: &apiPriceInfo{
												DisplayAmount: "₹59,999",
												Amount:        59999.0,
												Currency:      "INR",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}

		_ = json.NewEncoder(w).Encode(response)
	}))
	defer apiServer.Close()

	// Setup Mock OAuth server
	oauthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "mock-token",
			"expires_in":   3600,
		})
	}))
	defer oauthServer.Close()

	fallback := &MockScraper{}
	scraper := NewAmazonCreatorAPIScraper("id", "secret", oauthServer.URL, "tag-21", fallback)
	
	oldTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = oldTransport }()

	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "creatorsapi.amazon" {
			// Redirect request to our test server
			targetURL, _ := url.Parse(apiServer.URL)
			req.URL.Scheme = targetURL.Scheme
			req.URL.Host = targetURL.Host
		}
		return oldTransport.RoundTrip(req)
	})

	product, err := scraper.Scrape("https://www.amazon.in/dp/B08G9J44ZN")
	if err != nil {
		t.Fatalf("scrape failed: %v", err)
	}

	if product.Title != "Mock Phone 128GB" {
		t.Errorf("expected title 'Mock Phone 128GB', got %q", product.Title)
	}
	if product.DealPrice != "49,999" {
		t.Errorf("expected deal price '49,999', got %q", product.DealPrice)
	}
	if product.MRP != "59,999" {
		t.Errorf("expected MRP '59,999', got %q", product.MRP)
	}
	if product.Discount != "17" { // ((59999 - 49999) / 59999) * 100 = 16.66% -> 17%
		t.Errorf("expected discount '17', got %q", product.Discount)
	}
	if len(product.Features) != 2 || product.Features[0] != "Feature One" {
		t.Errorf("unexpected features: %v", product.Features)
	}
	if fallback.Called {
		t.Error("fallback scraper was called but shouldn't have been")
	}
}

func TestAmazonCreatorAPIScraper_Fallback(t *testing.T) {
	// Setup Mock OAuth server returning error
	oauthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer oauthServer.Close()

	expectedProduct := &models.Product{Title: "Fallback Product Title", DealPrice: "123"}
	fallback := &MockScraper{Result: expectedProduct}
	scraper := NewAmazonCreatorAPIScraper("id", "secret", oauthServer.URL, "tag-21", fallback)

	product, err := scraper.Scrape("https://www.amazon.in/dp/B08G9J44ZN")
	if err != nil {
		t.Fatalf("expected no error (because fallback succeeded), got %v", err)
	}

	if !fallback.Called {
		t.Error("expected fallback scraper to be called, but it wasn't")
	}
	if product.Title != "Fallback Product Title" {
		t.Errorf("expected product title %q, got %q", expectedProduct.Title, product.Title)
	}
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
