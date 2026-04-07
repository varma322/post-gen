package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"post-gen/internal/config"
	"post-gen/internal/generator"
	"post-gen/internal/models"
	"post-gen/internal/scraper"
)

func main() {
	url := flag.String("url", "", "Product URL to scrape")
	accountName := flag.String("account", "", "Affiliate account name")
	allAccounts := flag.Bool("all", false, "Generate for all accounts")
	flag.Parse()

	if *url == "" {
		fmt.Println("Usage: postgen --url <link> [--account <name> | --all]")
		return
	}

	if *accountName == "" && !*allAccounts {
		fmt.Println("Please specify --account or --all")
		return
	}

	// Load accounts
	accounts, err := config.LoadAccounts("accounts.json")
	if err != nil {
		log.Fatalf("Error loading accounts: %v", err)
	}

	// Load selectors
	sel, err := config.LoadSelectors("selectors.json")
	if err != nil {
		log.Fatalf("Error loading selectors: %v", err)
	}

	// Get platform-specific scraper
	s, err := scraper.GetScraper(*url, sel)
	if err != nil {
		log.Fatalf("Error getting scraper: %v", err)
	}

	// Scrape Product Data
	fmt.Printf("Scraping product data from %s...\n", *url)
	scrapedProduct, err := s.Scrape(*url)
	if err != nil {
		log.Fatalf("Error scraping product: %v", err)
	}
	product := *scrapedProduct

	// Phase 4: Output integration
	// Let's set some default extra fields that scraper doesn't fetch
	product.Tagline = "Don't miss out on this amazing deal!"
	product.Hashtags = "#AmazonDeals #Offers #MustHave"

	// Output directory creation
	if err := os.MkdirAll("output", os.ModePerm); err != nil {
		log.Fatalf("Error creating output directory: %v", err)
	}

	if *allAccounts {
		for _, acc := range accounts {
			generateForAccount(acc, product)
		}
	} else {
		found := false
		for _, acc := range accounts {
			if acc.Name == *accountName {
				generateForAccount(acc, product)
				found = true
				break
			}
		}
		if !found {
			fmt.Printf("Account '%s' not found in accounts.json\n", *accountName)
		}
	}
}

func generateForAccount(acc models.Account, product models.Product) {
	fmt.Printf("\n--- [%s] ---\n", acc.Name)
	post, err := generator.GeneratePost(product, acc.TemplatePath)
	if err != nil {
		fmt.Printf("Error generating post for %s: %v\n", acc.Name, err)
		return
	}
	
	// Output to stdout
	fmt.Println(post)
	fmt.Printf("--- END [%s] ---\n\n", acc.Name)

	// Output to file
	outputPath := filepath.Join("output", acc.Name+".txt")
	err = os.WriteFile(outputPath, []byte(post), 0644)
	if err != nil {
		log.Printf("Error writing output file for %s: %v\n", acc.Name, err)
	}
}
