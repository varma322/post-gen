package generator

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"
)

// TemplateProfile describes what a post template actually asks for, so the AI
// enricher can generate copy that fits the layout it will be rendered into
// rather than generic marketing text.
//
// The seven shipped templates differ in ways that matter to a generator:
// dealsvault uses only Title, DealPrice, and Link, so generating features and
// hashtags for it wastes tokens on output that is discarded; priceswoop and
// smartbuy prefix every feature bullet with their own glyph, so a model that
// also prefixes each bullet produces "✅ 🎯 Long battery life".
type TemplateProfile struct {
	// UsedFields lists the Product fields the template references, sorted.
	UsedFields []string
	// EmojiPalette lists the literal emoji in the template, in first-seen
	// order. Passing it to the model keeps generated copy in the same visual
	// register as the surrounding layout.
	EmojiPalette []string
	// FeaturePrefix is the literal text the template puts before each feature
	// in its range block, empty when it renders them bare.
	FeaturePrefix string
	// FeaturesPrefixed reports whether FeaturePrefix is non-empty. When true,
	// generated feature bullets must not carry their own leading emoji.
	FeaturesPrefixed bool
	// DecoratedFields lists fields the layout already surrounds with emoji on
	// the same line, such as the Title in "🔥 {{.Title}} 🔥". Copy generated
	// for these must be plain text, or the glyphs stack up.
	DecoratedFields []string
}

// Decorates reports whether the layout already puts emoji around the field.
func (p TemplateProfile) Decorates(field string) bool {
	for _, f := range p.DecoratedFields {
		if f == field {
			return true
		}
	}
	return false
}

// Uses reports whether the template references the named Product field.
func (p TemplateProfile) Uses(field string) bool {
	for _, f := range p.UsedFields {
		if f == field {
			return true
		}
	}
	return false
}

var (
	profileCache   = make(map[string]TemplateProfile)
	profileCacheMu sync.RWMutex
)

// actionRe matches a template action, e.g. `{{- range .Features}}`.
var actionRe = regexp.MustCompile(`\{\{[^}]*\}\}`)

// fieldRe matches a reference to an exported Product field inside an action.
// Product fields are all exported, so requiring an initial capital avoids
// matching template builtins and the bare `{{.}}` inside a range body.
var fieldRe = regexp.MustCompile(`\.([A-Z]\w*)`)

// featureRangeRe captures whatever literal text sits between the opening of a
// `range .Features` block and the first action inside it - the per-item prefix.
var featureRangeRe = regexp.MustCompile(`\{\{[-\s]*range\s+\.Features\s*[-\s]*\}\}([^{]*)`)

// Profile reads a template and reports what it uses. Results are cached by
// path; InvalidateCache clears both this and the parsed-template cache, so a
// template edited through the API is re-profiled on the next render.
func Profile(templatePath string) (TemplateProfile, error) {
	profileCacheMu.RLock()
	cached, ok := profileCache[templatePath]
	profileCacheMu.RUnlock()
	if ok {
		return cached, nil
	}

	source, err := os.ReadFile(templatePath)
	if err != nil {
		return TemplateProfile{}, err
	}

	profile := profileFromSource(string(source))

	profileCacheMu.Lock()
	profileCache[templatePath] = profile
	profileCacheMu.Unlock()

	return profile, nil
}

// profileFromSource does the analysis, split out so it is testable without
// touching the filesystem.
func profileFromSource(source string) TemplateProfile {
	profile := TemplateProfile{}

	// Fields: scan inside actions only, so a literal "." in prose can't be
	// mistaken for a field reference.
	fields := make(map[string]struct{})
	for _, action := range actionRe.FindAllString(source, -1) {
		for _, match := range fieldRe.FindAllStringSubmatch(action, -1) {
			fields[match[1]] = struct{}{}
		}
	}
	for field := range fields {
		profile.UsedFields = append(profile.UsedFields, field)
	}
	sort.Strings(profile.UsedFields)

	// Emoji: scan the literal text between actions, so a glyph can only come
	// from the layout itself.
	profile.EmojiPalette = extractEmoji(actionRe.ReplaceAllString(source, " "))

	// Feature prefix: whatever literal precedes the first action inside the
	// `range .Features` block.
	if match := featureRangeRe.FindStringSubmatch(source); match != nil {
		if prefix := strings.TrimSpace(match[1]); prefix != "" {
			profile.FeaturePrefix = prefix
			profile.FeaturesPrefixed = true
		}
	}

	profile.DecoratedFields = findDecoratedFields(source)

	return profile
}

// findDecoratedFields reports fields rendered on a line that already carries
// emoji of its own, so the layout is supplying the decoration.
//
// Line-scoped on purpose: a glyph three lines away says nothing about this
// field, while one on the same line ("🔥 {{.Title}} 🔥") is unambiguously
// wrapping it.
func findDecoratedFields(source string) []string {
	decorated := make(map[string]struct{})

	for _, line := range strings.Split(source, "\n") {
		literal := actionRe.ReplaceAllString(line, " ")
		if len(extractEmoji(literal)) == 0 {
			continue
		}

		for _, action := range actionRe.FindAllString(line, -1) {
			// The features range is handled by FeaturePrefix; treating it as
			// decorated too would strip the wrong end of each bullet.
			if strings.Contains(action, "range") {
				continue
			}
			for _, match := range fieldRe.FindAllStringSubmatch(action, -1) {
				decorated[match[1]] = struct{}{}
			}
		}
	}

	fields := make([]string, 0, len(decorated))
	for field := range decorated {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	return fields
}

// StripLeadingDecoration removes leading emoji, bullet characters, and
// whitespace from s, leaving the first word of real content.
//
// This exists because instructing a model not to prefix its output is
// unreliable - a 7B model shown a palette containing "✅" will cheerfully use
// it as a bullet regardless, producing "✅ ✅ Auto exposure" once the template
// adds its own. Stripping after the fact is the only guarantee.
func StripLeadingDecoration(s string) string {
	runes := []rune(s)

	i := 0
	for i < len(runes) {
		r := runes[i]
		switch {
		case unicode.IsSpace(r), isEmojiBase(r), isEmojiModifier(r), r == zeroWidthJoiner:
			i++
		case r == '-', r == '*', r == '•', r == '·', r == '‣', r == '▪':
			i++
		default:
			return string(runes[i:])
		}
	}

	// Everything was decoration; return the original rather than an empty
	// string, so a pathological response degrades to visible-but-odd instead
	// of silently vanishing.
	return s
}

// StripSurroundingDecoration removes emoji, bullets, and whitespace from both
// ends of s. Used for fields the layout already wraps in its own glyphs.
func StripSurroundingDecoration(s string) string {
	stripped := StripLeadingDecoration(s)

	runes := []rune(stripped)
	end := len(runes)
	for end > 0 {
		r := runes[end-1]
		if unicode.IsSpace(r) || isEmojiBase(r) || isEmojiModifier(r) || r == zeroWidthJoiner {
			end--
			continue
		}
		break
	}

	if end == 0 {
		return stripped
	}
	return string(runes[:end])
}

// extractEmoji pulls emoji out of literal template text in first-seen order,
// keeping each one whole: a base symbol plus any variation selectors and
// zero-width-joiner sequences that follow it.
func extractEmoji(text string) []string {
	var (
		found []string
		seen  = make(map[string]struct{})
	)

	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		if !isEmojiBase(runes[i]) {
			continue
		}

		var cluster strings.Builder
		cluster.WriteRune(runes[i])

		// Absorb the modifiers and joined components that make up one glyph,
		// so "✔️" stays a single palette entry rather than splitting into the
		// dingbat and its variation selector.
		for i+1 < len(runes) {
			next := runes[i+1]
			if isEmojiModifier(next) {
				cluster.WriteRune(next)
				i++
				continue
			}
			if next == zeroWidthJoiner && i+2 < len(runes) && isEmojiBase(runes[i+2]) {
				cluster.WriteRune(next)
				cluster.WriteRune(runes[i+2])
				i += 2
				continue
			}
			break
		}

		glyph := cluster.String()
		if _, dup := seen[glyph]; !dup {
			seen[glyph] = struct{}{}
			found = append(found, glyph)
		}
	}

	return found
}

const (
	zeroWidthJoiner    = '‍'
	variationSelector  = '️'
	variationSelector0 = '︎'
)

// isEmojiBase reports whether r can start an emoji cluster. unicode.So ("other
// symbol") covers the pictographs and dingbats these templates use - ✅ ✨ 🔥
// 💰 ⚡ - and the explicit range catches the supplemental planes.
func isEmojiBase(r rune) bool {
	if r >= 0x1F000 && r <= 0x1FAFF {
		return true
	}
	if r >= 0x2600 && r <= 0x27BF {
		return true
	}
	return unicode.Is(unicode.So, r)
}

// isEmojiModifier reports whether r modifies the preceding glyph rather than
// starting a new one: variation selectors and skin-tone modifiers.
func isEmojiModifier(r rune) bool {
	switch {
	case r == variationSelector, r == variationSelector0:
		return true
	case r >= 0x1F3FB && r <= 0x1F3FF: // skin tone modifiers
		return true
	default:
		return false
	}
}
