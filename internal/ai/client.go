// Package ai provides product content enrichment for PostGen.
//
// Enrichment turns raw scraped product data into polished copy that is then
// fed into each account's Go template. The template structure is never
// altered - AI only writes the data fields templates reference (Title,
// Features, Tagline, Hashtags, Headline, Description). Prices, the discount,
// and the affiliate link are never sent through a model.
//
// Providers are tried in order: a local Ollama model first, then hosted Gemini
// as a fallback. If every provider fails, the original scraped product is
// returned unmodified and the pipeline continues.
package ai

import (
	"context"
	"log"
	"time"

	"post-gen/internal/events"
	"post-gen/internal/generator"
	"post-gen/internal/models"
)

// Enricher rewrites product content fields through the first provider that
// succeeds.
type Enricher struct {
	providers []Provider
	events    *events.Logger
}

// New builds an Enricher from the environment, returning nil when no provider
// is configured at all. A nil Enricher is a valid no-op: Enrich returns the
// product unchanged, so callers need no branch.
//
// eventLog may be nil; the logger tolerates a nil receiver.
func New(eventLog *events.Logger) *Enricher {
	var providers []Provider

	// Local first: no per-call cost, no rate limit, and no product data
	// leaving the machine.
	providers = append(providers, newOllamaProvider())

	if gemini := newGeminiProvider(); gemini != nil {
		providers = append(providers, gemini)
	}

	if len(providers) == 0 {
		return nil
	}

	names := make([]string, 0, len(providers))
	for _, p := range providers {
		names = append(names, p.Name())
	}
	log.Printf("[INFO] AI enrichment providers, in order: %v", names)

	return &Enricher{providers: providers, events: eventLog}
}

// Enrich rewrites the product's text fields for one account.
//
// The account's template decides which fields are requested and how emoji
// should be used, so copy generated for smartbuy.tmpl arrives in a different
// visual register than copy for notyoffers.tmpl. If enrichment fails for every
// provider, the original product is returned unmodified.
func (e *Enricher) Enrich(ctx context.Context, product models.Product, account models.Account) models.Product {
	if e == nil {
		return product
	}

	// A template that can't be profiled (missing file, unreadable) still gets
	// enriched - the zero profile requests every field with no emoji guidance,
	// which is the old behaviour.
	profile, err := generator.Profile(account.TemplatePath)
	if err != nil {
		log.Printf("[WARN] Could not profile template %q for account %q: %v. Enriching without layout guidance.",
			account.TemplatePath, account.Name, err)
	}

	prompt := buildPrompt(product, account.AIPrompt, profile)
	schema := schemaFor(profile)

	traceID := traceFromContext(ctx)

	for _, provider := range e.providers {
		e.events.Emit(events.Event{
			Type:       events.AIGenerationStarted,
			Source:     provider.Name(),
			TraceID:    traceID,
			Account:    account.Name,
			ProductURL: product.Link,
			Message:    "Generating post copy",
			Metadata: map[string]any{
				"provider": provider.Name(),
				"model":    modelOf(provider),
			},
		})

		started := time.Now()
		enriched, genErr := provider.Generate(ctx, prompt, schema)
		if genErr != nil {
			log.Printf("[WARN] AI enrichment via %s failed for account %q: %v", provider.Name(), account.Name, genErr)
			e.events.Emit(events.Event{
				Type:       events.AIGenerationFailed,
				Source:     provider.Name(),
				TraceID:    traceID,
				Account:    account.Name,
				ProductURL: product.Link,
				Message:    genErr.Error(),
				Duration:   time.Since(started),
				Metadata: map[string]any{
					"provider": provider.Name(),
					"model":    modelOf(provider),
				},
			})
			continue
		}

		e.events.Emit(events.Event{
			Type:       events.AIGenerationSuccess,
			Source:     provider.Name(),
			TraceID:    traceID,
			Account:    account.Name,
			ProductURL: product.Link,
			Message:    "Post copy generated",
			Duration:   time.Since(started),
			Metadata: map[string]any{
				"provider":       provider.Name(),
				"model":          modelOf(provider),
				"fields":         profile.UsedFields,
				"emoji_palette":  profile.EmojiPalette,
				"feature_prefix": profile.FeaturePrefix,
			},
		})

		return enriched.applyTo(product, profile)
	}

	log.Printf("[WARN] All AI providers failed for account %q; falling back to raw scraped data.", account.Name)
	return product
}

// modelOf reports a provider's model when it exposes one, for event metadata.
func modelOf(p Provider) string {
	type modelReporter interface{ Model() string }
	if mr, ok := p.(modelReporter); ok {
		return mr.Model()
	}
	return ""
}

// traceContextKey carries the pipeline trace id into enrichment so AI events
// join the same chain as the scrape and publish around them.
type traceContextKey struct{}

// WithTrace returns a context carrying the pipeline trace id.
func WithTrace(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceContextKey{}, traceID)
}

// traceFromContext recovers the trace id, falling back to a fresh one so an
// un-traced call still produces correlatable events rather than a blank id.
func traceFromContext(ctx context.Context) string {
	if traceID, ok := ctx.Value(traceContextKey{}).(string); ok && traceID != "" {
		return traceID
	}
	return events.NewTraceID()
}
