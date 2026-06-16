package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"post-gen/internal/api"
	"post-gen/internal/core"
	"post-gen/internal/db"
	"post-gen/internal/utils"
	"strings"
	"time"

	"github.com/joho/godotenv"
)


func main() {
	// Load .env from working directory (non-fatal if absent)
	if err := godotenv.Load(); err != nil {
		log.Println("[INFO] No .env file found, using system environment variables.")
	}

	url := flag.String("url", "", "Product URL to scrape")
	filePath := flag.String("file", "", "Path to file containing product URLs for bulk processing")
	accountName := flag.String("account", "", "Affiliate account name")
	allAccounts := flag.Bool("all", false, "Generate for all accounts")
	splitMode := flag.Bool("split", false, "Save each product to a separate file (Split mode)")
	clearDir := flag.Bool("clear", false, "Clear output directory before starting")
	serveMode := flag.Bool("serve", false, "Start API server mode (compatibility)")
	addr := flag.String("addr", ":8080", "Address for HTTP API server")
	apiToken := flag.String("api-token", "", "Bearer token for API authentication (overrides POSTGEN_API_TOKEN env var)")
	publish := flag.Bool("publish", false, "Publish generated posts directly to Facebook Pages (configured in accounts.json)")
	publishDelay := flag.Duration("publish-delay", 15*time.Minute, "Delay spacing duration between consecutive Facebook publications to prevent spam blocks (e.g. 15m)")
	flag.Parse()

	// Resolve token: flag takes priority, then env variable
	token := strings.TrimSpace(*apiToken)
	if token == "" {
		token = strings.TrimSpace(os.Getenv("POSTGEN_API_TOKEN"))
	}

	// Connect to PostgreSQL (graceful fallback to JSON)
	ctx := context.Background()
	dbPool, err := db.New(ctx)
	if err != nil {
		log.Printf("[WARN] Could not connect to PostgreSQL (%v). Falling back to accounts.json.", err)
		dbPool = nil
	} else {
		log.Println("[INFO] Connected to PostgreSQL.")
	}

	paths := core.DefaultPaths()
	engine, err := core.NewEngine(paths, dbPool)
	if err != nil {
		log.Fatalf("[ERR] Bootstrapping engine: %v", err)
	}

	if *serveMode {
		if token == "" {
			log.Println("[WARN] API server started WITHOUT authentication. Set --api-token or POSTGEN_API_TOKEN env var to secure it.")
		} else {
			log.Println("[INFO] API authentication enabled (Bearer token).")
		}
		log.Printf("[INFO] Starting API server on %s", *addr)
		if err := http.ListenAndServe(*addr, api.NewServer(engine, token)); err != nil {
			log.Fatalf("[ERR] Starting API server: %v", err)
		}
		return
	}

	if *url == "" && *filePath == "" {
		fmt.Println("Usage: postgen [--url <link> | --file <path>] [--account <name> | --all] [--split] [--clear] [--publish] [--publish-delay <duration>]")
		fmt.Println("       postgen --serve [--addr :8080]")
		return
	}

	if *accountName == "" && !*allAccounts {
		fmt.Println("Please specify --account or --all")
		return
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

	results, err := engine.GeneratePostsWithPublish(ctx, urls, accountNames, *publish, *publishDelay, func(d time.Duration) {
		log.Printf("[INFO] Waiting %v before next publish to prevent rate limiting...", d)
	})
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

		if result.PublishID != "" {
			log.Printf("[INFO] [%s] [%s] Published to Facebook! Post ID: %s", result.URL, result.Account, result.PublishID)
		} else if result.PublishError != "" {
			log.Printf("[ERR] [%s] [%s] Facebook publish failed: %s", result.URL, result.Account, result.PublishError)
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
