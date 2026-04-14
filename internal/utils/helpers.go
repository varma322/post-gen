package utils

import (
	"regexp"
	"strings"
)

// Slugify converts text to a lowercase URL-friendly slug, truncated to the given character limit.
func Slugify(text string, limit int) string {
	text = strings.ToLower(text)
	reg := regexp.MustCompile("[^a-z0-9 ]+")
	text = reg.ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, " ", "_")

	if len(text) > limit {
		text = text[:limit]
	}

	return strings.Trim(text, "_")
}
