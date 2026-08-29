// Package deals discovers, scores and stores Amazon deals.
//
// Discovery runs a matrix of queries against the Creators API, falling back to
// HTML listing scrapes when no API-eligible account is available. What it
// produces is stored deals; scoring and queueing happen downstream.
package deals

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"post-gen/internal/models"
	"post-gen/internal/scraper"
)

// Category pairs a scoring category with the browse node that scopes searches
// to it.
//
// The node is what actually narrows the search; Name is the label carried onto
// stored deals and used by scoring. Keeping them together is what lets category
// be known by construction rather than guessed from a product title.
type Category struct {
	Name         string
	BrowseNodeID string
	// Keywords optionally splits one category into several queries. A category
	// with no keywords is searched by browse node alone.
	Keywords []string
}

// DefaultSavingTiers are the discount floors each category is searched at.
//
// Running several floors rather than one cheap floor is deliberate: asking for
// 70% off surfaces the steepest deals even when they sit far down the relevance
// ordering that a 30% search would return.
var DefaultSavingTiers = []int{30, 50, 70}

// VerifiedCategories are the categories whose browse nodes have been confirmed
// against the live API.
//
// Only Electronics is listed. The others in the scoring table - Home, Kitchen,
// Fashion, Books - are deliberately absent rather than guessed: searchItems
// accepts any numeric node and silently returns an unfiltered search for one
// that does not exist, so an invented ID would quietly widen discovery instead
// of failing. Add them once resolved through GetBrowseNodes.
var VerifiedCategories = []Category{
	{Name: "Electronics", BrowseNodeID: "976419031"},
}

// NodeResolver resolves browse node IDs, so the matrix can refuse to run on a
// node that no longer exists. *scraper.AmazonCreatorAPIScraper satisfies it.
type NodeResolver interface {
	GetBrowseNodes(ctx context.Context, ids []string, marketplace string) ([]scraper.BrowseNode, error)
}

// BuildMatrix expands categories into one query per category, keyword and
// saving tier.
//
// Order matters: tiers descend so the steepest discounts are fetched first. A
// run cut short by a throttle then keeps the best of what it was going to find,
// rather than the first alphabetical slice.
func BuildMatrix(categories []Category, tiers []int) []models.DealQuery {
	if len(tiers) == 0 {
		tiers = DefaultSavingTiers
	}

	descending := append([]int(nil), tiers...)
	sort.Sort(sort.Reverse(sort.IntSlice(descending)))

	queries := make([]models.DealQuery, 0, len(categories)*len(descending))
	for _, category := range categories {
		keywords := category.Keywords
		if len(keywords) == 0 {
			keywords = []string{""}
		}

		for _, tier := range descending {
			for _, keyword := range keywords {
				queries = append(queries, models.DealQuery{
					Category:     category.Name,
					BrowseNodeID: category.BrowseNodeID,
					Keywords:     keyword,
					MinSavingPct: tier,
					Page:         1,
				})
			}
		}
	}

	return queries
}

// ValidateCategories checks every category is usable before a run starts.
//
// Format is checked locally - a non-numeric node is rejected by the API with a
// 400, so there is no reason to spend a call learning that. Existence needs the
// API, because a numeric node that does not exist is accepted and answered with
// an unfiltered search. A nil resolver checks format only.
func ValidateCategories(ctx context.Context, resolver NodeResolver, categories []Category, marketplace string) error {
	if len(categories) == 0 {
		return fmt.Errorf("no categories configured for discovery")
	}

	ids := make([]string, 0, len(categories))
	for _, category := range categories {
		if strings.TrimSpace(category.Name) == "" {
			return fmt.Errorf("category with node %q has no name", category.BrowseNodeID)
		}
		if err := validNodeID(category.BrowseNodeID); err != nil {
			return fmt.Errorf("category %s: %w", category.Name, err)
		}
		ids = append(ids, category.BrowseNodeID)
	}

	if resolver == nil {
		return nil
	}

	nodes, err := resolver.GetBrowseNodes(ctx, ids, marketplace)
	if err != nil {
		return fmt.Errorf("resolving browse nodes: %w", err)
	}

	resolved := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		resolved[node.ID] = true
	}

	for _, category := range categories {
		if !resolved[category.BrowseNodeID] {
			return fmt.Errorf("category %s: browse node %s did not resolve; "+
				"searchItems would silently return an unfiltered search for it",
				category.Name, category.BrowseNodeID)
		}
	}

	return nil
}

// validNodeID enforces the pattern the API itself enforces: [1-9][0-9]*.
func validNodeID(id string) error {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return fmt.Errorf("browse node id is required")
	}
	if strings.HasPrefix(trimmed, "0") {
		return fmt.Errorf("browse node id %q must not start with zero", trimmed)
	}
	if _, err := strconv.ParseUint(trimmed, 10, 64); err != nil {
		return fmt.Errorf("browse node id %q must be numeric", trimmed)
	}
	return nil
}
