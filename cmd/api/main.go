package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/joho/godotenv"

	"post-gen/internal/api"
	"post-gen/internal/core"
	"post-gen/internal/db"
)

func main() {
	// Load .env from working directory (non-fatal if absent)
	if err := godotenv.Load(); err != nil {
		log.Println("[INFO] No .env file found, using system environment variables.")
	}

	addr := flag.String("addr", ":8080", "Address for HTTP API server")
	apiToken := flag.String("api-token", "", "Bearer token for API authentication (overrides POSTGEN_API_TOKEN env var)")
	flag.Parse()

	// Resolve token: flag takes priority, then env variable
	token := strings.TrimSpace(*apiToken)
	if token == "" {
		token = strings.TrimSpace(os.Getenv("POSTGEN_API_TOKEN"))
	}

	// Connect to PostgreSQL
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

	if token == "" {
		log.Println("[WARN] API server started WITHOUT authentication. Set --api-token or POSTGEN_API_TOKEN env var to secure it.")
	} else {
		log.Println("[INFO] API authentication enabled (Bearer token).")
	}

	log.Printf("[INFO] Starting API server on %s", *addr)
	if err := http.ListenAndServe(*addr, api.NewServer(engine, token)); err != nil {
		log.Fatalf("[ERR] Starting API server: %v", err)
	}
}
