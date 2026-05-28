// Package bot implements the PostGen Telegram bot.
//
// The bot listens for product URLs sent by whitelisted users and guides them through
// an interactive workflow: scrape → AI enrich → preview per account → publish to Facebook.
//
// State machine per-user:
//   idle      → user sends URL → scraping...
//   preview   → bot shows post preview with inline keyboard
//   confirmed → bot publishes to Facebook
package bot

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"post-gen/internal/core"
)

// BotEngine is the minimal interface the bot needs from the core engine.
type BotEngine interface {
	GeneratePosts(urls []string, accountNames []string) ([]core.Result, error)
	GeneratePostsWithPublish(urls []string, accountNames []string, publish bool, delay time.Duration, onCooldown func(time.Duration)) ([]core.Result, error)
}

// Bot manages the Telegram bot lifecycle and user interactions.
type Bot struct {
	api          *tgbotapi.BotAPI
	engine       BotEngine
	allowedUsers map[int64]bool
	// userState tracks the last generated results per user, keyed by Telegram user ID.
	userState map[int64]*userSession
}

// userSession holds state between scrape and publish for a single user.
type userSession struct {
	url     string
	results []core.Result
	msgIDs  []int // message IDs of preview messages to clean up
}

// New creates and configures the PostGen Telegram bot.
// allowedIDs is a comma-separated list of Telegram user IDs permitted to use the bot.
func New(engine BotEngine) (*Bot, error) {
	token := strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))
	if token == "" {
		return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN environment variable is not set")
	}

	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("creating telegram bot: %w", err)
	}
	api.Debug = false
	log.Printf("[BOT] Authorized on account @%s", api.Self.UserName)

	allowed := parseAllowedUsers(os.Getenv("TELEGRAM_ALLOWED_USER_IDS"))

	return &Bot{
		api:          api,
		engine:       engine,
		allowedUsers: allowed,
		userState:    make(map[int64]*userSession),
	}, nil
}

// Run starts the long-polling update loop. Blocks until ctx is cancelled.
func (b *Bot) Run(ctx context.Context) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 30

	updates := b.api.GetUpdatesChan(u)

	log.Println("[BOT] Listening for updates...")
	for {
		select {
		case <-ctx.Done():
			log.Println("[BOT] Shutting down.")
			return
		case update, ok := <-updates:
			if !ok {
				return
			}
			go b.handleUpdate(update)
		}
	}
}

func (b *Bot) handleUpdate(update tgbotapi.Update) {
	// Handle callback queries (inline button presses)
	if update.CallbackQuery != nil {
		b.handleCallback(update.CallbackQuery)
		return
	}

	if update.Message == nil {
		return
	}

	userID := update.Message.From.ID
	chatID := update.Message.Chat.ID

	if !b.isAllowed(userID) {
		b.send(chatID, "⛔ You are not authorized to use this bot.")
		return
	}

	text := strings.TrimSpace(update.Message.Text)
	if text == "" {
		return
	}

	// Handle commands
	if strings.HasPrefix(text, "/") {
		b.handleCommand(chatID, userID, text)
		return
	}

	// Treat non-command messages as product URLs
	b.handleURL(chatID, userID, text)
}

func (b *Bot) handleCommand(chatID, userID int64, cmd string) {
	switch {
	case strings.HasPrefix(cmd, "/start"):
		b.send(chatID, "👋 *Welcome to PostGen Bot!*\n\nSend me an Amazon product URL and I'll generate affiliate posts for all your accounts with AI-powered copy. Then you can publish directly to Facebook! 🚀\n\nSupported commands:\n/status — Show bot status\n/cancel — Cancel current session", tgbotapi.ModeMarkdown)
	case strings.HasPrefix(cmd, "/status"):
		b.send(chatID, fmt.Sprintf("✅ Bot is online — @%s", b.api.Self.UserName))
	case strings.HasPrefix(cmd, "/cancel"):
		delete(b.userState, userID)
		b.send(chatID, "❌ Session cancelled.")
	default:
		b.send(chatID, "Unknown command. Send a product URL to get started!")
	}
}

func (b *Bot) handleURL(chatID, userID int64, rawURL string) {
	// Basic URL validation
	if !strings.HasPrefix(rawURL, "http") {
		b.send(chatID, "⚠️ Please send a valid Amazon product URL (starting with https://)")
		return
	}

	typing := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)
	_, _ = b.api.Send(typing)

	b.send(chatID, "🔍 *Scraping product details…* This may take a few seconds.", tgbotapi.ModeMarkdown)

	results, err := b.engine.GeneratePosts([]string{rawURL}, nil)
	if err != nil || len(results) == 0 {
		b.send(chatID, fmt.Sprintf("❌ Failed to process URL:\n`%v`", err), tgbotapi.ModeMarkdown)
		return
	}

	// Check for scraping errors
	if results[0].Error != "" && results[0].Account == "" {
		b.send(chatID, fmt.Sprintf("❌ Scraping failed: %s", results[0].Error))
		return
	}

	// Store session for callback handling
	b.userState[userID] = &userSession{
		url:     rawURL,
		results: results,
	}

	// Count successful results
	successCount := 0
	for _, r := range results {
		if r.Error == "" {
			successCount++
		}
	}

	b.send(chatID, fmt.Sprintf("✅ *Generated %d/%d posts!*\n\nChoose an action:", successCount, len(results)), tgbotapi.ModeMarkdown)

	// Send preview for each account result
	for _, result := range results {
		if result.Error != "" {
			b.send(chatID, fmt.Sprintf("⚠️ *[%s]* Failed: %s", result.Account, result.Error), tgbotapi.ModeMarkdown)
			continue
		}

		previewText := fmt.Sprintf("📝 *Preview — %s:*\n\n```\n%s\n```", result.Account, truncate(result.Output, 800))
		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(
					fmt.Sprintf("📢 Publish [%s]", result.Account),
					fmt.Sprintf("publish:%s", result.Account),
				),
			),
		)
		msg := tgbotapi.NewMessage(chatID, previewText)
		msg.ParseMode = tgbotapi.ModeMarkdown
		msg.ReplyMarkup = keyboard
		_, _ = b.api.Send(msg)
	}

	// Add a bulk publish button if multiple accounts
	if successCount > 1 {
		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("🚀 Publish ALL to Facebook", "publish:ALL"),
				tgbotapi.NewInlineKeyboardButtonData("❌ Cancel", "cancel"),
			),
		)
		msg := tgbotapi.NewMessage(chatID, "Or publish all accounts at once (15 min delay between posts):")
		msg.ReplyMarkup = keyboard
		_, _ = b.api.Send(msg)
	}
}

func (b *Bot) handleCallback(query *tgbotapi.CallbackQuery) {
	userID := query.From.ID
	chatID := query.Message.Chat.ID
	data := query.Data

	// Acknowledge the button press
	ack := tgbotapi.NewCallback(query.ID, "")
	_, _ = b.api.Request(ack)

	if data == "cancel" {
		delete(b.userState, userID)
		b.editMessage(chatID, query.Message.MessageID, "❌ Cancelled.")
		return
	}

	session, ok := b.userState[userID]
	if !ok {
		b.send(chatID, "⚠️ Session expired. Please send the URL again.")
		return
	}

	if strings.HasPrefix(data, "publish:") {
		accountTarget := strings.TrimPrefix(data, "publish:")
		b.publishForUser(chatID, userID, session, accountTarget, query.Message.MessageID)
	}
}

func (b *Bot) publishForUser(chatID, userID int64, session *userSession, accountTarget string, msgID int) {
	b.editMessage(chatID, msgID, "⏳ Publishing to Facebook…")

	var accountNames []string
	if accountTarget != "ALL" {
		accountNames = []string{accountTarget}
	}
	// nil = all accounts

	results, err := b.engine.GeneratePostsWithPublish(
		[]string{session.url},
		accountNames,
		true,
		15*time.Minute,
		func(d time.Duration) {
			b.send(chatID, fmt.Sprintf("⏳ Rate-limit cooldown: waiting %v before next post…", d))
		},
	)

	if err != nil {
		b.send(chatID, fmt.Sprintf("❌ Publish error: %v", err))
		return
	}

	var sb strings.Builder
	sb.WriteString("📊 *Publish Results:*\n\n")
	for _, r := range results {
		if r.PublishID != "" {
			sb.WriteString(fmt.Sprintf("✅ *[%s]* Published! Post ID: `%s`\n", r.Account, r.PublishID))
		} else if r.PublishError != "" {
			sb.WriteString(fmt.Sprintf("❌ *[%s]* Failed: %s\n", r.Account, r.PublishError))
		} else if r.Error != "" {
			sb.WriteString(fmt.Sprintf("⚠️ *[%s]* Error: %s\n", r.Account, r.Error))
		} else {
			sb.WriteString(fmt.Sprintf("ℹ️ *[%s]* No Facebook credentials configured — post generated but not published.\n", r.Account))
		}
	}

	// Clean up session
	delete(b.userState, userID)

	b.send(chatID, sb.String(), tgbotapi.ModeMarkdown)
}

// send is a convenience wrapper for sending text messages.
func (b *Bot) send(chatID int64, text string, parseMode ...string) {
	msg := tgbotapi.NewMessage(chatID, text)
	if len(parseMode) > 0 {
		msg.ParseMode = parseMode[0]
	}
	_, _ = b.api.Send(msg)
}

// editMessage updates an existing message text.
func (b *Bot) editMessage(chatID int64, msgID int, text string) {
	edit := tgbotapi.NewEditMessageText(chatID, msgID, text)
	_, _ = b.api.Send(edit)
}

// isAllowed returns true if the user ID is in the allowed list.
// If no IDs are configured, all users are allowed (development mode).
func (b *Bot) isAllowed(userID int64) bool {
	if len(b.allowedUsers) == 0 {
		return true
	}
	return b.allowedUsers[userID]
}

// parseAllowedUsers parses a comma-separated string of Telegram user IDs.
func parseAllowedUsers(raw string) map[int64]bool {
	result := make(map[int64]bool)
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if id, err := strconv.ParseInt(part, 10, 64); err == nil {
			result[id] = true
		}
	}
	return result
}

// truncate shortens a string to maxLen characters to keep Telegram messages readable.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "\n…[truncated]"
}
