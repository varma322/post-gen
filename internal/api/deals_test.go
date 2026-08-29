package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"post-gen/internal/core"
	"post-gen/internal/deals"
	"post-gen/internal/models"
)

// errDiscoveryBoom stands in for an unexpected failure mid-run.
var errDiscoveryBoom = errors.New("discovery failed: rate limited")

func sampleDeals() []models.Deal {
	return []models.Deal{
		{ASIN: "B0APITEST01", Title: "Headphones", URL: "https://www.amazon.in/dp/B0APITEST01",
			Category: "Electronics", Price: 999, OldPrice: 4999, DiscountPercent: 80,
			Score: 95, Provider: models.DealProviderCreatorAPI, Status: models.DealNew},
		{ASIN: "B0APITEST02", Title: "Kettle", URL: "https://www.amazon.in/dp/B0APITEST02",
			Category: "Kitchen", Price: 750, OldPrice: 1000, DiscountPercent: 25,
			Score: 30, Provider: models.DealProviderScraper, Status: models.DealIgnored},
	}
}

func do(t *testing.T, gen stubGenerator, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	handler := NewServer(gen, "")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, httptest.NewRequest(method, target, nil))
	return resp
}

func TestHandleDealsListsDeals(t *testing.T) {
	resp := do(t, stubGenerator{deals: sampleDeals()}, http.MethodGet, "/deals")

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.Code)
	}

	var payload struct {
		Deals []models.Deal `json:"deals"`
		Count int           `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if payload.Count != 2 || len(payload.Deals) != 2 {
		t.Errorf("got count=%d len=%d, want 2", payload.Count, len(payload.Deals))
	}
	if payload.Deals[0].ASIN != "B0APITEST01" {
		t.Errorf("first deal = %q", payload.Deals[0].ASIN)
	}
}

func TestHandleDealsRejectsAnUnknownStatusFilter(t *testing.T) {
	// A typo in a status would otherwise return an empty list, which reads as
	// "no deals" rather than "you asked for something that does not exist".
	resp := do(t, stubGenerator{deals: sampleDeals()}, http.MethodGet, "/deals?status=publishing")

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.Code)
	}
}

func TestHandleDealsValidatesNumericFilters(t *testing.T) {
	for _, target := range []string{"/deals?limit=0", "/deals?limit=abc", "/deals?min_score=-1"} {
		resp := do(t, stubGenerator{}, http.MethodGet, target)
		if resp.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", target, resp.Code)
		}
	}
}

func TestHandleDealsAcceptsValidFilters(t *testing.T) {
	targets := []string{
		"/deals?status=new",
		"/deals?category=Electronics",
		"/deals?provider=creator_api",
		"/deals?min_score=70&limit=10",
	}
	for _, target := range targets {
		resp := do(t, stubGenerator{deals: sampleDeals()}, http.MethodGet, target)
		if resp.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", target, resp.Code)
		}
	}
}

func TestHandleDealsRejectsNonGET(t *testing.T) {
	resp := do(t, stubGenerator{}, http.MethodDelete, "/deals")
	if resp.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.Code)
	}
}

func TestHandleDealsReportsUnavailableStorageAs503(t *testing.T) {
	// No database is a configuration state, not a server fault.
	resp := do(t, stubGenerator{dealsErr: core.ErrDealsUnavailable}, http.MethodGet, "/deals")

	if resp.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.Code)
	}
}

func TestHandleDealByASINReturnsOne(t *testing.T) {
	resp := do(t, stubGenerator{deals: sampleDeals()}, http.MethodGet, "/deals/B0APITEST01")

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.Code)
	}

	var deal models.Deal
	if err := json.NewDecoder(resp.Body).Decode(&deal); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if deal.ASIN != "B0APITEST01" || deal.Score != 95 {
		t.Errorf("got %+v", deal)
	}
}

func TestHandleDealByASINUppercasesTheASIN(t *testing.T) {
	// ASINs are canonically upper case, and a lower-case link pasted from a
	// browser should still resolve.
	resp := do(t, stubGenerator{deals: sampleDeals()}, http.MethodGet, "/deals/b0apitest01")

	if resp.Code != http.StatusOK {
		t.Errorf("status = %d, want the lower-case ASIN to resolve", resp.Code)
	}
}

func TestHandleDealByASINReturns404WhenAbsent(t *testing.T) {
	resp := do(t, stubGenerator{deals: sampleDeals()}, http.MethodGet, "/deals/B0MISSING01")

	if resp.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.Code)
	}
}

func TestHandleDealByASINRequiresAnASIN(t *testing.T) {
	resp := do(t, stubGenerator{}, http.MethodGet, "/deals/")
	if resp.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.Code)
	}
}

func TestHandleDealByASINRejectsUnknownActions(t *testing.T) {
	resp := do(t, stubGenerator{deals: sampleDeals()}, http.MethodPost, "/deals/B0APITEST01/detonate")
	if resp.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.Code)
	}
}

func TestHandleDealIgnore(t *testing.T) {
	resp := do(t, stubGenerator{deals: sampleDeals()}, http.MethodPost, "/deals/B0APITEST01/ignore")

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.Code)
	}

	var payload map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if payload["status"] != models.DealIgnored {
		t.Errorf("status = %q, want %q", payload["status"], models.DealIgnored)
	}
}

func TestHandleDealIgnoreRequiresPOST(t *testing.T) {
	resp := do(t, stubGenerator{deals: sampleDeals()}, http.MethodGet, "/deals/B0APITEST01/ignore")
	if resp.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.Code)
	}
}

func TestHandleDealIgnoreReturns404ForAnUnknownASIN(t *testing.T) {
	resp := do(t, stubGenerator{deals: sampleDeals()}, http.MethodPost, "/deals/B0MISSING01/ignore")
	if resp.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.Code)
	}
}

func TestHandleDealsDiscover(t *testing.T) {
	gen := stubGenerator{discoverResult: &deals.Result{
		Queries: 3, Candidates: 30, New: 12, Updated: 5,
		ByProvider: map[string]int{models.DealProviderCreatorAPI: 17},
	}}

	resp := do(t, gen, http.MethodPost, "/deals/discover")

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.Code)
	}

	var result deals.Result
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if result.New != 12 || result.Updated != 5 {
		t.Errorf("got %+v, want the run's counts", result)
	}
}

func TestHandleDealsDiscoverRequiresPOST(t *testing.T) {
	resp := do(t, stubGenerator{}, http.MethodGet, "/deals/discover")
	if resp.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.Code)
	}
}

func TestHandleDealsDiscoverReportsMissingCredentialsAs503(t *testing.T) {
	resp := do(t, stubGenerator{discoverErr: core.ErrDiscoveryUnavailable}, http.MethodPost, "/deals/discover")

	if resp.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 for missing credentials", resp.Code)
	}
}

func TestHandleDealsDiscoverReturnsPartialCountsOnFailure(t *testing.T) {
	// A failed run still learned something; discarding the counts would leave
	// the operator with an error and no idea how far it got.
	gen := stubGenerator{
		discoverResult: &deals.Result{Queries: 3, Failed: 3, Errors: []string{"rate limited"}},
		discoverErr:    errDiscoveryBoom,
	}

	resp := do(t, gen, http.MethodPost, "/deals/discover")

	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.Code)
	}

	var payload struct {
		Error  string        `json:"error"`
		Result *deals.Result `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if payload.Error == "" {
		t.Error("expected the failure reason")
	}
	if payload.Result == nil || payload.Result.Failed != 3 {
		t.Errorf("expected the partial result alongside the error, got %+v", payload.Result)
	}
}

func TestDealsEndpointsRequireAuth(t *testing.T) {
	handler := NewServer(stubGenerator{deals: sampleDeals()}, "secret-token")

	for _, target := range []string{"/deals", "/deals/B0APITEST01", "/deals/discover"} {
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, target, nil))
		if resp.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401 without a token", target, resp.Code)
		}
	}
}

func TestHandleDealQueue(t *testing.T) {
	resp := do(t, stubGenerator{deals: sampleDeals()}, http.MethodPost, "/deals/B0APITEST01/queue")

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.Code)
	}

	var deal models.Deal
	if err := json.NewDecoder(resp.Body).Decode(&deal); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if deal.Status != models.DealQueued {
		t.Errorf("status = %q, want %q", deal.Status, models.DealQueued)
	}
}

func TestHandleDealQueueRequiresPOST(t *testing.T) {
	resp := do(t, stubGenerator{deals: sampleDeals()}, http.MethodGet, "/deals/B0APITEST01/queue")
	if resp.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.Code)
	}
}

func TestHandleDealQueueReturns404ForAnUnknownASIN(t *testing.T) {
	resp := do(t, stubGenerator{deals: sampleDeals()}, http.MethodPost, "/deals/B0MISSING01/queue")
	if resp.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.Code)
	}
}

func TestHandleDealQueueReportsUnavailableStorageAs503(t *testing.T) {
	resp := do(t, stubGenerator{dealsErr: core.ErrDealsUnavailable}, http.MethodPost, "/deals/B0APITEST01/queue")
	if resp.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.Code)
	}
}

func TestHandleAnalyticsDeals(t *testing.T) {
	resp := do(t, stubGenerator{deals: sampleDeals()}, http.MethodGet, "/analytics/deals")

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.Code)
	}

	var analytics models.DealAnalytics
	if err := json.NewDecoder(resp.Body).Decode(&analytics); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if analytics.Total != 2 {
		t.Errorf("Total = %d, want 2", analytics.Total)
	}
	if analytics.ByProvider[models.DealProviderCreatorAPI] != 1 {
		t.Errorf("ByProvider = %v, want the API deal counted", analytics.ByProvider)
	}
	if analytics.ByStatus[models.DealNew] != 1 {
		t.Errorf("ByStatus = %v, want the new deal counted", analytics.ByStatus)
	}
}

func TestHandleAnalyticsDealsRejectsNonGET(t *testing.T) {
	resp := do(t, stubGenerator{}, http.MethodPost, "/analytics/deals")
	if resp.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.Code)
	}
}

func TestHandleAnalyticsDealsReportsUnavailableStorageAs503(t *testing.T) {
	resp := do(t, stubGenerator{dealsErr: core.ErrDealsUnavailable}, http.MethodGet, "/analytics/deals")
	if resp.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.Code)
	}
}

func TestHandleDealsRescore(t *testing.T) {
	resp := do(t, stubGenerator{deals: sampleDeals()}, http.MethodPost, "/deals/rescore")

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.Code)
	}

	var payload map[string]int
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if payload["changed"] != 2 {
		t.Errorf("changed = %d, want 2", payload["changed"])
	}
}

func TestHandleDealsRescoreRequiresPOST(t *testing.T) {
	resp := do(t, stubGenerator{}, http.MethodGet, "/deals/rescore")
	if resp.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.Code)
	}
}

func TestRescoreIsNotTreatedAsAnASIN(t *testing.T) {
	// "rescore" shares the /deals/ prefix with every ASIN route, so it has to
	// be matched before the path is read as an identifier.
	resp := do(t, stubGenerator{deals: sampleDeals()}, http.MethodPost, "/deals/rescore")
	if resp.Code == http.StatusNotFound {
		t.Error("rescore was parsed as an unknown ASIN")
	}
}
