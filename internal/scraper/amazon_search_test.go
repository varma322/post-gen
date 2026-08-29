package scraper

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// searchServers stands up a token endpoint and a catalog endpoint, routing
// creatorsapi.amazon to the latter, and records the payloads it receives.
func searchServers(t *testing.T, tag string, handler http.HandlerFunc) (*AmazonCreatorAPIScraper, *[]map[string]any) {
	t.Helper()

	var received []map[string]any

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		received = append(received, payload)
		handler(w, r)
	}))
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

	return NewAmazonCreatorAPIScraper("id", "secret", oauthServer.URL, tag, nil), &received
}

func searchJSON(items ...string) string {
	body := `{"searchResult":{"totalResultCount":306,"items":[`
	for i, item := range items {
		if i > 0 {
			body += ","
		}
		body += item
	}
	return body + `]}}`
}

const fullItem = `{
	"asin": "B0SEARCH001",
	"detailPageUrl": "https://www.amazon.in/dp/B0SEARCH001?tag=someoneelse-21",
	"itemInfo": {"title": {"displayValue": "Wireless Headphones"}},
	"images": {"primary": {"large": {"url": "https://m.media-amazon.com/i/x.jpg"}}},
	"offersV2": {"listings": [{"price": {
		"money": {"amount": 1499.5, "currency": "INR", "displayAmount": "₹1,499.50"},
		"savingBasis": {"money": {"amount": 4999, "currency": "INR"}},
		"savings": {"percentage": 70}
	}}]}
}`

func okHandler(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

func TestSearchItemsMapsCandidates(t *testing.T) {
	scraper, _ := searchServers(t, "search-map-21", okHandler(searchJSON(fullItem)))

	candidates, err := scraper.SearchItems(context.Background(), SearchOptions{Keywords: "headphones"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("got %d candidates, want 1", len(candidates))
	}

	got := candidates[0]
	if got.ASIN != "B0SEARCH001" {
		t.Errorf("ASIN = %q", got.ASIN)
	}
	if got.Title != "Wireless Headphones" {
		t.Errorf("Title = %q", got.Title)
	}
	if got.Price != 1499.5 {
		t.Errorf("Price = %v, want the numeric amount", got.Price)
	}
	if got.OldPrice != 4999 {
		t.Errorf("OldPrice = %v, want the savingBasis amount", got.OldPrice)
	}
	if got.DiscountPct != 70 {
		t.Errorf("DiscountPct = %d, want 70", got.DiscountPct)
	}
	if got.ImageURL == "" {
		t.Error("ImageURL should be carried across")
	}
}

func TestSearchItemsBuildsURLFromASINNotDetailPage(t *testing.T) {
	// detailPageUrl carries the tag the API call ran under. Letting that reach
	// a post would attribute the sale to the wrong account - the published link
	// must be rebuilt with the publishing page's own tag downstream.
	scraper, _ := searchServers(t, "search-url-21", okHandler(searchJSON(fullItem)))

	candidates, err := scraper.SearchItems(context.Background(), SearchOptions{Keywords: "headphones"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "https://www.amazon.in/dp/B0SEARCH001"
	if candidates[0].URL != want {
		t.Errorf("URL = %q, want %q with no tag on it", candidates[0].URL, want)
	}
}

func TestSearchItemsSkipsUnusableItems(t *testing.T) {
	noASIN := `{"itemInfo": {"title": {"displayValue": "Anonymous"}}}`
	noTitle := `{"asin": "B0NOTITLE01"}`

	scraper, _ := searchServers(t, "search-skip-21", okHandler(searchJSON(noASIN, noTitle, fullItem)))

	candidates, err := scraper.SearchItems(context.Background(), SearchOptions{Keywords: "headphones"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("got %d candidates, want only the complete one", len(candidates))
	}
	if candidates[0].ASIN != "B0SEARCH001" {
		t.Errorf("kept the wrong item: %q", candidates[0].ASIN)
	}
}

func TestSearchItemsDerivesDiscountWhenPercentageMissing(t *testing.T) {
	noPct := `{
		"asin": "B0DERIVE001",
		"itemInfo": {"title": {"displayValue": "Kettle"}},
		"offersV2": {"listings": [{"price": {
			"money": {"amount": 750},
			"savingBasis": {"money": {"amount": 1000}}
		}}]}
	}`

	scraper, _ := searchServers(t, "search-derive-21", okHandler(searchJSON(noPct)))

	candidates, err := scraper.SearchItems(context.Background(), SearchOptions{Keywords: "kettle"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if candidates[0].DiscountPct != 25 {
		t.Errorf("DiscountPct = %d, want 25 derived from the two prices", candidates[0].DiscountPct)
	}
}

func TestSearchItemsSendsOnlyTheFiltersThatWereSet(t *testing.T) {
	scraper, received := searchServers(t, "search-payload-21", okHandler(searchJSON(fullItem)))

	_, err := scraper.SearchItems(context.Background(), SearchOptions{
		BrowseNodeID: "976419031",
		MinSavingPct: 50,
		Page:         2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*received) != 1 {
		t.Fatalf("expected exactly one request, got %d", len(*received))
	}

	payload := (*received)[0]
	if payload["browseNodeId"] != "976419031" {
		t.Errorf("browseNodeId = %v", payload["browseNodeId"])
	}
	if payload["minSavingPercent"] != float64(50) {
		t.Errorf("minSavingPercent = %v", payload["minSavingPercent"])
	}
	if payload["itemPage"] != float64(2) {
		t.Errorf("itemPage = %v", payload["itemPage"])
	}
	if payload["partnerTag"] != "search-payload-21" {
		t.Errorf("partnerTag = %v, want the eligible account", payload["partnerTag"])
	}

	// Unset filters must be absent, not sent as zero: searchItems ignores
	// unknown and empty values silently, so a stray zero teaches nothing and a
	// stray empty string would be indistinguishable from a real filter.
	for _, absent := range []string{"keywords", "sortBy"} {
		if _, present := payload[absent]; present {
			t.Errorf("%q should be omitted when unset, got %v", absent, payload[absent])
		}
	}
}

func TestSearchItemsRequiresKeywordsOrBrowseNode(t *testing.T) {
	scraper, received := searchServers(t, "search-empty-21", okHandler(searchJSON()))

	if _, err := scraper.SearchItems(context.Background(), SearchOptions{MinSavingPct: 50}); err == nil {
		t.Error("expected a query with nothing to search on to be refused")
	}
	if len(*received) != 0 {
		t.Error("an unsearchable query should not spend a request")
	}
}

func TestSearchItemsSurfacesAValidationException(t *testing.T) {
	// A malformed browse node reports itself at the top level rather than in
	// errors[], which is how the live API answered a non-numeric node.
	validation := `{"message":"1 validation error detected: Value 'x' at 'browseNodeId' failed to satisfy constraint","type":"ValidationException"}`

	scraper, _ := searchServers(t, "search-validation-21", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(validation))
	})

	_, err := scraper.SearchItems(context.Background(), SearchOptions{Keywords: "x", BrowseNodeID: "1"})
	if err == nil {
		t.Fatal("expected a validation exception to surface as an error")
	}
	got := err.Error()
	if !strings.Contains(got, "ValidationException") || !strings.Contains(got, "browseNodeId") {
		t.Errorf("error %q should name the exception and the offending field", got)
	}
}

func TestSearchItemsTripsTheCircuitOnThrottle(t *testing.T) {
	var calls int
	scraper, _ := searchServers(t, "search-throttle-21", func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", strconv.Itoa(30))
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message":"Request rate limit exceeded.","type":"ThrottleException"}`))
	})

	if _, err := scraper.SearchItems(context.Background(), SearchOptions{Keywords: "x"}); err == nil {
		t.Fatal("expected a throttled search to return an error")
	}
	if calls != 1 {
		t.Errorf("made %d calls on a 429, want exactly 1", calls)
	}
	if !creatorAPICircuitOpen("search-throttle-21", "www.amazon.in") {
		t.Error("a 429 during discovery should trip the same circuit the lookup path uses")
	}

	// With the circuit open, a second search must not reach the network.
	_, err := scraper.SearchItems(context.Background(), SearchOptions{Keywords: "x"})
	if !errors.Is(err, ErrNoEligibleAccount) {
		t.Errorf("err = %v, want ErrNoEligibleAccount once the circuit is open", err)
	}
	if calls != 1 {
		t.Errorf("made %d calls; the open circuit should have stopped the second", calls)
	}
}

func TestSearchItemsFailsOverToAnEligibleAccount(t *testing.T) {
	// Credentials being issued is not proof of eligibility - Amazon grants API
	// access per Associates account, and one account in a registry can be
	// refused while another works. Discovery must land on the working one
	// rather than failing the whole query.
	const (
		refused = "failover-refused-21"
		working = "failover-working-21"
	)

	var tried []string

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		tag, _ := payload["partnerTag"].(string)
		tried = append(tried, tag)

		w.Header().Set("Content-Type", "application/json")
		if tag == refused {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"Your account does not currently meet the eligibility requirements.","reason":"AssociateNotEligible","type":"AccessDeniedException"}`))
			return
		}
		_, _ = w.Write([]byte(searchJSON(fullItem)))
	}))
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

	registry := NewCredentialRegistry([]APICredential{
		{Tag: refused, ClientID: "id", ClientSecret: "secret", TokenURL: oauthServer.URL},
		{Tag: working, ClientID: "id", ClientSecret: "secret", TokenURL: oauthServer.URL},
	})
	scraper := NewAmazonCreatorAPIScraperWithRegistry(registry, nil)

	// Name the refused account as preferred so the first attempt is
	// deterministic; otherwise the rotation might pick the working one and the
	// failover path would never be exercised.
	ctx := WithPartnerTag(context.Background(), refused)

	candidates, err := scraper.SearchItems(ctx, SearchOptions{Keywords: "headphones"})
	if err != nil {
		t.Fatalf("expected failover to the eligible account, got: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("got %d candidates, want the working account's result", len(candidates))
	}

	if !creatorAPICircuitOpen(refused, "www.amazon.in") {
		t.Error("the refused account's circuit should be open so later queries skip it")
	}
	if creatorAPICircuitOpen(working, "www.amazon.in") {
		t.Error("the working account should not have been penalised")
	}

	// The next query must go straight to the working account, even though the
	// refused one is still named as preferred: an open circuit outranks the
	// preference.
	before := len(tried)
	if _, err := scraper.SearchItems(ctx, SearchOptions{Keywords: "headphones"}); err != nil {
		t.Fatalf("second search failed: %v", err)
	}
	for _, tag := range tried[before:] {
		if tag == refused {
			t.Error("a later query still tried the account whose circuit is open")
		}
	}
}

func TestSearchItemsWithoutAPartnerTagDoesNotCall(t *testing.T) {
	scraper := &AmazonCreatorAPIScraper{}

	_, err := scraper.SearchItems(context.Background(), SearchOptions{Keywords: "x"})
	if !errors.Is(err, ErrNoEligibleAccount) {
		t.Errorf("err = %v, want ErrNoEligibleAccount with nothing configured", err)
	}
}
