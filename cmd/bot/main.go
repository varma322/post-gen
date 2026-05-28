package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	"post-gen/internal/bot"
	"post-gen/internal/core"
	"post-gen/internal/db"
)

func main() {
	// Load .env from working directory (non-fatal if absent)
	if err := godotenv.Load(); err != nil {
		log.Println("[INFO] No .env file found, using system environment variables.")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Connect to PostgreSQL
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

	postgenBot, err := bot.New(engine)
	if err != nil {
		log.Fatalf("[ERR] Starting Telegram bot: %v", err)
	}

	postgenBot.Run(ctx)
}
