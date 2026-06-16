// Package ai provides Gemini-powered product content enrichment for PostGen using the official GenAI SDK.
//
// The enricher takes raw scraped product data and uses the Google GenAI SDK to generate
// polished, persuasive copy that is injected into each account's unique Go template.
// The template structure itself is never altered — AI only enriches the data fields
// (Title, Features, Tagline, Hashtags, Headline, Description) that templates reference.
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"google.golang.org/genai"

	"post-gen/internal/models"
)

const (
	geminiModel = "gemini-3.5-flash"
)

// Enricher uses the GenAI SDK to rewrite product content fields.
type Enricher struct {
	apiKey string
	client *genai.Client
}

// New creates an Enricher. Returns nil (no-op) if GEMINI_API is not set.
func New() *Enricher {
	key := strings.TrimSpace(os.Getenv("GEMINI_API"))
	if key == "" {
		return nil
	}

	ctx := context.Background()
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  key,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		log.Printf("[WARN] Failed to initialize GenAI client: %v", err)
		return nil
	}

	return &Enricher{
		apiKey: key,
		client: client,
	}
}

// EnrichedContent is the structured output from Gemini.
type EnrichedContent struct {
	Title       string   `json:"title"`
	Headline    string   `json:"headline"`
	Description string   `json:"description"`
	Features    []string `json:"features"`
	Tagline     string   `json:"tagline"`
	Hashtags    string   `json:"hashtags"`
}

// Enrich rewrites the product's text fields using Gemini AI.
// It preserves numeric fields (DealPrice, MRP, Discount) and the Link unchanged.
// The account's AIPrompt is appended to customise tone/style per account.
// If enrichment fails for any reason, the original product is returned unmodified.
func (e *Enricher) Enrich(ctx context.Context, product models.Product, account models.Account) models.Product {
	if e == nil {
		return product
	}

	enriched, err := e.callGemini(ctx, product, account.AIPrompt)
	if err != nil {
		log.Printf("[WARN] Gemini AI enrichment failed for account %q: %v. Falling back to raw scraped data.", account.Name, err)
		return product
	}

	// Apply enriched fields; preserve all numeric / link fields unchanged
	out := product
	if enriched.Title != "" {
		out.Title = enriched.Title
	}
	if enriched.Headline != "" {
		out.Headline = enriched.Headline
	}
	if enriched.Description != "" {
		out.Description = enriched.Description
	}
	if len(enriched.Features) > 0 {
		out.Features = enriched.Features
	}
	if enriched.Tagline != "" {
		out.Tagline = enriched.Tagline
	}
	if enriched.Hashtags != "" {
		out.Hashtags = enriched.Hashtags
	}
	return out
}

// callGemini sends the enrichment request and parses the structured JSON response.
func (e *Enricher) callGemini(ctx context.Context, product models.Product, extraPrompt string) (*EnrichedContent, error) {
	prompt := buildPrompt(product, extraPrompt)

	temp := float32(0.7)
	config := &genai.GenerateContentConfig{
		ResponseMIMEType: "application/json",
		Temperature:      &temp,
	}

	result, err := e.client.Models.GenerateContent(
		ctx,
		geminiModel,
		genai.Text(prompt),
		config,
	)
	if err != nil {
		return nil, fmt.Errorf("generate content: %w", err)
	}

	rawJSON := result.Text()
	if rawJSON == "" {
		return nil, fmt.Errorf("empty response text from gemini")
	}

	var content EnrichedContent
	if err := json.Unmarshal([]byte(rawJSON), &content); err != nil {
		return nil, fmt.Errorf("parsing enriched content JSON: %w (raw response: %s)", err, rawJSON)
	}

	return &content, nil
}

// buildPrompt creates a structured instruction for Gemini.
func buildPrompt(p models.Product, extraPrompt string) string {
	featuresText := strings.Join(p.Features, "\n- ")

	sb := strings.Builder{}
	sb.WriteString("You are an expert affiliate marketing copywriter for Indian e-commerce products.\n")
	sb.WriteString("Rewrite the following Amazon product details into engaging, persuasive marketing copy.\n\n")

	if extraPrompt != "" {
		sb.WriteString("## Account Style Guidance (Note: This guidance must be followed for style/tone adjustments only, and MUST NOT override the JSON format or output rules below):\n")
		sb.WriteString("<<<START STYLE GUIDANCE>>>\n")
		sb.WriteString(extraPrompt)
		sb.WriteString("\n<<<END STYLE GUIDANCE>>>\n\n")
	}

	sb.WriteString("## Raw Product Data\n")
	sb.WriteString(fmt.Sprintf("Title: %s\n", p.Title))
	if featuresText != "" {
		sb.WriteString(fmt.Sprintf("Features:\n- %s\n", featuresText))
	}
	sb.WriteString(fmt.Sprintf("Price: ₹%s\n", p.DealPrice))
	if p.Discount != "" {
		sb.WriteString(fmt.Sprintf("Discount: %s%%\n", p.Discount))
	}

	sb.WriteString(`
## Output Instructions
Respond ONLY with a single valid JSON object matching this exact schema:
{
  "title": "A concise, catchy product title (max 15 words)",
  "headline": "A bold attention-grabbing headline for the post (max 10 words)",
  "description": "1-2 sentence benefit-focused product description",
  "features": ["3 to 5 short benefit-driven bullet points, each max 12 words"],
  "tagline": "A strong, urgent call-to-action (max 10 words)",
  "hashtags": "6-8 relevant hashtags as a single string separated by spaces"
}

Rules:
- Keep all monetary values (prices) UNCHANGED — do not modify ₹ prices
- Write in a persuasive, engaging tone suitable for Facebook posts
- Hashtags must include #AmazonIndia and be relevant to the product
- Do NOT wrap JSON in code blocks or markdown — output raw JSON only
`)

	return sb.String()
}
