// Package bot implements the PostGen Telegram bot.
//
// The bot listens for product URLs sent by whitelisted users and guides them through
// an interactive workflow: scrape → AI enrich → preview per account → publish to Facebook.
//
// State machine per-user:
//   idle                  → user selects options or views accounts
//   awaiting_url          → user is prompted to send URL
//   selecting_accounts    → bot shows inline account selection checklist
//   generating            → bot is scraping/AI generating for selected accounts
//   preview               → bot shows post preview with inline keyboard
//   confirmed             → bot publishes to Facebook
package bot

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"post-gen/internal/core"
	"post-gen/internal/models"
)

// BotEngine is the minimal interface the bot needs from the core engine.
type BotEngine interface {
	GeneratePosts(ctx context.Context, urls []string, accountNames []string) ([]core.Result, error)
	GeneratePostsWithPublish(ctx context.Context, urls []string, accountNames []string, publish bool, delay time.Duration, onCooldown func(time.Duration)) ([]core.Result, error)
	PublishPost(accountName, postText string) (string, error)
	Accounts() []models.Account
}

// Bot manages the Telegram bot lifecycle and user interactions.
type Bot struct {
	api          *tgbotapi.BotAPI
	engine       BotEngine
	allowedUsers map[int64]bool
	// userState tracks the last generated results per user, keyed by Telegram user ID.
	stateMu      sync.RWMutex
	userState    map[int64]*userSession
}

// userSession holds state between scrape and publish for a single user.
type userSession struct {
	state            string          // "idle", "awaiting_url", "selecting_accounts", "generating"
	url              string          // target URL for generation
	selectedAccounts map[string]bool // map of accountName -> selected (checked)
	results          []core.Result   // last generated results
	msgIDs           []int           // message IDs of preview messages to clean up
}

// New creates and configures the PostGen Telegram bot.
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

// getSession gets or creates a userSession for the given user ID.
func (b *Bot) getSession(userID int64) *userSession {
	b.stateMu.Lock()
	defer b.stateMu.Unlock()
	session, ok := b.userState[userID]
	if !ok {
		session = &userSession{
			state:            "idle",
			selectedAccounts: make(map[string]bool),
		}
		b.userState[userID] = session
	}
	return session
}

// getMainMenuKeyboard returns the persistent bottom reply menu keyboard.
func getMainMenuKeyboard() tgbotapi.ReplyKeyboardMarkup {
	kb := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🔑 My accounts"),
			tgbotapi.NewKeyboardButton("📝 Generate post"),
		),
	)
	kb.ResizeKeyboard = true
	return kb
}

// welcomeMessage formats the rich Flipkart-like greeting/how-it-works message.
func (b *Bot) welcomeMessage(accountsCount int) string {
	return fmt.Sprintf(`🚀 *PostGen — Amazon Affiliate Bot*

🎁 *Auto-generate & publish stunning Facebook affiliate posts!*

*How it works:*
1️⃣ 🔑 *My accounts* — View details of your registered affiliate channels.
2️⃣ 📝 *Generate post* — Start the step-by-step product generation.
3️⃣ 🔗 *Select Accounts & Send URL* — Choose target Facebook Pages, then paste any Amazon product link.
4️⃣ 🤖 *AI Enrichment* — Gemini AI will automatically optimize product headlines, features, and hashtags!
5️⃣ 📢 *Preview & Publish* — Review the draft for each account and publish instantly with a tap!

📦 *Track every page post directly in Telegram.*

⚡ *Engine Status:* Active & Ready
✅ *%d page account(s)* configured. Tap a button below to get started!`, accountsCount)
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

	// Route Reply Keyboard button clicks
	switch text {
	case "🔑 My accounts":
		b.handleMyAccounts(chatID, userID)
		return
	case "📝 Generate post":
		b.handleGeneratePostPrompt(chatID, userID)
		return
	}

	// Treat starting with http as product URLs directly
	if strings.HasPrefix(text, "http") {
		b.handleURL(chatID, userID, text)
		return
	}

	// Check if we are actively expecting a URL
	session := b.getSession(userID)
	if session.state == "awaiting_url" {
		b.send(chatID, "⚠️ Please send a valid Amazon product URL (starting with https://)")
		return
	}

	// Default fallback: show the welcome guide
	accountsCount := len(b.engine.Accounts())
	b.sendWithMenu(chatID, b.welcomeMessage(accountsCount), getMainMenuKeyboard(), tgbotapi.ModeMarkdown)
}

func (b *Bot) handleCommand(chatID, userID int64, cmd string) {
	switch {
	case strings.HasPrefix(cmd, "/start"):
		accountsCount := len(b.engine.Accounts())
		b.sendWithMenu(chatID, b.welcomeMessage(accountsCount), getMainMenuKeyboard(), tgbotapi.ModeMarkdown)
	case strings.HasPrefix(cmd, "/status"):
		b.send(chatID, fmt.Sprintf("✅ Bot is online — @%s", b.api.Self.UserName))
	case strings.HasPrefix(cmd, "/cancel"):
		b.stateMu.Lock()
		delete(b.userState, userID)
		b.stateMu.Unlock()
		b.send(chatID, "❌ Session cancelled.")
	default:
		b.send(chatID, "Unknown command. Use the menu buttons below to get started!")
	}
}

func (b *Bot) handleMyAccounts(chatID, userID int64) {
	accounts := b.engine.Accounts()
	if len(accounts) == 0 {
		b.sendWithMenu(chatID, "⚠️ *No accounts configured.* Please configure accounts in your `accounts.json` / PostgreSQL to get started.", getMainMenuKeyboard(), tgbotapi.ModeMarkdown)
		return
	}

	var sb strings.Builder
	sb.WriteString("🔑 *Registered Affiliate Accounts:*\n\n")
	for i, acc := range accounts {
		sb.WriteString(fmt.Sprintf("%d. *%s*\n", i+1, acc.Name))
		sb.WriteString(fmt.Sprintf("   🏷️ *Tag:* `%s`\n", acc.AffiliateTag))
		if acc.FacebookPageID != "" {
			sb.WriteString(fmt.Sprintf("   📢 *FB Page ID:* `%s`\n", acc.FacebookPageID))
		} else {
			sb.WriteString("   📢 *FB Page ID:* _Not Configured_\n")
		}
		if acc.UseAI {
			sb.WriteString("   🤖 *AI Enrichment:* Enabled ✅\n")
		} else {
			sb.WriteString("   🤖 *AI Enrichment:* Disabled ❌\n")
		}
		if acc.AIPrompt != "" {
			sb.WriteString(fmt.Sprintf("   📝 *Persona prompt:* _\"%s\"_\n", truncate(acc.AIPrompt, 50)))
		}
		sb.WriteString("\n")
	}

	b.sendWithMenu(chatID, sb.String(), getMainMenuKeyboard(), tgbotapi.ModeMarkdown)
}

func (b *Bot) handleGeneratePostPrompt(chatID, userID int64) {
	session := b.getSession(userID)
	session.state = "awaiting_url"
	b.sendWithMenu(chatID, "🔗 *Send Amazon product URL*\n\nPlease paste a valid Amazon product URL starting with `https://`:", getMainMenuKeyboard(), tgbotapi.ModeMarkdown)
}

func (b *Bot) handleURL(chatID, userID int64, rawURL string) {
	// Basic URL validation
	if !strings.HasPrefix(rawURL, "http") {
		b.send(chatID, "⚠️ Please send a valid Amazon product URL (starting with https://)")
		return
	}

	accounts := b.engine.Accounts()
	if len(accounts) == 0 {
		b.send(chatID, "❌ No accounts configured. Cannot generate posts.")
		return
	}

	session := b.getSession(userID)
	session.url = rawURL
	session.state = "selecting_accounts"

	// Initialize selected accounts to true (checked) by default
	session.selectedAccounts = make(map[string]bool)
	for _, acc := range accounts {
		session.selectedAccounts[acc.Name] = true
	}

	msgText := fmt.Sprintf("🔗 *Product URL Received:*\n`%s`\n\nChoose the target affiliate account(s) you want to generate posts for:", truncate(rawURL, 150))
	msg := tgbotapi.NewMessage(chatID, msgText)
	msg.ParseMode = tgbotapi.ModeMarkdown
	msg.ReplyMarkup = b.buildAccountSelectionKeyboard(session)
	_, _ = b.api.Send(msg)
}

func (b *Bot) buildAccountSelectionKeyboard(session *userSession) tgbotapi.InlineKeyboardMarkup {
	accounts := b.engine.Accounts()
	var rows [][]tgbotapi.InlineKeyboardButton

	// Create grid: 2 buttons per row
	var currentRow []tgbotapi.InlineKeyboardButton
	for _, acc := range accounts {
		selected := session.selectedAccounts[acc.Name]
		checkbox := "⬜"
		if selected {
			checkbox = "✅"
		}
		buttonText := fmt.Sprintf("%s %s", checkbox, acc.Name)
		btn := tgbotapi.NewInlineKeyboardButtonData(buttonText, fmt.Sprintf("toggle:%s", acc.Name))
		currentRow = append(currentRow, btn)

		if len(currentRow) == 2 {
			rows = append(rows, currentRow)
			currentRow = []tgbotapi.InlineKeyboardButton{}
		}
	}
	if len(currentRow) > 0 {
		rows = append(rows, currentRow)
	}

	// Toggle All button
	allSelected := true
	for _, acc := range accounts {
		if !session.selectedAccounts[acc.Name] {
			allSelected = false
			break
		}
	}
	toggleAllText := "✨ Select All"
	if allSelected {
		toggleAllText = "⬜ Deselect All"
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData(toggleAllText, "toggle_all"),
	))

	// Action row: Generate & Cancel
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🚀 Generate Posts", "generate_posts"),
		tgbotapi.NewInlineKeyboardButtonData("❌ Cancel", "cancel"),
	))

	return tgbotapi.InlineKeyboardMarkup{
		InlineKeyboard: rows,
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
		b.stateMu.Lock()
		delete(b.userState, userID)
		b.stateMu.Unlock()
		b.editMessage(chatID, query.Message.MessageID, "❌ Cancelled.")
		return
	}

	b.stateMu.RLock()
	session, ok := b.userState[userID]
	b.stateMu.RUnlock()
	if !ok {
		b.send(chatID, "⚠️ Session expired. Please start again.")
		return
	}

	if strings.HasPrefix(data, "toggle:") {
		accountName := strings.TrimPrefix(data, "toggle:")
		session.selectedAccounts[accountName] = !session.selectedAccounts[accountName]

		edit := tgbotapi.NewEditMessageReplyMarkup(chatID, query.Message.MessageID, b.buildAccountSelectionKeyboard(session))
		_, _ = b.api.Send(edit)
		return
	}

	if data == "toggle_all" {
		accounts := b.engine.Accounts()
		allSelected := true
		for _, acc := range accounts {
			if !session.selectedAccounts[acc.Name] {
				allSelected = false
				break
			}
		}

		for _, acc := range accounts {
			session.selectedAccounts[acc.Name] = !allSelected
		}

		edit := tgbotapi.NewEditMessageReplyMarkup(chatID, query.Message.MessageID, b.buildAccountSelectionKeyboard(session))
		_, _ = b.api.Send(edit)
		return
	}

	if data == "generate_posts" {
		var targetAccounts []string
		for name, selected := range session.selectedAccounts {
			if selected {
				targetAccounts = append(targetAccounts, name)
			}
		}

		if len(targetAccounts) == 0 {
			alert := tgbotapi.NewCallbackWithAlert(query.ID, "⚠️ Please select at least one account!")
			_, _ = b.api.Request(alert)
			return
		}

		session.state = "generating"
		b.editMessage(chatID, query.Message.MessageID, "⏳ *Generating posts...* Please wait while we scrape the product details and run AI enrichment.", tgbotapi.ModeMarkdown)

		go b.executeGeneration(chatID, userID, session, targetAccounts)
		return
	}

	if strings.HasPrefix(data, "publish:") {
		accountTarget := strings.TrimPrefix(data, "publish:")
		b.publishForUser(chatID, userID, session, accountTarget, query.Message.MessageID)
	}
}

func (b *Bot) executeGeneration(chatID, userID int64, session *userSession, targetAccounts []string) {
	typing := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)
	_, _ = b.api.Send(typing)

	results, err := b.engine.GeneratePosts(context.Background(), []string{session.url}, targetAccounts)
	if err != nil || len(results) == 0 {
		b.send(chatID, fmt.Sprintf("❌ Failed to process URL:\n`%v`", err), tgbotapi.ModeMarkdown)
		return
	}

	// Check for scraping errors
	if results[0].Error != "" && results[0].Account == "" {
		b.send(chatID, fmt.Sprintf("❌ Scraping failed: %s", results[0].Error))
		return
	}

	// Store session results for publishing callbacks
	session.results = results
	session.state = "idle"

	// Count successful results
	successCount := 0
	for _, r := range results {
		if r.Error == "" {
			successCount++
		}
	}

	b.send(chatID, fmt.Sprintf("✅ *Generated %d/%d posts!*", successCount, len(results)), tgbotapi.ModeMarkdown)

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

func (b *Bot) publishForUser(chatID, userID int64, session *userSession, accountTarget string, msgID int) {
	b.editMessage(chatID, msgID, "⏳ Publishing to Facebook…")

	var targetResults []core.Result
	for _, r := range session.results {
		if r.Error != "" {
			continue
		}
		if accountTarget == "ALL" || r.Account == accountTarget {
			targetResults = append(targetResults, r)
		}
	}

	if len(targetResults) == 0 {
		b.send(chatID, "⚠️ No valid generated posts found to publish.")
		return
	}

	var publishResults []core.Result
	publishAttempts := 0

	for _, r := range targetResults {
		if publishAttempts > 0 {
			delay := 15 * time.Minute
			b.send(chatID, fmt.Sprintf("⏳ Rate-limit cooldown: waiting %v before next post…", delay))
			time.Sleep(delay)
		}
		publishAttempts++

		pubID, err := b.engine.PublishPost(r.Account, r.Output)
		if err != nil {
			if strings.Contains(err.Error(), "credentials not configured") {
				r.PublishError = ""
			} else {
				r.PublishError = err.Error()
			}
		} else {
			r.PublishID = pubID
		}
		publishResults = append(publishResults, r)
	}

	var sb strings.Builder
	sb.WriteString("📊 *Publish Results:*\n\n")
	for _, r := range publishResults {
		if r.PublishID != "" {
			sb.WriteString(fmt.Sprintf("✅ *[%s]* Published! Post ID: `%s`\n", r.Account, r.PublishID))
		} else if r.PublishError != "" {
			sb.WriteString(fmt.Sprintf("❌ *[%s]* Failed: %s\n", r.Account, r.PublishError))
		} else {
			sb.WriteString(fmt.Sprintf("ℹ️ *[%s]* No Facebook credentials configured — post generated but not published.\n", r.Account))
		}
	}

	// Clean up session
	b.stateMu.Lock()
	delete(b.userState, userID)
	b.stateMu.Unlock()

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

// sendWithMenu is a convenience wrapper for sending text messages with custom markup.
func (b *Bot) sendWithMenu(chatID int64, text string, replyMarkup interface{}, parseMode ...string) {
	msg := tgbotapi.NewMessage(chatID, text)
	if len(parseMode) > 0 {
		msg.ParseMode = parseMode[0]
	}
	if replyMarkup != nil {
		msg.ReplyMarkup = replyMarkup
	}
	_, _ = b.api.Send(msg)
}

// editMessage updates an existing message text.
func (b *Bot) editMessage(chatID int64, msgID int, text string, parseMode ...string) {
	edit := tgbotapi.NewEditMessageText(chatID, msgID, text)
	if len(parseMode) > 0 {
		edit.ParseMode = parseMode[0]
	}
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
