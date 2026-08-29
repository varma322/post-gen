// Package providers holds the discovery sources deals are found through.
//
// Each provider turns one query into candidates. The interface they satisfy is
// declared by the consumer in internal/deals, so this package imports only
// models and the API client - never internal/deals itself.
package providers

import (
	"context"
	"fmt"

	"post-gen/internal/models"
	"post-gen/internal/scraper"
)

// CreatorAPI discovers deals through the Creators API searchItems operation.
//
// It is the primary source. The Best Sellers listing scraper stands behind it
// and only runs when every API-eligible account is throttled.
type CreatorAPI struct {
	client      *scraper.AmazonCreatorAPIScraper
	marketplace string
}

// NewCreatorAPI wraps an existing Creators API client as a discovery provider.
// An empty marketplace defaults to the Indian storefront.
func NewCreatorAPI(client *scraper.AmazonCreatorAPIScraper, marketplace string) *CreatorAPI {
	if marketplace == "" {
		marketplace = "www.amazon.in"
	}
	return &CreatorAPI{client: client, marketplace: marketplace}
}

// Name identifies this provider in stored deals and analytics.
func (p *CreatorAPI) Name() string { return models.DealProviderCreatorAPI }

// Discover runs one cell of the query matrix.
//
// The candidates come back carrying the query's category label and this
// provider's name, which the API cannot supply: category is known only because
// the search was scoped to that browse node in the first place.
func (p *CreatorAPI) Discover(ctx context.Context, query models.DealQuery) ([]models.DealCandidate, error) {
	if p.client == nil {
		return nil, fmt.Errorf("creator api provider has no client")
	}

	page := query.Page
	if page < 1 {
		page = 1
	}

	candidates, err := p.client.SearchItems(ctx, scraper.SearchOptions{
		Keywords:     query.Keywords,
		BrowseNodeID: query.BrowseNodeID,
		MinSavingPct: query.MinSavingPct,
		Page:         page,
		Marketplace:  p.marketplace,
	})
	if err != nil {
		return nil, fmt.Errorf("searching %s: %w", query.Category, err)
	}

	for i := range candidates {
		candidates[i].Category = query.Category
		candidates[i].Provider = models.DealProviderCreatorAPI
	}

	return candidates, nil
}
