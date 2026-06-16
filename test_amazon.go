package main

import (
	"context"
	"fmt"
	"log"

	"post-gen/internal/config"
	"post-gen/internal/scraper"

	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(".env"); err != nil {
		log.Println("Note: .env file not loaded or not found, continuing with env variables")
	}

	url := "https://www.amazon.in/Philips-Technology-Protection-adjustable-QP2724/dp/B0CRF6NJC3"

	// Load selectors properly from selectors.json
	selectors, err := config.LoadSelectors("selectors.json")
	if err != nil {
		log.Fatalf("Error loading selectors.json: %v", err)
	}

	s, err := scraper.GetScraper(url, selectors)
	if err != nil {
		log.Fatalf("GetScraper error: %v", err)
	}

	fmt.Printf("Using scraper type: %T\n", s)

	product, err := s.Scrape(context.Background(), url)
	if err != nil {
		log.Fatalf("Scrape error: %v", err)
	}

	fmt.Println("--- Scraped Product Data ---")
	fmt.Printf("Title: %s\n", product.Title)
	fmt.Printf("DealPrice: %s\n", product.DealPrice)
	fmt.Printf("MRP: %s\n", product.MRP)
	fmt.Printf("Discount: %s\n", product.Discount)
	if len(product.Features) > 0 {
		fmt.Printf("Features: %v\n", product.Features)
	}
}



