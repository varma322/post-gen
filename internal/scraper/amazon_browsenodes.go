package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// BrowseNode is a resolved catalog category.
type BrowseNode struct {
	ID          string
	DisplayName string
	// AncestorNames runs from the immediate parent up to the category root.
	// The nodes attached to a product are mostly merchandising groupings
	// ("Clearance store", "Top150ASINhero"), so the real category is only
	// found by walking up.
	AncestorNames []string
}

// GetBrowseNodes resolves browse node IDs to their names and ancestry.
//
// This is what makes the query matrix safe. searchItems accepts a numeric node
// ID that does not exist and answers with an unfiltered keyword search rather
// than an error, so a stale ID would silently widen discovery. Resolving IDs up
// front turns that silent degradation into a startup failure.
//
// Note the plural key: getBrowseNodes takes browseNodeIds, unlike searchItems
// which takes the singular browseNodeId.
func (s *AmazonCreatorAPIScraper) GetBrowseNodes(ctx context.Context, ids []string, marketplace string) ([]BrowseNode, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if marketplace == "" {
		marketplace = "www.amazon.in"
	}

	body, err := withEligibleAccount(ctx, s, marketplace,
		func(tokenManager *TokenManager, partnerTag string) ([]byte, error) {
			return postCatalog(ctx, tokenManager, "getBrowseNodes", marketplace, map[string]any{
				"browseNodeIds": ids,
				"marketplace":   marketplace,
				"partnerTag":    partnerTag,
				"resources":     []string{"browseNodes.ancestor"},
			})
		})
	if err != nil {
		return nil, err
	}

	var resp apiBrowseNodesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshaling getBrowseNodes response: %w", err)
	}
	if len(resp.Errors) > 0 {
		return nil, fmt.Errorf("getBrowseNodes returned errors: %s - %s",
			resp.Errors[0].Code, resp.Errors[0].Message)
	}
	if resp.Message != "" {
		return nil, fmt.Errorf("getBrowseNodes rejected the request: %s: %s", resp.Type, resp.Message)
	}
	if resp.BrowseNodesResult == nil {
		return nil, nil
	}

	nodes := make([]BrowseNode, 0, len(resp.BrowseNodesResult.BrowseNodes))
	for _, node := range resp.BrowseNodesResult.BrowseNodes {
		resolved := BrowseNode{
			ID:          strings.TrimSpace(node.ID),
			DisplayName: strings.TrimSpace(node.DisplayName),
		}
		for ancestor := node.Ancestor; ancestor != nil; ancestor = ancestor.Ancestor {
			if name := strings.TrimSpace(ancestor.DisplayName); name != "" {
				resolved.AncestorNames = append(resolved.AncestorNames, name)
			}
		}
		nodes = append(nodes, resolved)
	}

	return nodes, nil
}

type apiBrowseNodesResponse struct {
	BrowseNodesResult *apiBrowseNodesResult `json:"browseNodesResult,omitempty"`
	Errors            []apiError            `json:"errors,omitempty"`
	Message           string                `json:"message,omitempty"`
	Type              string                `json:"type,omitempty"`
}

type apiBrowseNodesResult struct {
	BrowseNodes []apiBrowseNode `json:"browseNodes,omitempty"`
}

type apiBrowseNode struct {
	ID          string         `json:"id"`
	DisplayName string         `json:"displayName"`
	Ancestor    *apiBrowseNode `json:"ancestor,omitempty"`
}
