package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"post-gen/internal/core"
	"post-gen/internal/utils"
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

	paths := core.DefaultPaths()
	engine, err := core.NewEngine(paths)
	if err != nil {
		log.Fatalf("[ERR] Bootstrapping engine: %v", err)
	}

	if *clearDir {
		if err := clearOutputDirectory(paths.OutputDir); err != nil {
			log.Fatalf("[ERR] Clearing output directory: %v", err)
		}
		log.Println("[INFO] Cleared output directory.")
	}

	if err := os.MkdirAll(paths.OutputDir, os.ModePerm); err != nil {
		log.Fatalf("[ERR] Creating output directory: %v", err)
	}

	urls, err := collectURLs(*url, *filePath)
	if err != nil {
		log.Fatalf("[ERR] Collecting URLs: %v", err)
	}

	accountNames := []string{}
	if !*allAccounts {
		accountNames = []string{*accountName}
	}

	results, err := engine.GeneratePosts(urls, accountNames)
	if err != nil {
		log.Fatalf("[ERR] Generating posts: %v", err)
	}

	logResults(results, len(urls), paths.OutputDir, *splitMode)
	log.Println("[INFO] Bulk processing complete.")
}

func collectURLs(singleURL string, filePath string) ([]string, error) {
	var urls []string
	if singleURL != "" {
		urls = append(urls, strings.TrimSpace(singleURL))
	}

	if filePath != "" {
		fileURLs, err := readLines(filePath)
		if err != nil {
			return nil, err
		}
		urls = append(urls, fileURLs...)
	}

	return urls, nil
}

func logResults(results []core.Result, totalURLs int, outputDir string, split bool) {
	processed := make(map[string]bool, totalURLs)
	for _, result := range results {
		if !processed[result.URL] {
			log.Printf("%s Processing URL: %s", nextProgress(processed, totalURLs), result.URL)
			processed[result.URL] = true
		}

		if result.Error != "" {
			if result.Account != "" {
				log.Printf("[ERR] [%s] [%s] %s", result.URL, result.Account, result.Error)
			} else {
				log.Printf("[ERR] [%s] %s", result.URL, result.Error)
			}
			continue
		}

		if err := writeResult(outputDir, result, split); err != nil {
			log.Printf("[ERR] Writing output file for %s: %v", result.Account, err)
		}
	}
}

func writeResult(outputDir string, result core.Result, split bool) error {
	var outputPath string
	if split {
		slug := utils.Slugify(result.ProductTitle, 20)
		outputPath = filepath.Join(outputDir, fmt.Sprintf("%s_%s.txt", result.Account, slug))
	} else {
		outputPath = filepath.Join(outputDir, result.Account+".txt")
	}

	fmt.Printf("\n--- [%s] ---\n%s\n--- END ---\n", result.Account, result.Output)

	var err error
	if split {
		err = os.WriteFile(outputPath, []byte(result.Output), 0644)
	} else {
		file, openErr := os.OpenFile(outputPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if openErr != nil {
			return openErr
		}
		defer file.Close()
		_, err = file.WriteString(result.Output + "\n\n-------------------\n\n")
	}

	if err == nil {
		log.Printf("[INFO] Saved to: %s", outputPath)
	}

	return err
}

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

func clearOutputDirectory(outputDir string) error {
	if err := os.RemoveAll(outputDir); err != nil {
		return err
	}

	return os.MkdirAll(outputDir, os.ModePerm)
}

func nextProgress(processed map[string]bool, total int) string {
	current := len(processed) + 1
	if total == 0 {
		total = 1
	}

	return fmt.Sprintf("[%d/%d]", current, total)
}
