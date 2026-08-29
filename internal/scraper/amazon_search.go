package scraper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"post-gen/internal/models"
)

// ErrNoEligibleAccount reports that every API-eligible account is throttled, so
// the caller should fall back rather than treat this as a failure.
var ErrNoEligibleAccount = errors.New("creators api: no eligible account available")

// SearchOptions describes one searchItems query.
//
// Field names match what the live API accepts. Empty values are omitted from
// the payload rather than sent as zero, because searchItems ignores unknown or
// empty filters silently and a zero minSavingPercent would read as "no floor"
// either way.
type SearchOptions struct {
	Keywords string
	// BrowseNodeID must be numeric. searchItems rejects a non-numeric value
	// with a 400, but accepts a numeric one that does not exist and returns an
	// unfiltered search - so callers must validate node IDs, not trust them.
	BrowseNodeID string
	MinSavingPct int
	// Page is 1-based. searchItems returns up to ten items per page and
	// sometimes fewer, so callers must not assume a full page.
	Page        int
	SortBy      string
	Marketplace string
}

// SearchItems runs one discovery query against the Creators API.
//
// It returns candidates with the API-derived fields filled: ASIN, title, URL,
// image and numeric prices. Category and Provider are left to the caller, which
// knows which matrix cell produced the results.
//
// Throttling and ineligibility trip the same circuit breaker the product lookup
// path uses, keyed on the account the call ran under, so a discovery run that
// exhausts one account's quota moves the next one onto the other account rather
// than hammering the same one.
func (s *AmazonCreatorAPIScraper) SearchItems(ctx context.Context, opts SearchOptions) ([]models.DealCandidate, error) {
	marketplace := opts.Marketplace
	if marketplace == "" {
		marketplace = "www.amazon.in"
	}

	// searchItems needs at least one of keywords or a browse node; without
	// either it has nothing to search and the request is wasted quota. Checked
	// before any account is engaged, since no account would fare better.
	if strings.TrimSpace(opts.Keywords) == "" && strings.TrimSpace(opts.BrowseNodeID) == "" {
		return nil, errors.New("searchItems needs keywords or a browse node")
	}

	return withEligibleAccount(ctx, s, marketplace,
		func(tokenManager *TokenManager, partnerTag string) ([]models.DealCandidate, error) {
			return s.searchOnce(ctx, opts, marketplace, tokenManager, partnerTag)
		})
}

// searchOnce runs one searchItems call under a resolved account.
func (s *AmazonCreatorAPIScraper) searchOnce(
	ctx context.Context, opts SearchOptions, marketplace string,
	tokenManager *TokenManager, partnerTag string,
) ([]models.DealCandidate, error) {
	payload := map[string]any{
		"marketplace": marketplace,
		"partnerTag":  partnerTag,
		"resources": []string{
			"itemInfo.title",
			"images.primary.large",
			"offersV2.listings.price",
		},
	}

	if keywords := strings.TrimSpace(opts.Keywords); keywords != "" {
		payload["keywords"] = keywords
	}
	if node := strings.TrimSpace(opts.BrowseNodeID); node != "" {
		payload["browseNodeId"] = node
	}
	if opts.MinSavingPct > 0 {
		payload["minSavingPercent"] = opts.MinSavingPct
	}
	if opts.Page > 0 {
		payload["itemPage"] = opts.Page
	}
	if sortBy := strings.TrimSpace(opts.SortBy); sortBy != "" {
		payload["sortBy"] = sortBy
	}

	body, err := postCatalog(ctx, tokenManager, "searchItems", marketplace, payload)
	if err != nil {
		return nil, err
	}

	var resp apiSearchItemsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshaling searchItems response: %w", err)
	}

	if len(resp.Errors) > 0 {
		apiErr := fmt.Errorf("searchItems returned errors: %s - %s", resp.Errors[0].Code, resp.Errors[0].Message)
		if resp.Errors[0].Code == "AssociateNotEligible" {
			// Wrapped so the caller trips this account's circuit and moves on.
			return nil, fmt.Errorf("%w: %v", errCreatorsAPIIneligible, apiErr)
		}
		return nil, apiErr
	}
	// A ValidationException arrives as a bare message rather than in errors[],
	// which is how a malformed browse node reports itself.
	if resp.Message != "" {
		return nil, fmt.Errorf("searchItems rejected the query: %s: %s", resp.Type, resp.Message)
	}

	if resp.SearchResult == nil {
		return nil, nil
	}

	candidates := make([]models.DealCandidate, 0, len(resp.SearchResult.Items))
	for _, item := range resp.SearchResult.Items {
		candidate, ok := candidateFromItem(item, marketplace)
		if !ok {
			continue
		}
		candidates = append(candidates, candidate)
	}

	return candidates, nil
}

// candidateFromItem maps one search result, reporting false for a row too
// incomplete to be worth storing.
func candidateFromItem(item apiItem, marketplace string) (models.DealCandidate, bool) {
	asin := strings.ToUpper(strings.TrimSpace(item.ASIN))
	if asin == "" {
		return models.DealCandidate{}, false
	}

	candidate := models.DealCandidate{
		ASIN: asin,
		// Built from the ASIN rather than taken from item.detailPageUrl, which
		// carries the tag the API call ran under. The published link must carry
		// the tag of the page doing the publishing, applied downstream by
		// utils.AddAffiliateTag.
		URL: "https://" + marketplace + "/dp/" + asin,
	}

	if item.ItemInfo != nil && item.ItemInfo.Title != nil {
		candidate.Title = cleanText(item.ItemInfo.Title.DisplayValue)
	}
	if candidate.Title == "" {
		return models.DealCandidate{}, false
	}

	if item.Images != nil && item.Images.Primary != nil && item.Images.Primary.Large != nil {
		candidate.ImageURL = item.Images.Primary.Large.URL
	}

	if item.OffersV2 != nil && len(item.OffersV2.Listings) > 0 {
		if price := item.OffersV2.Listings[0].Price; price != nil {
			if price.Money != nil {
				candidate.Price = price.Money.Amount
			}
			if price.SavingBasis != nil && price.SavingBasis.Money != nil {
				candidate.OldPrice = price.SavingBasis.Money.Amount
			}
			if price.Savings != nil {
				candidate.DiscountPct = price.Savings.Percentage
			}
		}
	}

	// Amazon reports the percentage against savingBasis; derive it when only
	// the two prices came back, so scoring has something to work with.
	if candidate.DiscountPct == 0 && candidate.OldPrice > candidate.Price && candidate.OldPrice > 0 {
		candidate.DiscountPct = int((candidate.OldPrice - candidate.Price) / candidate.OldPrice * 100)
	}

	return candidate, true
}

type apiSearchItemsResponse struct {
	SearchResult *apiSearchResult `json:"searchResult,omitempty"`
	Errors       []apiError       `json:"errors,omitempty"`
	// A ValidationException is reported at the top level rather than in errors.
	Message string `json:"message,omitempty"`
	Type    string `json:"type,omitempty"`
}

type apiSearchResult struct {
	TotalResultCount int       `json:"totalResultCount"`
	Items            []apiItem `json:"items,omitempty"`
}
