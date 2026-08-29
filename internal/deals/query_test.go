package deals

import (
	"context"
	"errors"
	"strings"
	"testing"

	"post-gen/internal/scraper"
)

type fakeResolver struct {
	resolve map[string]string
	err     error
	asked   [][]string
}

func (f *fakeResolver) GetBrowseNodes(ctx context.Context, ids []string, marketplace string) ([]scraper.BrowseNode, error) {
	f.asked = append(f.asked, ids)
	if f.err != nil {
		return nil, f.err
	}

	nodes := make([]scraper.BrowseNode, 0, len(ids))
	for _, id := range ids {
		if name, ok := f.resolve[id]; ok {
			nodes = append(nodes, scraper.BrowseNode{ID: id, DisplayName: name})
		}
	}
	return nodes, nil
}

func TestBuildMatrixExpandsCategoriesAcrossTiers(t *testing.T) {
	categories := []Category{
		{Name: "Electronics", BrowseNodeID: "976419031"},
		{Name: "Kitchen", BrowseNodeID: "976442031"},
	}

	queries := BuildMatrix(categories, []int{30, 50, 70})

	if len(queries) != 6 {
		t.Fatalf("got %d queries, want 2 categories x 3 tiers", len(queries))
	}
	for _, query := range queries {
		if query.Page != 1 {
			t.Errorf("query %+v should start at page 1", query)
		}
		if query.BrowseNodeID == "" {
			t.Errorf("query %+v lost its browse node", query)
		}
	}
}

func TestBuildMatrixOrdersTiersSteepestFirst(t *testing.T) {
	// A run cut short by a throttle should keep the best of what it was going
	// to find, not the shallowest tier it happened to reach first.
	queries := BuildMatrix([]Category{{Name: "Electronics", BrowseNodeID: "1"}}, []int{30, 70, 50})

	want := []int{70, 50, 30}
	for i, query := range queries {
		if query.MinSavingPct != want[i] {
			t.Errorf("query %d has tier %d, want %d (descending)", i, query.MinSavingPct, want[i])
		}
	}
}

func TestBuildMatrixExpandsKeywords(t *testing.T) {
	categories := []Category{
		{Name: "Electronics", BrowseNodeID: "1", Keywords: []string{"headphones", "laptop"}},
	}

	queries := BuildMatrix(categories, []int{50})

	if len(queries) != 2 {
		t.Fatalf("got %d queries, want one per keyword", len(queries))
	}
	seen := map[string]bool{}
	for _, query := range queries {
		seen[query.Keywords] = true
	}
	if !seen["headphones"] || !seen["laptop"] {
		t.Errorf("keywords did not both make it into the matrix: %v", seen)
	}
}

func TestBuildMatrixSearchesByNodeAloneWithoutKeywords(t *testing.T) {
	queries := BuildMatrix([]Category{{Name: "Electronics", BrowseNodeID: "1"}}, []int{50})

	if len(queries) != 1 {
		t.Fatalf("got %d queries, want 1", len(queries))
	}
	if queries[0].Keywords != "" {
		t.Errorf("keywords = %q, want empty so the node alone scopes the search", queries[0].Keywords)
	}
}

func TestBuildMatrixFallsBackToDefaultTiers(t *testing.T) {
	queries := BuildMatrix([]Category{{Name: "Electronics", BrowseNodeID: "1"}}, nil)

	if len(queries) != len(DefaultSavingTiers) {
		t.Errorf("got %d queries, want one per default tier", len(queries))
	}
}

func TestValidateCategoriesRejectsMalformedNodes(t *testing.T) {
	cases := []struct {
		name       string
		categories []Category
	}{
		{"no categories", nil},
		{"blank name", []Category{{Name: " ", BrowseNodeID: "1"}}},
		{"missing node", []Category{{Name: "Electronics"}}},
		{"non-numeric node", []Category{{Name: "Electronics", BrowseNodeID: "976419031x"}}},
		{"leading zero", []Category{{Name: "Electronics", BrowseNodeID: "0976419031"}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateCategories(context.Background(), nil, tc.categories, "www.amazon.in"); err == nil {
				t.Error("expected validation to fail")
			}
		})
	}
}

func TestValidateCategoriesChecksFormatWithoutAResolver(t *testing.T) {
	// Format is checked locally because the API rejects a non-numeric node with
	// a 400; there is no reason to spend a call learning that.
	err := ValidateCategories(context.Background(), nil,
		[]Category{{Name: "Electronics", BrowseNodeID: "976419031"}}, "www.amazon.in")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateCategoriesRejectsANodeThatDoesNotResolve(t *testing.T) {
	// This is the stale-node trap: searchItems accepts a numeric node that does
	// not exist and answers with an unfiltered search, so discovery would widen
	// silently. Validation has to turn that into a startup failure.
	resolver := &fakeResolver{resolve: map[string]string{"976419031": "Electronics"}}

	err := ValidateCategories(context.Background(), resolver, []Category{
		{Name: "Electronics", BrowseNodeID: "976419031"},
		{Name: "Ghost", BrowseNodeID: "999999999031"},
	}, "www.amazon.in")

	if err == nil {
		t.Fatal("expected an unresolvable node to fail validation")
	}
	if !strings.Contains(err.Error(), "Ghost") || !strings.Contains(err.Error(), "999999999031") {
		t.Errorf("error %q should name the category and node that failed", err)
	}
}

func TestValidateCategoriesPassesWhenEveryNodeResolves(t *testing.T) {
	resolver := &fakeResolver{resolve: map[string]string{
		"976419031": "Electronics",
		"976442031": "Kitchen",
	}}

	err := ValidateCategories(context.Background(), resolver, []Category{
		{Name: "Electronics", BrowseNodeID: "976419031"},
		{Name: "Kitchen", BrowseNodeID: "976442031"},
	}, "www.amazon.in")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(resolver.asked) != 1 {
		t.Errorf("resolver called %d times, want one batched call", len(resolver.asked))
	}
}

func TestValidateCategoriesSurfacesAResolverFailure(t *testing.T) {
	resolver := &fakeResolver{err: errors.New("throttled")}

	err := ValidateCategories(context.Background(), resolver,
		[]Category{{Name: "Electronics", BrowseNodeID: "976419031"}}, "www.amazon.in")
	if err == nil {
		t.Fatal("expected a resolver failure to surface")
	}
	if !strings.Contains(err.Error(), "throttled") {
		t.Errorf("error %q should carry the resolver's reason", err)
	}
}

func TestVerifiedCategoriesOnlyHoldsConfirmedNodes(t *testing.T) {
	// Guessing node IDs is the failure mode this list exists to avoid, so it
	// should stay short until more are resolved live.
	if len(VerifiedCategories) == 0 {
		t.Fatal("expected at least the confirmed Electronics node")
	}
	for _, category := range VerifiedCategories {
		if err := validNodeID(category.BrowseNodeID); err != nil {
			t.Errorf("category %s: %v", category.Name, err)
		}
	}
}
