package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"post-gen/internal/config"
	"post-gen/internal/generator"
	"post-gen/internal/models"
	"post-gen/internal/scraper"
	"regexp"
	"strings"
)

func main() {
	url := flag.String("url", "", "Product URL to scrape")
	filePath := flag.String("file", "", "Path to file containing product URLs for bulk processing")
	accountName := flag.String("account", "", "Affiliate account name")
	allAccounts := flag.Bool("all", false, "Generate for all accounts")
	splitMode := flag.Bool("split", false, "Save each product to a separate file (Split mode)")
	clearDir := flag.Bool("clear", false, "Clear output directory before starting")
	flag.Parse()

	if *url == "" && *filePath == "" {
		fmt.Println("Usage: postgen [--url <link> | --file <path>] [--account <name> | --all] [--split] [--clear]")
		return
	}

	if *accountName == "" && !*allAccounts {
		fmt.Println("Please specify --account or --all")
		return
	}

	// Load shared configurations
	accounts, err := config.LoadAccounts("accounts.json")
	if err != nil {
		log.Fatalf("[ERR] Loading accounts: %v", err)
	}

	sel, err := config.LoadSelectors("selectors.json")
	if err != nil {
		log.Fatalf("[ERR] Loading selectors: %v", err)
	}

	// Handle clearing output directory
	if *clearDir {
		if err := clearOutputDirectory(); err != nil {
			log.Fatalf("[ERR] Clearing output directory: %v", err)
		}
		log.Println("[INFO] Cleared output directory.")
	}

	// Ensure output directory exists
	if err := os.MkdirAll("output", os.ModePerm); err != nil {
		log.Fatalf("[ERR] Creating output directory: %v", err)
	}

	var urls []string
	if *url != "" {
		urls = append(urls, *url)
	}

	if *filePath != "" {
		fileUrls, err := readLines(*filePath)
		if err != nil {
			log.Fatalf("[ERR] Reading file: %v", err)
		}
		urls = append(urls, fileUrls...)
	}

	// Process all URLs
	total := len(urls)
	for i, u := range urls {
		progress := fmt.Sprintf("[%d/%d]", i+1, total)
		log.Printf("%s Processing URL: %s", progress, u)

		// 1. Get Scraper
		s, err := scraper.GetScraper(u, sel)
		if err != nil {
			log.Printf("%s [ERR] %v", progress, err)
			continue
		}

		// 2. Scrape
		product, err := s.Scrape(u)
		if err != nil {
			log.Printf("%s [ERR] %v", progress, err)
			continue
		}

		// 3. Post-scrape fields
		product.Tagline = "Don't miss out on this amazing deal!"
		product.Hashtags = "#AmazonDeals #Offers #MustHave"

		// 4. Generate & Output
		if *allAccounts {
			for _, acc := range accounts {
				generateForAccount(acc, *product, *splitMode)
			}
		} else {
			accountFound := false
			for _, acc := range accounts {
				if acc.Name == *accountName {
					generateForAccount(acc, *product, *splitMode)
					accountFound = true
					break
				}
			}
			if !accountFound {
				log.Printf("%s [ERR] Account '%s' not found", progress, *accountName)
			}
		}
	}

	log.Println("[INFO] Bulk processing complete.")
}

func generateForAccount(acc models.Account, product models.Product, split bool) {
	post, err := generator.GeneratePost(product, acc.TemplatePath)
	if err != nil {
		log.Printf("[ERR] Generating post for %s: %v", acc.Name, err)
		return
	}

	var outputPath string
	if split {
		// Split mode: {account}_{slug}.txt
		slug := slugify(product.Title, 20)
		outputPath = filepath.Join("output", fmt.Sprintf("%s_%s.txt", acc.Name, slug))
	} else {
		// Append mode: {account}.txt
		outputPath = filepath.Join("output", acc.Name+".txt")
	}

	// Output to console
	fmt.Printf("\n--- [%s] ---\n%s\n--- END ---\n", acc.Name, post)

	// Output to file
	if split {
		err = os.WriteFile(outputPath, []byte(post), 0644)
	} else {
		// Append mode with separator
		f, openErr := os.OpenFile(outputPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if openErr == nil {
			defer f.Close()
			_, err = f.WriteString(post + "\n\n-------------------\n\n")
		} else {
			err = openErr
		}
	}

	if err != nil {
		log.Printf("[ERR] Writing output file for %s: %v", acc.Name, err)
	} else {
		log.Printf("[INFO] Saved to: %s", outputPath)
	}
}

// Helpers

func readLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines, scanner.Err()
}

func clearOutputDirectory() error {
	if err := os.RemoveAll("output"); err != nil {
		return err
	}
	return os.MkdirAll("output", os.ModePerm)
}

func slugify(text string, limit int) string {
	// Lowercase and remove non-alphanumeric (except spaces)
	text = strings.ToLower(text)
	reg := regexp.MustCompile("[^a-z0-9 ]+")
	text = reg.ReplaceAllString(text, "")

	// Replace spaces with underscores
	text = strings.ReplaceAll(text, " ", "_")

	// Limit length
	if len(text) > limit {
		text = text[:limit]
	}

	return strings.Trim(text, "_")
}
