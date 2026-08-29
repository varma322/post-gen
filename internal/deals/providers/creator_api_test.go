package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"post-gen/internal/models"
	"post-gen/internal/scraper"
)

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// stubbedProvider wires a provider to a catalog endpoint that answers with body.
func stubbedProvider(t *testing.T, tag, body string) *CreatorAPI {
	t.Helper()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
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

	client := scraper.NewAmazonCreatorAPIScraper("id", "secret", oauthServer.URL, tag, nil)
	return NewCreatorAPI(client, "www.amazon.in")
}

const searchBody = `{"searchResult":{"totalResultCount":42,"items":[{
	"asin": "B0PROV00001",
	"itemInfo": {"title": {"displayValue": "Air Fryer"}},
	"offersV2": {"listings": [{"price": {
		"money": {"amount": 3499},
		"savingBasis": {"money": {"amount": 6999}},
		"savings": {"percentage": 50}
	}}]}
}]}}`

func TestProviderStampsCategoryAndProvider(t *testing.T) {
	// The API cannot supply either: the category is known only because the
	// search was scoped to that browse node, and the provider name is a
	// pipeline concept the catalog knows nothing about.
	provider := stubbedProvider(t, "provider-stamp-21", searchBody)

	candidates, err := provider.Discover(context.Background(), models.DealQuery{
		Category:     "Kitchen",
		BrowseNodeID: "976442031",
		MinSavingPct: 50,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("got %d candidates, want 1", len(candidates))
	}

	got := candidates[0]
	if got.Category != "Kitchen" {
		t.Errorf("Category = %q, want the query's label", got.Category)
	}
	if got.Provider != models.DealProviderCreatorAPI {
		t.Errorf("Provider = %q, want %q", got.Provider, models.DealProviderCreatorAPI)
	}
	if got.Price != 3499 || got.DiscountPct != 50 {
		t.Errorf("price fields did not survive: %+v", got)
	}
}

func TestProviderCandidatesConvertToValidDeals(t *testing.T) {
	provider := stubbedProvider(t, "provider-valid-21", searchBody)

	candidates, err := provider.Discover(context.Background(), models.DealQuery{
		Category:     "Kitchen",
		BrowseNodeID: "976442031",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	deal := candidates[0].Deal()
	if err := deal.Validate(); err != nil {
		t.Errorf("a discovered candidate should store without further work: %v", err)
	}
	if deal.Status != models.DealNew {
		t.Errorf("status = %q, want new", deal.Status)
	}
}

func TestProviderDefaultsThePage(t *testing.T) {
	provider := stubbedProvider(t, "provider-page-21", searchBody)

	// Page 0 is what a zero-valued query carries; it must become page 1 rather
	// than being sent as an invalid page.
	if _, err := provider.Discover(context.Background(), models.DealQuery{
		Category: "Kitchen", BrowseNodeID: "976442031", Page: 0,
	}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestProviderNameIdentifiesTheSource(t *testing.T) {
	if got := NewCreatorAPI(nil, "").Name(); got != models.DealProviderCreatorAPI {
		t.Errorf("Name = %q, want %q", got, models.DealProviderCreatorAPI)
	}
}

func TestProviderWithoutAClientFails(t *testing.T) {
	if _, err := NewCreatorAPI(nil, "").Discover(context.Background(), models.DealQuery{Keywords: "x"}); err == nil {
		t.Error("expected a provider with no client to report an error")
	}
}
