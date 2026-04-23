package utils

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var amazonASINRegex = regexp.MustCompile(`(?i)/(?:dp|gp/product|gp/aw/d|product)/([a-z0-9]{10})(?:[/?]|$)`)

var shortURLResolver = resolveAmazonShortURL

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

func resolveAmazonShortURL(raw string) string {
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
