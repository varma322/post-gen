package config

import (
	"encoding/json"
	"os"
)

// Selectors represents the CSS selectors used for scraping
type Selectors struct {
	Title    string `json:"title"`
	Price    string `json:"price"`
	MRP      string `json:"mrp"`
	Features string `json:"features"`
}

// LoadSelectors loads the CSS selectors from the given path
func LoadSelectors(path string) (*Selectors, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var selectors Selectors
	err = json.Unmarshal(data, &selectors)
	if err != nil {
		return nil, err
	}

	return &selectors, nil
}
