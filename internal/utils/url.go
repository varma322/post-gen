package utils

import (
	"net/url"
	"regexp"
	"strings"
)

var amazonDPRegex = regexp.MustCompile(`/dp/([A-Z0-9]{10})`)

// NormalizeAmazonURL trims Amazon links to canonical /dp/<ASIN> form when possible.
func NormalizeAmazonURL(raw string) string {
	matches := amazonDPRegex.FindStringSubmatch(raw)
	if len(matches) > 1 {
		return "https://www.amazon.in/dp/" + matches[1]
	}

	return raw
}

// NormalizeURL applies platform-aware URL normalization.
func NormalizeURL(raw string) string {
	if strings.Contains(raw, "amazon") {
		return NormalizeAmazonURL(raw)
	}

	// Future platforms can be normalized here.
	return raw
}

// AddAffiliateTag injects or overrides the affiliate tag query parameter.
func AddAffiliateTag(raw string, tag string) string {
	if strings.TrimSpace(tag) == "" {
		return raw
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}

	query := parsed.Query()
	query.Set("tag", tag)
	parsed.RawQuery = query.Encode()

	return parsed.String()
}
