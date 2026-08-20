package ai

import (
	"context"
	"fmt"
	"strings"

	"post-gen/internal/generator"
	"post-gen/internal/models"
)

// Provider is one backend capable of turning scraped product data into post
// copy. Providers are tried in order by Enricher, so a local model can serve
// the common case while a hosted one covers its outages.
type Provider interface {
	// Name identifies the provider in logs and event metadata.
	Name() string
	// Generate returns enriched copy. schema describes the exact JSON shape
	// wanted; providers that support constrained decoding should enforce it.
	Generate(ctx context.Context, prompt string, schema map[string]any) (*EnrichedContent, error)
}

// EnrichedContent is the structured output every provider returns.
type EnrichedContent struct {
	Title       string   `json:"title"`
	Headline    string   `json:"headline"`
	Description string   `json:"description"`
	Features    []string `json:"features"`
	Tagline     string   `json:"tagline"`
	Hashtags    string   `json:"hashtags"`
}

// fieldSpec describes one generatable field: the Product field it populates,
// its JSON key, and the instruction that tells the model what to write.
type fieldSpec struct {
	productField string
	jsonKey      string
	description  string
}

// generatableFields are the only fields AI is allowed to write. Prices, the
// discount, and the affiliate link are deliberately absent - those are facts
// from the scrape, and a model must never be in a position to alter them.
var generatableFields = []fieldSpec{
	{"Title", "title", "A concise, catchy product title (max 15 words)"},
	{"Headline", "headline", "A bold attention-grabbing headline for the post (max 10 words)"},
	{"Description", "description", "A 1-2 sentence benefit-focused product description"},
	{"Features", "features", "3 to 5 short benefit-driven bullet points, each max 12 words"},
	{"Tagline", "tagline", "A strong, urgent call-to-action (max 10 words)"},
	{"Hashtags", "hashtags", "6-8 relevant hashtags as a single space-separated string"},
}

// schemaFor builds a JSON schema covering only the fields the template
// actually renders. Asking for the other four costs latency and tokens on
// output that gets discarded - dealsvault.tmpl renders Title alone.
func schemaFor(profile generator.TemplateProfile) map[string]any {
	properties := make(map[string]any)
	var required []string

	for _, field := range wantedFields(profile) {
		if field.jsonKey == "features" {
			properties[field.jsonKey] = map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			}
		} else {
			properties[field.jsonKey] = map[string]any{"type": "string"}
		}
		required = append(required, field.jsonKey)
	}

	return map[string]any{
		"type":       "object",
		"properties": properties,
		"required":   required,
	}
}

// wantedFields returns the generatable fields this template renders. A
// template referencing none of them (an unusual but valid layout) falls back
// to Title so there is always something to generate.
func wantedFields(profile generator.TemplateProfile) []fieldSpec {
	var wanted []fieldSpec
	for _, field := range generatableFields {
		if profile.Uses(field.productField) {
			wanted = append(wanted, field)
		}
	}
	if len(wanted) == 0 {
		wanted = append(wanted, generatableFields[0])
	}
	return wanted
}

// buildPrompt assembles the instruction sent to the provider.
//
// The account's style guidance is wrapped in explicit delimiters and framed as
// tone-only, so a prompt pasted into the per-account field cannot redirect the
// output contract. That boundary predates this refactor and is kept verbatim.
func buildPrompt(p models.Product, extraPrompt string, profile generator.TemplateProfile) string {
	sb := &strings.Builder{}

	sb.WriteString("You are an expert affiliate marketing copywriter for Indian e-commerce products.\n")
	sb.WriteString("Rewrite the following Amazon product details into engaging, persuasive marketing copy.\n\n")

	if extraPrompt != "" {
		sb.WriteString("## Account Style Guidance (Note: This guidance must be followed for style/tone adjustments only, and MUST NOT override the JSON format or output rules below):\n")
		sb.WriteString("<<<START STYLE GUIDANCE>>>\n")
		sb.WriteString(extraPrompt)
		sb.WriteString("\n<<<END STYLE GUIDANCE>>>\n\n")
	}

	sb.WriteString("## Raw Product Data\n")
	fmt.Fprintf(sb, "Title: %s\n", p.Title)
	if features := strings.Join(p.Features, "\n- "); features != "" {
		fmt.Fprintf(sb, "Features:\n- %s\n", features)
	}
	fmt.Fprintf(sb, "Price: ₹%s\n", p.DealPrice)
	if p.Discount != "" {
		fmt.Fprintf(sb, "Discount: %s%%\n", p.Discount)
	}

	writeStyleSection(sb, profile)

	sb.WriteString("\n## Output Instructions\n")
	sb.WriteString("Respond ONLY with a single valid JSON object with exactly these keys:\n")
	for _, field := range wantedFields(profile) {
		fmt.Fprintf(sb, "  %q: %s\n", field.jsonKey, field.description)
	}

	sb.WriteString("\nRules:\n")
	// Without this the 7B local model intermittently returns a whole post in
	// Chinese - observed end to end on zonerush.tmpl, which would have gone
	// straight to a Facebook page.
	sb.WriteString("- Write EVERY field in English. Do not use any other language or script\n")
	sb.WriteString("- Keep all monetary values (prices) UNCHANGED - do not modify ₹ prices\n")
	sb.WriteString("- Write in a persuasive, engaging tone suitable for Facebook posts\n")
	// Only mention hashtags when the layout renders them. Stating the rule
	// unconditionally leaks "#AmazonIndia" into whichever field the model can
	// reach on templates that have no hashtag slot.
	if profile.Uses("Hashtags") {
		sb.WriteString("- Hashtags must include #AmazonIndia and be relevant to the product\n")
	} else {
		sb.WriteString("- Do NOT include hashtags anywhere in your output\n")
	}
	if rendersPrices(profile) {
		sb.WriteString("- The layout prints the price, M.R.P. and discount on their own lines. " +
			"Do NOT repeat any price, rupee amount, or discount percentage inside the features, " +
			"description, headline, or tagline\n")
	}
	sb.WriteString("- Do NOT wrap JSON in code blocks or markdown - output raw JSON only\n")

	return sb.String()
}

// writeStyleSection tells the model how to handle emoji for this specific
// layout. This is what keeps generated copy visually consistent with the
// template it renders into, rather than merely emoji-laden.
func writeStyleSection(sb *strings.Builder, profile generator.TemplateProfile) {
	if len(profile.EmojiPalette) == 0 && !profile.FeaturesPrefixed {
		return
	}

	sb.WriteString("\n## Post Style\n")

	// When the layout prefixes features itself, its glyph is withheld from the
	// palette. Showing it invites the model to reuse it as a bullet, which is
	// precisely the collision the next instruction is trying to prevent.
	palette := profile.EmojiPalette
	if profile.FeaturesPrefixed {
		palette = withoutGlyph(palette, profile.FeaturePrefix)
	}

	if len(palette) > 0 {
		fmt.Fprintf(sb, "This post's layout already uses these emoji: %s\n", strings.Join(palette, " "))
		sb.WriteString("Match that visual style. Prefer emoji from that set, and keep the overall density similar.\n")
	}

	if profile.FeaturesPrefixed {
		fmt.Fprintf(sb, "IMPORTANT: the layout already prints %q before every feature bullet. "+
			"Every feature you write MUST begin with a letter - no emoji, no dash, no bullet character.\n",
			profile.FeaturePrefix)
	} else if profile.Uses("Features") {
		sb.WriteString("Feature bullets are rendered as-is with no prefix, so start each one with a fitting emoji.\n")
	}

	// Fields the layout already wraps in its own glyphs must arrive plain.
	if decorated := decoratedGeneratableFields(profile); len(decorated) > 0 {
		fmt.Fprintf(sb, "The layout already places emoji around these fields: %s. "+
			"Write them as plain text with NO emoji at the start or end.\n",
			strings.Join(decorated, ", "))
	}
}

// decoratedGeneratableFields lists the JSON keys whose layout position is
// already decorated, so the prompt can name them precisely.
func decoratedGeneratableFields(profile generator.TemplateProfile) []string {
	var keys []string
	for _, field := range wantedFields(profile) {
		if profile.Decorates(field.productField) {
			keys = append(keys, field.jsonKey)
		}
	}
	return keys
}

// rendersPrices reports whether the layout prints any price figure itself, in
// which case generated copy must not restate one.
func rendersPrices(profile generator.TemplateProfile) bool {
	return profile.Uses("DealPrice") || profile.Uses("MRP") || profile.Uses("Discount")
}

// normalizeFeatures trims each feature and, when the template prints its own
// prefix, strips any emoji or bullet the model added at either end anyway.
// Entries left empty are dropped rather than rendered as a bare prefix.
func normalizeFeatures(features []string, profile generator.TemplateProfile) []string {
	cleaned := make([]string, 0, len(features))
	pricesShown := rendersPrices(profile)

	for _, feature := range features {
		feature = strings.TrimSpace(feature)
		if !profile.FeaturesPrefixed {
			// The layout supplies no glyph, so the model's own is the only
			// one there - it just tends to omit the space after it.
			feature = generator.SpaceAfterLeadingEmoji(feature)
		} else {
			// Both ends, not just the leading one: told not to open a bullet
			// with an emoji, models reliably park it at the end instead,
			// which renders as "✔️ Finest quality sunflower 📦" against a
			// layout that already owns the decoration for that line.
			feature = generator.StripSurroundingDecoration(feature)
		}
		// A bullet that restates the price duplicates the layout's own price
		// block; there are several bullets, so dropping one costs less than
		// keeping a stale or contradictory figure next to the affiliate link.
		if feature != "" && !(pricesShown && mentionsPrice(feature)) {
			cleaned = append(cleaned, feature)
		}
	}

	return cleaned
}

// withoutGlyph returns palette with glyph removed.
func withoutGlyph(palette []string, glyph string) []string {
	filtered := make([]string, 0, len(palette))
	for _, e := range palette {
		if e != glyph {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

// applyTo copies generated fields onto the product, leaving prices, discount,
// and the affiliate link untouched. Empty fields are skipped so a partial
// response degrades to the scraped values rather than blanking them.
//
// The profile is used to normalise features against the layout: when the
// template supplies its own bullet glyph, any the model added is stripped
// here. The prompt asks for this too, but only this step guarantees it.
func (c *EnrichedContent) applyTo(product models.Product, profile generator.TemplateProfile) models.Product {
	out := product

	pricesShown := rendersPrices(profile)

	// plain strips the layout's own decoration back off a field it already
	// wraps, so "🔥 {{.Title}} 🔥" can't render as "🔥 Camera 🔥 🔥", and
	// removes any price the model smuggled into a field it can write.
	plain := func(field, value string) string {
		if pricesShown {
			value = stripPriceMentions(value)
		}
		if profile.Decorates(field) {
			return generator.StripSurroundingDecoration(value)
		}
		return value
	}

	if c.Title != "" {
		out.Title = plain("Title", c.Title)
	}
	if c.Headline != "" {
		out.Headline = plain("Headline", c.Headline)
	}
	if c.Description != "" {
		out.Description = plain("Description", c.Description)
	}
	if len(c.Features) > 0 {
		out.Features = normalizeFeatures(c.Features, profile)
	}
	if c.Tagline != "" {
		out.Tagline = plain("Tagline", c.Tagline)
	}
	if c.Hashtags != "" {
		out.Hashtags = normalizeHashtags(c.Hashtags)
	}

	return out
}

// normalizeHashtags guarantees every tag carries its "#". Models routinely
// return bare words here, which the template renders verbatim - producing a
// line of stray keywords rather than hashtags.
func normalizeHashtags(raw string) string {
	fields := strings.Fields(raw)
	tags := make([]string, 0, len(fields))

	for _, tag := range fields {
		// Models routinely return a comma- or bullet-separated list. Left as
		// is, the trailing punctuation lands inside the tag and Facebook stops
		// linking it: "#AmazonIndia," is not a hashtag.
		tag = strings.Trim(tag, `,;:.|/\-–—`)
		tag = strings.TrimLeft(tag, "#")
		if tag == "" {
			continue
		}
		tags = append(tags, "#"+tag)
	}

	return strings.Join(tags, " ")
}
