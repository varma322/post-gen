package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"post-gen/internal/core"
	"post-gen/internal/models"
)

// maxDealListLimit caps how many deals one listing can return, so a missing or
// absurd limit cannot pull the whole table into a response.
const maxDealListLimit = 500

// defaultDealListLimit applies when the caller does not ask for one.
const defaultDealListLimit = 100

// handleDeals serves the deal collection at /deals.
func (s server) handleDeals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}

	filter, err := dealFilterFromQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	deals, err := s.engine.Deals(r.Context(), filter)
	if err != nil {
		writeDealError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"deals": deals, "count": len(deals)})
}

// handleDealByASIN serves /deals/{asin} and the actions beneath it.
//
// /deals/discover is handled here too: it shares the prefix, and Go's older
// mux pattern matching gives this handler everything under /deals/.
func (s server) handleDealByASIN(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/deals/")
	if rest == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "deal asin is required"})
		return
	}

	if rest == "discover" {
		s.handleDealsDiscover(w, r)
		return
	}

	if rest == "rescore" {
		s.handleDealsRescore(w, r)
		return
	}

	asinPart, action, _ := strings.Cut(rest, "/")
	asin := strings.ToUpper(strings.TrimSpace(asinPart))
	if asin == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "deal asin is required"})
		return
	}

	switch action {
	case "":
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}

		deal, err := s.engine.Deal(r.Context(), asin)
		if err != nil {
			writeDealError(w, err)
			return
		}
		if deal == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "deal not found"})
			return
		}
		writeJSON(w, http.StatusOK, deal)

	case "ignore":
		s.setDealStatus(w, r, asin, models.DealIgnored)

	case "queue":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}

		deal, err := s.engine.QueueDeal(r.Context(), asin)
		if err != nil {
			writeDealError(w, err)
			return
		}
		if deal == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "deal not found"})
			return
		}
		writeJSON(w, http.StatusOK, deal)

	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown deal action " + action})
	}
}

// setDealStatus applies a status transition requested by one of the sub-routes.
func (s server) setDealStatus(w http.ResponseWriter, r *http.Request, asin, status string) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}

	found, err := s.engine.SetDealStatus(r.Context(), asin, status)
	if err != nil {
		writeDealError(w, err)
		return
	}
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "deal not found"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"asin": asin, "status": status})
}

// handleDealsDiscover triggers one discovery run at /deals/discover.
//
// The run is synchronous: the matrix is paced but small, and an operator
// pressing the button wants the counts back rather than a job id to poll.
func (s server) handleDealsDiscover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}

	result, err := s.engine.DiscoverDeals(r.Context())
	if err != nil {
		// A run that stored nothing still carries useful counts, so report the
		// partial result alongside the error rather than discarding it.
		payload := map[string]any{"error": err.Error()}
		if result != nil {
			payload["result"] = result
		}

		status := http.StatusInternalServerError
		if errors.Is(err, core.ErrDiscoveryUnavailable) || errors.Is(err, core.ErrDealsUnavailable) {
			status = http.StatusServiceUnavailable
		}
		writeJSON(w, status, payload)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// handleDealsRescore recomputes stored scores at /deals/rescore.
//
// It reads only what the deals already carry, so it costs no API calls and is
// the right response to a scoring change rather than waiting for each deal to
// be rediscovered under the new rules.
func (s server) handleDealsRescore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}

	changed, err := s.engine.RescoreDeals(r.Context())
	if err != nil {
		writeDealError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]int{"changed": changed})
}

// handleAnalyticsDeals serves the deal catalog summary at /analytics/deals.
func (s server) handleAnalyticsDeals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}

	analytics, err := s.engine.DealAnalytics(r.Context())
	if err != nil {
		writeDealError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, analytics)
}

// dealFilterFromQuery reads the listing filters off the query string.
func dealFilterFromQuery(r *http.Request) (models.DealFilter, error) {
	query := r.URL.Query()

	filter := models.DealFilter{
		Status:   strings.TrimSpace(query.Get("status")),
		Category: strings.TrimSpace(query.Get("category")),
		Provider: strings.TrimSpace(query.Get("provider")),
		Limit:    defaultDealListLimit,
	}

	if filter.Status != "" && !models.ValidDealStatus(filter.Status) {
		return filter, errors.New("unknown status " + filter.Status)
	}

	if raw := strings.TrimSpace(query.Get("min_score")); raw != "" {
		score, err := strconv.Atoi(raw)
		if err != nil || score < 0 {
			return filter, errors.New("min_score must be a non-negative integer")
		}
		filter.MinScore = score
	}

	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 {
			return filter, errors.New("limit must be a positive integer")
		}
		filter.Limit = limit
	}

	if filter.Limit > maxDealListLimit {
		filter.Limit = maxDealListLimit
	}

	return filter, nil
}

// writeDealError maps engine errors onto status codes. A missing database or
// missing credentials is a configuration state, not a server fault, so it
// reports 503 rather than 500.
func writeDealError(w http.ResponseWriter, err error) {
	if errors.Is(err, core.ErrDealsUnavailable) || errors.Is(err, core.ErrDiscoveryUnavailable) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
}
