package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestOllama points a provider at a stub server.
func newTestOllama(t *testing.T, handler http.HandlerFunc) (*ollamaProvider, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	provider := newOllamaProvider()
	provider.host = server.URL
	return provider, server
}

func TestOllamaSendsStructuredOutputRequest(t *testing.T) {
	var captured ollamaChatRequest

	provider, _ := newTestOllama(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("expected /api/chat, got %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decoding request: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"{\"title\":\"A camera\"}"},"done":true}`))
	})

	schema := map[string]any{"type": "object"}
	if _, err := provider.Generate(context.Background(), "write copy", schema); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if captured.Stream {
		t.Error("stream must be false; the caller reads a single response")
	}
	if captured.Format == nil {
		t.Error("format (the JSON schema) must be sent - it is what constrains decoding")
	}
	if captured.KeepAlive == "" {
		t.Error("keep_alive must be set so the model stays resident across a batch")
	}
	if len(captured.Messages) != 1 || captured.Messages[0].Content != "write copy" {
		t.Errorf("unexpected messages: %+v", captured.Messages)
	}
	if captured.Model == "" {
		t.Error("model must be set")
	}
}

func TestOllamaParsesContent(t *testing.T) {
	provider, _ := newTestOllama(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"{\"title\":\"Instax mini 13\",\"features\":[\"Auto exposure\",\"Self timer\"],\"hashtags\":\"#AmazonIndia\"}"},"done":true}`))
	})

	got, err := provider.Generate(context.Background(), "prompt", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Title != "Instax mini 13" {
		t.Errorf("Title = %q", got.Title)
	}
	if len(got.Features) != 2 {
		t.Errorf("Features = %v, want 2", got.Features)
	}
	if got.Hashtags != "#AmazonIndia" {
		t.Errorf("Hashtags = %q", got.Hashtags)
	}
}

func TestOllamaSurfacesHTTPFailure(t *testing.T) {
	provider, _ := newTestOllama(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`model "missing" not found`))
	})

	_, err := provider.Generate(context.Background(), "prompt", nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	// The message has to name the cause; this is what lands in the
	// AI_GENERATION_FAILED event and tells you to run `ollama pull`.
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should carry the server's reason, got: %v", err)
	}
}

func TestOllamaSurfacesBodyError(t *testing.T) {
	provider, _ := newTestOllama(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"error":"model requires more system memory"}`))
	})

	_, err := provider.Generate(context.Background(), "prompt", nil)
	if err == nil || !strings.Contains(err.Error(), "more system memory") {
		t.Fatalf("expected the body error surfaced, got: %v", err)
	}
}

func TestOllamaRejectsNonJSONContent(t *testing.T) {
	provider, _ := newTestOllama(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"Sure! Here is your copy:"},"done":true}`))
	})

	_, err := provider.Generate(context.Background(), "prompt", nil)
	if err == nil {
		t.Fatal("expected an error for non-JSON content")
	}
}

func TestOllamaRejectsEmptyContent(t *testing.T) {
	provider, _ := newTestOllama(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"   "},"done":true}`))
	})

	if _, err := provider.Generate(context.Background(), "prompt", nil); err == nil {
		t.Fatal("expected an error for empty content")
	}
}

func TestOllamaUnreachableHostReturnsError(t *testing.T) {
	// The fallback to Gemini depends on this returning an error rather than
	// hanging or panicking when Ollama isn't running.
	provider := newOllamaProvider()
	provider.host = "http://127.0.0.1:1" // reserved, nothing listens

	_, err := provider.Generate(context.Background(), "prompt", nil)
	if err == nil {
		t.Fatal("expected a connection error")
	}
	if !strings.Contains(err.Error(), "calling ollama") {
		t.Errorf("error should identify the provider, got: %v", err)
	}
}

func TestOllamaDefaultsFromEnvironment(t *testing.T) {
	t.Setenv("OLLAMA_HOST", "http://example.invalid:9999/")
	t.Setenv("OLLAMA_MODEL", "custom-model")
	t.Setenv("OLLAMA_TIMEOUT", "90s")

	provider := newOllamaProvider()

	// The trailing slash must be trimmed or requests hit a double-slash path.
	if provider.host != "http://example.invalid:9999" {
		t.Errorf("host = %q, want the trailing slash trimmed", provider.host)
	}
	if provider.Model() != "custom-model" {
		t.Errorf("model = %q", provider.Model())
	}
	if provider.client.Timeout.String() != "1m30s" {
		t.Errorf("timeout = %v, want 90s", provider.client.Timeout)
	}
}

func TestOllamaIgnoresUnparseableTimeout(t *testing.T) {
	t.Setenv("OLLAMA_TIMEOUT", "not-a-duration")

	provider := newOllamaProvider()
	if provider.client.Timeout != defaultOllamaTimeout {
		t.Errorf("timeout = %v, want the default when unparseable", provider.client.Timeout)
	}
}
