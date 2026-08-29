package scraper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// catalogBaseURL is the Creators API root for catalog operations. getItems,
// searchItems and getBrowseNodes differ only in operation name and payload.
const catalogBaseURL = "https://creatorsapi.amazon/catalog/v1/"

// withEligibleAccount runs call against each configured account in turn,
// returning the first success.
//
// An account that answers with a throttle or an eligibility refusal has its
// circuit tripped and the next one is tried, because credentials being present
// is not proof of eligibility: Amazon issues them per application but grants
// API access per Associates account, so a registry entry can authenticate
// cleanly and still be refused. Any other failure - a malformed query, a parse
// error - would fail identically on every account, so it returns immediately
// rather than spending the rest of the quota learning that.
func withEligibleAccount[T any](
	ctx context.Context,
	s *AmazonCreatorAPIScraper,
	marketplace string,
	call func(tokenManager *TokenManager, partnerTag string) (T, error),
) (T, error) {
	var zero T

	attempts := s.registry.Len()
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	for i := 0; i < attempts; i++ {
		tokenManager, partnerTag := s.resolveCredential(ctx, marketplace)
		if partnerTag == "" {
			return zero, fmt.Errorf("%w: no partner tag configured", ErrNoEligibleAccount)
		}
		// resolveCredential already skips accounts whose circuit is open, so
		// reaching here with nothing usable means none are available.
		if tokenManager == nil || creatorAPICircuitOpen(partnerTag, marketplace) {
			return zero, fmt.Errorf("%w: %s is unavailable for %s",
				ErrNoEligibleAccount, partnerTag, marketplace)
		}

		result, err := call(tokenManager, partnerTag)
		if err == nil {
			return result, nil
		}
		lastErr = err

		var throttled *throttleError
		switch {
		case errors.As(err, &throttled):
			tripCreatorAPICircuit(partnerTag, marketplace, throttled.cooldown())
			log.Printf("[WARN] Creators API: %s rate limited on %s; pausing that account for %s",
				partnerTag, marketplace, throttled.cooldown())
		case errors.Is(err, errCreatorsAPIIneligible):
			tripCreatorAPICircuit(partnerTag, marketplace, 1*time.Hour)
			log.Printf("[WARN] Creators API: %s not eligible on %s; pausing that account for 1 hour",
				partnerTag, marketplace)
		default:
			return zero, err
		}
	}

	return zero, lastErr
}

// postCatalog sends one Creators API catalog request and returns the raw
// response body, applying the retry, throttle and eligibility classification
// that every catalog operation needs.
//
// A 429 returns immediately as a throttleError rather than being retried: a
// quota that is already spent is not replenished by asking again a few seconds
// later, and each retry only delays the fallback that would have worked. 5xx is
// retried, because that genuinely can be transient.
func postCatalog(ctx context.Context, tokenManager *TokenManager, operation, marketplace string, payload map[string]any) ([]byte, error) {
	// Token acquisition failures are deliberately not wrapped with
	// errCreatorsAPINetworkFailure: a token-endpoint hiccup says nothing about
	// whether the catalog host is reachable, so callers should treat it as a
	// soft failure rather than aborting their fallback.
	token, err := tokenManager.GetToken()
	if err != nil {
		return nil, fmt.Errorf("auth token error: %w", err)
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshaling %s request payload: %w", operation, err)
	}

	apiURL := catalogBaseURL + operation

	var resp *http.Response
	client := &http.Client{Timeout: 15 * time.Second}
	const maxRetries = 3

	for attempt := 1; attempt <= maxRetries; attempt++ {
		req, reqErr := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(payloadBytes))
		if reqErr != nil {
			return nil, fmt.Errorf("creating %s request: %w", operation, reqErr)
		}

		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-marketplace", marketplace)

		resp, err = client.Do(req)
		if err == nil {
			if resp.StatusCode == http.StatusTooManyRequests {
				retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
				snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
				resp.Body.Close()
				return nil, &throttleError{
					retryAfter: retryAfter,
					detail:     strings.TrimSpace(string(snippet)),
				}
			}
			if resp.StatusCode >= 500 {
				snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
				resp.Body.Close()
				err = fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
			} else {
				break
			}
		}

		log.Printf("[WARN] Creators API %s request attempt %d/%d failed: %v", operation, attempt, maxRetries, err)
		if attempt < maxRetries {
			time.Sleep(time.Duration(attempt*2) * time.Second)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("%w: sending %s request after %d attempts: %v",
			errCreatorsAPINetworkFailure, operation, maxRetries, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading %s response body: %w", operation, err)
	}

	if resp.StatusCode != http.StatusOK {
		text := string(body)
		if strings.Contains(text, "AssociateNotEligible") {
			return nil, fmt.Errorf("%w: %s failed (HTTP %d): %s",
				errCreatorsAPIIneligible, operation, resp.StatusCode, text)
		}
		return nil, fmt.Errorf("%s failed (HTTP %d): %s", operation, resp.StatusCode, text)
	}

	return body, nil
}
