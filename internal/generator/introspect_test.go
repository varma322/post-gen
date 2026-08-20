package generator

import (
	"path/filepath"
	"testing"
)

func TestProfileFromSourceFindsFields(t *testing.T) {
	source := `{{.Title}}
{{- range .Features}}
{{.}}
{{- end}}
💰 Deal Price: ₹{{.DealPrice}}{{if .Discount}} ({{.Discount}}% OFF!){{end}}
{{if .Tagline}}{{.Tagline}}{{else}}Grab it{{end}}`

	profile := profileFromSource(source)

	for _, want := range []string{"Title", "Features", "DealPrice", "Discount", "Tagline"} {
		if !profile.Uses(want) {
			t.Errorf("expected field %q to be detected, got %v", want, profile.UsedFields)
		}
	}
	// The bare {{.}} inside the range body is the loop variable, not a field.
	if profile.Uses("") {
		t.Error("bare {{.}} must not register as a field")
	}
}

func TestProfileDetectsFeaturePrefix(t *testing.T) {
	cases := []struct {
		name       string
		source     string
		wantPrefix string
	}{
		{
			name:       "prefixed with check mark",
			source:     "{{- range .Features}}\n✅ {{.}}\n{{- end}}",
			wantPrefix: "✅",
		},
		{
			name:       "prefixed with heavy check",
			source:     "{{- range .Features}}\n✔️ {{.}}\n{{- end}}",
			wantPrefix: "✔️",
		},
		{
			name:       "bare features on their own line",
			source:     "{{- range .Features}}\n{{.}}\n{{- end}}",
			wantPrefix: "",
		},
		{
			name:       "bare features inline",
			source:     "{{range .Features}}{{.}}\n{{end}}",
			wantPrefix: "",
		},
		{
			name:       "prefixed with a bullet character",
			source:     "{{range .Features}}- {{.}}\n{{end}}",
			wantPrefix: "-",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			profile := profileFromSource(tc.source)
			if profile.FeaturePrefix != tc.wantPrefix {
				t.Errorf("FeaturePrefix = %q, want %q", profile.FeaturePrefix, tc.wantPrefix)
			}
			if profile.FeaturesPrefixed != (tc.wantPrefix != "") {
				t.Errorf("FeaturesPrefixed = %v for prefix %q", profile.FeaturesPrefixed, tc.wantPrefix)
			}
		})
	}
}

func TestExtractEmojiKeepsGlyphsWhole(t *testing.T) {
	// ✔️ is a dingbat plus a variation selector; splitting it would put a bare
	// U+2714 in the palette and the model would render the wrong glyph.
	got := extractEmoji("🔹 Key Features ✔️ done 💰 ₹100 🔹 again")
	want := []string{"🔹", "✔️", "💰"}

	if len(got) != len(want) {
		t.Fatalf("got %d emoji %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("emoji[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestExtractEmojiIgnoresPlainText(t *testing.T) {
	if got := extractEmoji("Deal Price: Rs. 8,499 (24% OFF) - buy now"); len(got) != 0 {
		t.Errorf("expected no emoji in plain text, got %v", got)
	}
}

func TestProfileEmojiPaletteScopesToRenderedText(t *testing.T) {
	// A glyph inside the action delimiters is a comparison operand, never
	// rendered, so it must not enter the palette.
	profile := profileFromSource(`{{if eq .Title "🔥"}}x{{end}} 💰 shown`)

	for _, e := range profile.EmojiPalette {
		if e == "🔥" {
			t.Error("emoji inside an action must not enter the palette")
		}
	}
	if len(profile.EmojiPalette) != 1 || profile.EmojiPalette[0] != "💰" {
		t.Errorf("expected only the literal 💰, got %v", profile.EmojiPalette)
	}

	// Conversely, a glyph between two actions is literal output and does
	// belong - it renders whenever the surrounding branch is taken.
	between := profileFromSource(`{{if .Tagline}}✨{{end}}`)
	if len(between.EmojiPalette) != 1 || between.EmojiPalette[0] != "✨" {
		t.Errorf("literal text between actions should be in the palette, got %v", between.EmojiPalette)
	}
}

// TestProfileAgainstShippedTemplates runs the profiler over the real templates,
// which are the actual contract the AI prompt is built from.
func TestProfileAgainstShippedTemplates(t *testing.T) {
	cases := []struct {
		file             string
		wantFields       []string
		wantPrefixed     bool
		wantSomeEmoji    bool
		mustNotUseFields []string
	}{
		{
			file:          "afficart.tmpl",
			wantFields:    []string{"Title", "Features", "DealPrice", "MRP", "Discount", "Link", "Tagline", "Hashtags"},
			wantPrefixed:  false,
			wantSomeEmoji: true,
		},
		{
			file:             "dealsvault.tmpl",
			wantFields:       []string{"Title", "DealPrice", "Link"},
			wantPrefixed:     false,
			wantSomeEmoji:    true,
			mustNotUseFields: []string{"Features", "Tagline", "Hashtags"},
		},
		{
			// priceswoop renders bullets bare so the model supplies a fitting
			// emoji per benefit; smartbuy below keeps its uniform check mark.
			file:          "priceswoop.tmpl",
			wantFields:    []string{"Headline", "Description", "Features", "DealPrice", "MRP", "Discount", "Link", "Hashtags"},
			wantPrefixed:  false,
			wantSomeEmoji: true,
		},
		{
			file:          "smartbuy.tmpl",
			wantFields:    []string{"Title", "Features", "DealPrice", "MRP", "Discount", "Link", "Hashtags"},
			wantPrefixed:  true,
			wantSomeEmoji: true,
		},
		{
			file:          "zonerush.tmpl",
			wantFields:    []string{"Title", "Features", "DealPrice", "MRP", "Discount", "Link", "Tagline", "Hashtags"},
			wantPrefixed:  false,
			wantSomeEmoji: true,
		},
		{
			file:          "hurrydeals.tmpl",
			wantFields:    []string{"Headline", "Description", "Features", "DealPrice", "MRP", "Discount", "Link", "Tagline", "Hashtags"},
			wantPrefixed:  false,
			wantSomeEmoji: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			path := filepath.Join("..", "..", "templates", tc.file)
			profile, err := Profile(path)
			if err != nil {
				t.Fatalf("profiling %s: %v", tc.file, err)
			}

			for _, field := range tc.wantFields {
				if !profile.Uses(field) {
					t.Errorf("%s should use %s, got %v", tc.file, field, profile.UsedFields)
				}
			}
			for _, field := range tc.mustNotUseFields {
				if profile.Uses(field) {
					t.Errorf("%s should not use %s", tc.file, field)
				}
			}
			if profile.FeaturesPrefixed != tc.wantPrefixed {
				t.Errorf("%s FeaturesPrefixed = %v (prefix %q), want %v",
					tc.file, profile.FeaturesPrefixed, profile.FeaturePrefix, tc.wantPrefixed)
			}
			if tc.wantSomeEmoji && len(profile.EmojiPalette) == 0 {
				t.Errorf("%s should have an emoji palette", tc.file)
			}
		})
	}
}

func TestProfileCacheIsInvalidated(t *testing.T) {
	path := filepath.Join("..", "..", "templates", "afficart.tmpl")

	first, err := Profile(path)
	if err != nil {
		t.Fatalf("first profile: %v", err)
	}

	InvalidateCache(path)

	second, err := Profile(path)
	if err != nil {
		t.Fatalf("profile after invalidation: %v", err)
	}
	if len(first.UsedFields) != len(second.UsedFields) {
		t.Error("re-profiling after invalidation produced a different result")
	}
}

func TestProfileMissingTemplateReturnsError(t *testing.T) {
	if _, err := Profile(filepath.Join("..", "..", "templates", "does-not-exist.tmpl")); err == nil {
		t.Fatal("expected an error for a missing template")
	}
}
