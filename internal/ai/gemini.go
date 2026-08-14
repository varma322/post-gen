package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"google.golang.org/genai"
)

const defaultGeminiModel = "gemini-3.5-flash"

// geminiProvider calls Google's hosted Gemini API. It serves as the fallback
// behind the local model, so an Ollama outage degrades to a paid call rather
// than to raw scraped titles.
type geminiProvider struct {
	client *genai.Client
	model  string
}

// newGeminiProvider returns nil when GEMINI_API is unset or the SDK client
// cannot be built, in which case the provider is simply left out of the chain.
func newGeminiProvider() *geminiProvider {
	key := strings.TrimSpace(os.Getenv("GEMINI_API"))
	if key == "" {
		return nil
	}

	client, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		APIKey:  key,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		log.Printf("[WARN] Failed to initialize GenAI client: %v", err)
		return nil
	}

	model := strings.TrimSpace(os.Getenv("GEMINI_MODEL"))
	if model == "" {
		model = defaultGeminiModel
	}

	return &geminiProvider{client: client, model: model}
}

func (g *geminiProvider) Name() string { return "gemini" }

// Model reports the configured model, for event metadata.
func (g *geminiProvider) Model() string { return g.model }

// Generate runs one generation. The schema is not sent to the API - JSON MIME
// mode plus the key list already in the prompt is what this integration has
// always relied on, and the response is validated by unmarshalling regardless.
func (g *geminiProvider) Generate(ctx context.Context, prompt string, _ map[string]any) (*EnrichedContent, error) {
	temp := float32(0.7)
	config := &genai.GenerateContentConfig{
		ResponseMIMEType: "application/json",
		Temperature:      &temp,
	}

	result, err := g.client.Models.GenerateContent(ctx, g.model, genai.Text(prompt), config)
	if err != nil {
		return nil, fmt.Errorf("generate content: %w", err)
	}

	rawJSON := strings.TrimSpace(result.Text())
	if rawJSON == "" {
		return nil, fmt.Errorf("empty response text from gemini")
	}

	var content EnrichedContent
	if err := json.Unmarshal([]byte(rawJSON), &content); err != nil {
		return nil, fmt.Errorf("parsing enriched content JSON: %w (raw response: %s)", err, truncate(rawJSON, 200))
	}

	return &content, nil
}
