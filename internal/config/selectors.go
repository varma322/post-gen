package config

import (
	"encoding/json"
	"os"
)

// PlatformSelectors represents the CSS selectors for a single platform
type PlatformSelectors struct {
	Title    string `json:"title"`
	Price    string `json:"price"`
	MRP      string `json:"mrp"`
	Features string `json:"features"`
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
