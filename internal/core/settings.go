package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"post-gen/internal/models"
)

// Setting keys, as stored in the settings table.
const (
	settingOllamaHost      = "ollama_host"
	settingOllamaModel     = "ollama_model"
	settingOllamaTimeout   = "ollama_timeout"
	settingGeminiModel     = "gemini_model"
	settingProviderOrder   = "provider_order"
	settingSystemPrompt    = "system_prompt"
	settingGraphAPIVersion = "graph_api_version"
	settingPartnerTag      = "partner_tag"
	settingWorkerCooldown  = "worker_cooldown_seconds"
	settingDebugLogging    = "debug_logging"
)

// settingEnv maps each setting to the environment variable it falls back to.
var settingEnv = map[string]string{
	settingOllamaHost:      "OLLAMA_HOST",
	settingOllamaModel:     "OLLAMA_MODEL",
	settingOllamaTimeout:   "OLLAMA_TIMEOUT",
	settingGeminiModel:     "GEMINI_MODEL",
	settingGraphAPIVersion: "FACEBOOK_API_VERSION",
	settingPartnerTag:      "AMAZON_CREATOR_PARTNER_TAG",
}

// settingDefault is the built-in used when neither the database nor the
// environment supplies a value. These mirror the constants in the packages
// that consume them.
var settingDefault = map[string]string{
	settingOllamaHost:      "http://127.0.0.1:11434",
	settingOllamaModel:     "qwen2.5:7b-instruct",
	settingOllamaTimeout:   "45s",
	settingGeminiModel:     "gemini-3.5-flash",
	settingGraphAPIVersion: "v23.0",
	settingPartnerTag:      "",
}

// Settings returns the effective configuration along with where each value
// came from, so the screen can show whether a field is a stored override or
// still inherited from the environment.
func (e *Engine) Settings(ctx context.Context) (*models.SettingsView, error) {
	stored := map[string]json.RawMessage{}
	if e.db != nil {
		loaded, err := e.db.LoadSettings(ctx)
		if err != nil {
			return nil, err
		}
		stored = loaded
	}

	sources := make(map[string]string, len(settingDefault))
	resolve := func(key string) string {
		if raw, ok := stored[key]; ok {
			var value string
			if err := json.Unmarshal(raw, &value); err == nil && value != "" {
				sources[key] = "database"
				return value
			}
		}
		if env := settingEnv[key]; env != "" {
			if value := strings.TrimSpace(os.Getenv(env)); value != "" {
				sources[key] = "environment"
				return value
			}
		}
		sources[key] = "default"
		return settingDefault[key]
	}

	view := &models.SettingsView{
		AI: models.AISettings{
			ProviderOrder: e.providerOrder(stored),
			OllamaHost:    resolve(settingOllamaHost),
			OllamaModel:   resolve(settingOllamaModel),
			OllamaTimeout: resolve(settingOllamaTimeout),
			GeminiModel:   resolve(settingGeminiModel),
			// Never returned, only its presence. The value stays server-side.
			GeminiKeySet: strings.TrimSpace(os.Getenv("GEMINI_API")) != "",
			SystemPrompt: stringSetting(stored, settingSystemPrompt, ""),
		},
		Meta: models.MetaSettings{
			GraphAPIVersion: resolve(settingGraphAPIVersion),
		},
		Amazon: models.AmazonSettings{
			PartnerTag:      resolve(settingPartnerTag),
			ClientIDSet:     anyEnvSet("Credential_ID", "AMAZON_CREATOR_CLIENT_ID"),
			ClientSecretSet: anyEnvSet("Secret", "AMAZON_CREATOR_CLIENT_SECRET"),
		},
		System: models.SystemSettings{
			WorkerCooldownSeconds: intSetting(stored, settingWorkerCooldown, int(defaultWorkerCooldownSeconds)),
			DebugLogging:          boolSetting(stored, settingDebugLogging, false),
		},
		Sources: sources,
	}

	return view, nil
}

// SaveSettings persists the supplied overrides. Fields left nil are untouched.
func (e *Engine) SaveSettings(ctx context.Context, update models.SettingsUpdate) error {
	if e.db == nil {
		return fmt.Errorf("database required to store settings")
	}

	values := make(map[string]json.RawMessage)
	put := func(key string, value any) error {
		encoded, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("encoding setting %q: %w", key, err)
		}
		values[key] = encoded
		return nil
	}

	assignments := []struct {
		key   string
		value any
		set   bool
	}{
		{settingOllamaHost, deref(update.OllamaHost), update.OllamaHost != nil},
		{settingOllamaModel, deref(update.OllamaModel), update.OllamaModel != nil},
		{settingOllamaTimeout, deref(update.OllamaTimeout), update.OllamaTimeout != nil},
		{settingGeminiModel, deref(update.GeminiModel), update.GeminiModel != nil},
		{settingSystemPrompt, deref(update.SystemPrompt), update.SystemPrompt != nil},
		{settingGraphAPIVersion, deref(update.GraphAPIVersion), update.GraphAPIVersion != nil},
		{settingPartnerTag, deref(update.PartnerTag), update.PartnerTag != nil},
	}

	for _, assignment := range assignments {
		if !assignment.set {
			continue
		}
		if err := put(assignment.key, assignment.value); err != nil {
			return err
		}
	}

	if update.ProviderOrder != nil {
		if err := put(settingProviderOrder, *update.ProviderOrder); err != nil {
			return err
		}
	}
	if update.WorkerCooldownSeconds != nil {
		if *update.WorkerCooldownSeconds < 0 {
			return fmt.Errorf("worker cooldown cannot be negative")
		}
		if err := put(settingWorkerCooldown, *update.WorkerCooldownSeconds); err != nil {
			return err
		}
	}
	if update.DebugLogging != nil {
		if err := put(settingDebugLogging, *update.DebugLogging); err != nil {
			return err
		}
	}

	return e.db.SaveSettings(ctx, values)
}

// providerOrder reports the enrichment chain as configured.
func (e *Engine) providerOrder(stored map[string]json.RawMessage) []string {
	if raw, ok := stored[settingProviderOrder]; ok {
		var order []string
		if err := json.Unmarshal(raw, &order); err == nil && len(order) > 0 {
			return order
		}
	}

	order := []string{"ollama"}
	if strings.TrimSpace(os.Getenv("GEMINI_API")) != "" {
		order = append(order, "gemini")
	}
	return order
}

func stringSetting(stored map[string]json.RawMessage, key, fallback string) string {
	if raw, ok := stored[key]; ok {
		var value string
		if err := json.Unmarshal(raw, &value); err == nil {
			return value
		}
	}
	return fallback
}

func intSetting(stored map[string]json.RawMessage, key string, fallback int) int {
	if raw, ok := stored[key]; ok {
		var value int
		if err := json.Unmarshal(raw, &value); err == nil {
			return value
		}
	}
	return fallback
}

func boolSetting(stored map[string]json.RawMessage, key string, fallback bool) bool {
	if raw, ok := stored[key]; ok {
		var value bool
		if err := json.Unmarshal(raw, &value); err == nil {
			return value
		}
	}
	return fallback
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func anyEnvSet(names ...string) bool {
	for _, name := range names {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			return true
		}
	}
	return false
}
