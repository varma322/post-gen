package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"strings"

	"post-gen/internal/api"
	"post-gen/internal/core"
)

func main() {
	addr := flag.String("addr", ":8080", "Address for HTTP API server")
	apiToken := flag.String("api-token", "", "Bearer token for API authentication (overrides POSTGEN_API_TOKEN env var)")
	flag.Parse()

	// Resolve token: flag takes priority, then env variable
	token := strings.TrimSpace(*apiToken)
	if token == "" {
		token = strings.TrimSpace(os.Getenv("POSTGEN_API_TOKEN"))
	}

	paths := core.DefaultPaths()
	engine, err := core.NewEngine(paths)
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
