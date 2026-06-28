package utils

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var amazonASINRegex = regexp.MustCompile(`(?i)/(?:dp|gp/product|gp/aw/d|product)/([a-z0-9]{10})(?:[/?]|$)`)

var shortURLResolver = ResolveAmazonShortURL

// NormalizeAmazonURL trims Amazon links to canonical /dp/<ASIN> form when possible.
func NormalizeAmazonURL(raw string) string {
	matches := amazonASINRegex.FindStringSubmatch(raw)
	if len(matches) > 1 {
		return "https://www.amazon.in/dp/" + strings.ToUpper(matches[1])
	}

	return raw
}

// NormalizeURL applies platform-aware URL normalization.
func NormalizeURL(raw string) string {
	// Temporary mode: keep the full provided URL untouched for scraping.
	return strings.TrimSpace(raw)
}

// ResolveAmazonShortURL resolves amzn.to/amzn.in short URLs to their full redirect locations.
func ResolveAmazonShortURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}

	host := strings.ToLower(parsed.Host)
	if !strings.Contains(host, "amzn.") {
		return raw
	}

	client := &http.Client{Timeout: 12 * time.Second}
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		return raw
	}

	resp, err := client.Do(req)
	if err != nil {
		return raw
	}
	defer resp.Body.Close()

	if resp.Request == nil || resp.Request.URL == nil {
		return raw
	}

	resolved := resp.Request.URL.String()
	if strings.TrimSpace(resolved) == "" {
		return raw
	}

	return resolved
}


// AddAffiliateTag injects or overrides the affiliate tag query parameter.
// Extra params (e.g. {"th": "1"}) are appended before the tag so the tag always appears last.
func AddAffiliateTag(raw string, tag string, extra map[string]string) string {
	if strings.TrimSpace(tag) == "" {
		return raw
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}

	// Strip tag and extra keys from existing params so we rebuild them in the right order.
	existing := parsed.Query()
	existing.Del("tag")
	for k := range extra {
		existing.Del(k)
	}

	// th=1 is added to all affiliate links before the tag.
	existing.Del("th")

	var sb strings.Builder
	if enc := existing.Encode(); enc != "" {
		sb.WriteString(enc)
	}
	for k, v := range extra {
		if k == "th" {
			continue // already hardcoded above
		}
		if sb.Len() > 0 {
			sb.WriteByte('&')
		}
		sb.WriteString(url.QueryEscape(k))
		sb.WriteByte('=')
		sb.WriteString(url.QueryEscape(v))
	}
	if sb.Len() > 0 {
		sb.WriteByte('&')
	}
	sb.WriteString("th=1&tag=")
	sb.WriteString(url.QueryEscape(tag))

	parsed.RawQuery = sb.String()
	return parsed.String()
}
