package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultOllamaHost    = "http://127.0.0.1:11434"
	defaultOllamaModel   = "qwen2.5:7b-instruct"
	defaultOllamaTimeout = 45 * time.Second

	// keepAlive holds the model in VRAM between calls. A batch fans out across
	// every active account in quick succession; without this each call pays
	// the load cost again, which dwarfs generation time.
	keepAlive = "10m"
)

// ollamaProvider talks to a local Ollama server.
type ollamaProvider struct {
	host   string
	model  string
	client *http.Client
}

// newOllamaProvider builds the local provider. It always returns a usable
// provider - reachability is not probed at construction, because Ollama may
// legitimately start after this process does, and a per-call connection
// refusal on loopback is cheap enough to treat as the availability check.
func newOllamaProvider() *ollamaProvider {
	host := strings.TrimRight(strings.TrimSpace(os.Getenv("OLLAMA_HOST")), "/")
	if host == "" {
		host = defaultOllamaHost
	}

	model := strings.TrimSpace(os.Getenv("OLLAMA_MODEL"))
	if model == "" {
		model = defaultOllamaModel
	}

	timeout := defaultOllamaTimeout
	if raw := strings.TrimSpace(os.Getenv("OLLAMA_TIMEOUT")); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil && parsed > 0 {
			timeout = parsed
		}
	}

	return &ollamaProvider{
		host:   host,
		model:  model,
		client: &http.Client{Timeout: timeout},
	}
}

func (o *ollamaProvider) Name() string { return "ollama" }

// Model reports the configured model, for event metadata.
func (o *ollamaProvider) Model() string { return o.model }

type ollamaChatRequest struct {
	Model     string              `json:"model"`
	Messages  []ollamaChatMessage `json:"messages"`
	Stream    bool                `json:"stream"`
	Format    map[string]any      `json:"format,omitempty"`
	Options   map[string]any      `json:"options,omitempty"`
	KeepAlive string              `json:"keep_alive,omitempty"`
}

type ollamaChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatResponse struct {
	Message ollamaChatMessage `json:"message"`
	Done    bool              `json:"done"`
	Error   string            `json:"error,omitempty"`
}

// Generate runs one non-streaming chat completion.
//
// The schema is passed as Ollama's `format`, which constrains decoding to
// valid JSON of that exact shape. That removes the whole class of failures
// where a model returns prose around the object or wraps it in a code fence,
// so no response-cleaning heuristics are needed here.
func (o *ollamaProvider) Generate(ctx context.Context, prompt string, schema map[string]any) (*EnrichedContent, error) {
	payload := ollamaChatRequest{
		Model:     o.model,
		Messages:  []ollamaChatMessage{{Role: "user", Content: prompt}},
		Stream:    false,
		Format:    schema,
		Options:   map[string]any{"temperature": 0.7},
		KeepAlive: keepAlive,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshaling ollama request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.host+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating ollama request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling ollama at %s: %w", o.host, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading ollama response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var chat ollamaChatResponse
	if err := json.Unmarshal(raw, &chat); err != nil {
		return nil, fmt.Errorf("decoding ollama response: %w", err)
	}
	if chat.Error != "" {
		return nil, fmt.Errorf("ollama error: %s", chat.Error)
	}

	content := strings.TrimSpace(chat.Message.Content)
	if content == "" {
		return nil, fmt.Errorf("ollama returned an empty message")
	}

	var enriched EnrichedContent
	if err := json.Unmarshal([]byte(content), &enriched); err != nil {
		return nil, fmt.Errorf("parsing ollama content as JSON: %w (content: %s)", err, truncate(content, 200))
	}

	return &enriched, nil
}

// truncate shortens text for error messages so a runaway response can't dump
// itself into the log.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
