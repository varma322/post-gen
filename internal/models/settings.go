package models

// SettingsView is the Settings screen's payload.
//
// Secrets are never returned. Each one is represented by a boolean saying
// whether a value is configured, so the UI can show a filled password field
// without the value ever leaving the server.
type SettingsView struct {
	AI      AISettings       `json:"ai"`
	Meta    MetaSettings     `json:"meta"`
	Amazon  AmazonSettings   `json:"amazon"`
	System  SystemSettings   `json:"system"`
	Sources map[string]string `json:"sources"` // setting name -> "database" | "environment" | "default"
}

// AISettings covers the enrichment providers.
type AISettings struct {
	ProviderOrder []string `json:"provider_order"`
	OllamaHost    string   `json:"ollama_host"`
	OllamaModel   string   `json:"ollama_model"`
	OllamaTimeout string   `json:"ollama_timeout"`
	GeminiModel   string   `json:"gemini_model"`
	GeminiKeySet  bool     `json:"gemini_key_set"`
	SystemPrompt  string   `json:"system_prompt"`
}

// MetaSettings covers the Facebook Graph integration.
type MetaSettings struct {
	GraphAPIVersion string `json:"graph_api_version"`
}

// AmazonSettings covers the Creators API integration.
type AmazonSettings struct {
	PartnerTag      string `json:"partner_tag"`
	ClientIDSet     bool   `json:"client_id_set"`
	ClientSecretSet bool   `json:"client_secret_set"`
}

// SystemSettings covers worker behaviour.
type SystemSettings struct {
	WorkerCooldownSeconds int  `json:"worker_cooldown_seconds"`
	DebugLogging          bool `json:"debug_logging"`
}

// SettingsUpdate is the write payload. Every field is a pointer so an omitted
// key means "leave unchanged" rather than "reset to zero" - the difference
// between editing one field and wiping the rest of the form.
type SettingsUpdate struct {
	OllamaHost            *string   `json:"ollama_host,omitempty"`
	OllamaModel           *string   `json:"ollama_model,omitempty"`
	OllamaTimeout         *string   `json:"ollama_timeout,omitempty"`
	GeminiModel           *string   `json:"gemini_model,omitempty"`
	ProviderOrder         *[]string `json:"provider_order,omitempty"`
	SystemPrompt          *string   `json:"system_prompt,omitempty"`
	GraphAPIVersion       *string   `json:"graph_api_version,omitempty"`
	PartnerTag            *string   `json:"partner_tag,omitempty"`
	WorkerCooldownSeconds *int      `json:"worker_cooldown_seconds,omitempty"`
	DebugLogging          *bool     `json:"debug_logging,omitempty"`
}
