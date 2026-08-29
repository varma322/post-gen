package config

import (
	"encoding/json"
	"os"
	"strings"
)

// PlatformSelectors represents the CSS selectors for a single platform
type PlatformSelectors struct {
	Title    string `json:"title"`
	Price    string `json:"price"`
	MRP      string `json:"mrp"`
	Features string `json:"features"`
	Image    string `json:"image"`
}

// Selectors maps platform names to their respective PlatformSelectors
type Selectors map[string]PlatformSelectors

// LoadSelectors loads the multi-platform CSS selectors from the given path
func LoadSelectors(path string) (Selectors, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var selectors Selectors
	err = json.Unmarshal(data, &selectors)
	if err != nil {
		return nil, err
	}

	return selectors, nil
}

// ListingSelectors are the CSS selectors for one platform's listing pages -
// Best Sellers, Movers and Shakers - which have a different shape from a
// product page: many tiles on one document rather than one product.
//
// Price and MRP are read from the tile so a candidate that clearly misses the
// discount threshold can be dropped before it costs a full product fetch.
type ListingSelectors struct {
	// Item matches one product tile; every other selector is scoped to it.
	Item  string `json:"item"`
	ASIN  string `json:"asin"`
	Title string `json:"title"`
	Price string `json:"price"`
	MRP   string `json:"mrp"`
	Link  string `json:"link"`
}

// ListingSelectorSet maps a platform key to its listing selectors.
type ListingSelectorSet map[string]ListingSelectors

// ListingSelectorSuffix marks the keys in selectors.json that describe listing
// pages rather than product pages.
const ListingSelectorSuffix = "_listings"

// LoadListingSelectors reads the listing selectors from the same file as the
// product ones, keyed "<platform>_listings".
//
// It is a separate loader rather than a wider PlatformSelectors so the product
// path is untouched: those two selector sets describe different documents, and
// merging them would mean every product scrape carried fields it never uses.
func LoadListingSelectors(path string) (ListingSelectorSet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var raw map[string]ListingSelectors
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	selectors := make(ListingSelectorSet, 1)
	for key, value := range raw {
		if strings.HasSuffix(key, ListingSelectorSuffix) && value.Item != "" {
			selectors[strings.TrimSuffix(key, ListingSelectorSuffix)] = value
		}
	}

	return selectors, nil
}
