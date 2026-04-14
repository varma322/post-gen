package main

import (
	"flag"
	"log"
	"net/http"
	"post-gen/internal/api"
	"post-gen/internal/core"
)

func main() {
	addr := flag.String("addr", ":8080", "Address for HTTP API server")
	flag.Parse()

	paths := core.DefaultPaths()
	engine, err := core.NewEngine(paths)
	if err != nil {
		log.Fatalf("[ERR] Bootstrapping engine: %v", err)
	}

	log.Printf("[INFO] Starting API server on %s", *addr)
	if err := http.ListenAndServe(*addr, api.NewServer(engine)); err != nil {
		log.Fatalf("[ERR] Starting API server: %v", err)
	}
}
