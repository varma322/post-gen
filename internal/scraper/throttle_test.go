package scraper

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"post-gen/internal/models"
)

// throttleServers stands up a token endpoint and a catalog endpoint that
// answers with the given handler, routing creatorsapi.amazon to the latter.
func throttleServers(t *testing.T, fallback Scraper, handler http.HandlerFunc) *AmazonCreatorAPIScraper {
	t.Helper()

	apiServer := httptest.NewServer(handler)
	t.Cleanup(apiServer.Close)

	oauthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "expires_in": 3600})
	}))
	t.Cleanup(oauthServer.Close)

	oldTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = oldTransport })
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "creatorsapi.amazon" {
			target, _ := url.Parse(apiServer.URL)
			req.URL.Scheme, req.URL.Host = target.Scheme, target.Host
		}
		return oldTransport.RoundTrip(req)
	})

	return NewAmazonCreatorAPIScraper("id", "secret", oauthServer.URL, "throttle-test-21", fallback)
}

func throttleHandler(t *testing.T, calls *int, retryAfter int) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		*calls++
		if retryAfter > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		}
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message":"Request rate limit exceeded.","type":"ThrottleException"}`))
	}
}

func TestThrottlingFallsBackToHTMLInsteadOfFailing(t *testing.T) {
	// The bug this pins: a 429 was classified as a network failure, which
	// aborted the HTML fallback and failed the post outright. A 429 is a
	// well-formed answer from the API host and says nothing about whether the
	// storefront will serve a product page.
	expected := &models.Product{Title: "Castrol Activ", DealPrice: "419"}
	fallback := &MockScraper{Result: expected}

	var calls int
	scraper := throttleServers(t, fallback, throttleHandler(t, &calls, 0))

	// The circuit breaker is package-global, so each test scopes itself to its
	// own partner tag rather than inheriting another test's open circuit.
	ctx := WithPartnerTag(context.Background(), "throttle-fallback-21")

	product, meta, err := scraper.ScrapeWithMeta(ctx, "https://www.amazon.in/dp/B0BL1RP9RX")
	if err != nil {
		t.Fatalf("a throttled lookup should fall back, not fail: %v", err)
	}
	if !fallback.Called {
		t.Fatal("the HTML fallback was not used")
	}
	if product.Title != expected.Title {
		t.Errorf("product = %q, want the fallback's result", product.Title)
	}
	if meta.FallbackReason != "rate limited" {
		t.Errorf("fallback reason = %q, want %q", meta.FallbackReason, "rate limited")
	}
}

func TestThrottlingIsNotRetried(t *testing.T) {
	// Three attempts in seven seconds against an exhausted quota spends more
	// of the quota and delays the fallback that would have worked.
	var calls int
	scraper := throttleServers(t, &MockScraper{Result: &models.Product{Title: "x"}}, throttleHandler(t, &calls, 0))

	ctx := WithPartnerTag(context.Background(), "throttle-noretry-21")

	if _, _, err := scraper.ScrapeWithMeta(ctx, "https://www.amazon.in/dp/B0BL1RP9RX"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("made %d API calls on a 429, want exactly 1", calls)
	}
}

func TestThrottlingTripsTheCircuitSoLaterItemsSkipTheAPI(t *testing.T) {
	var calls int
	fallback := &MockScraper{Result: &models.Product{Title: "x"}}
	scraper := throttleServers(t, fallback, throttleHandler(t, &calls, 0))

	ctx := WithPartnerTag(context.Background(), "throttle-circuit-21")

	for i := 0; i < 3; i++ {
		if _, _, err := scraper.ScrapeWithMeta(ctx, "https://www.amazon.in/dp/B0BL1RP9RX"); err != nil {
			t.Fatalf("item %d failed: %v", i, err)
		}
	}

	if calls != 1 {
		t.Errorf("made %d API calls across three items; the circuit should have stopped after the first", calls)
	}
	if !creatorAPICircuitOpen("throttle-circuit-21", "www.amazon.in") {
		t.Error("expected the circuit open after a 429")
	}
}

func TestRetryAfterSizesTheCooldown(t *testing.T) {
	err := &throttleError{retryAfter: 90 * time.Second}
	if got := err.cooldown(); got != 90*time.Second {
		t.Errorf("cooldown = %s, want the server's Retry-After", got)
	}

	none := &throttleError{}
	if got := none.cooldown(); got != defaultThrottleCooldown {
		t.Errorf("cooldown = %s, want the default", got)
	}

	if !errors.Is(err, errCreatorsAPIThrottled) {
		t.Error("a throttleError should match the throttled sentinel")
	}
	if errors.Is(err, errCreatorsAPINetworkFailure) {
		t.Error("throttling must not be classified as a network failure")
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		header string
		want   time.Duration
	}{
		{"60", 60 * time.Second},
		{" 5 ", 5 * time.Second},
		{"0", 0},
		{"", 0},
		{"Wed, 21 Oct 2026 07:28:00 GMT", 0},
	}
	for _, tc := range tests {
		if got := parseRetryAfter(tc.header); got != tc.want {
			t.Errorf("parseRetryAfter(%q) = %s, want %s", tc.header, got, tc.want)
		}
	}
}
