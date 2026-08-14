package publisher

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// defaultAPIVersion is the Graph API version used when FACEBOOK_API_VERSION is
// unset. Graph versions are sunset roughly two years after release; requesting
// a sunset version does not fail loudly, it is silently served by whatever
// version Facebook considers current, so this needs deliberate maintenance.
const defaultAPIVersion = "v23.0"

// FacebookPublisher implements the Page auto-posting integration with Facebook Graph API.
type FacebookPublisher struct {
	Client     *http.Client
	BaseURL    string
	APIVersion string
}

// PublishResult describes a successfully created post.
type PublishResult struct {
	// PostID is the composite "{page-id}_{post-id}" returned by Graph.
	PostID string
	// Permalink is the canonical public URL. It is fetched in a follow-up call
	// and may be empty if that call fails - publishing already succeeded by
	// then, so a missing permalink is never treated as a publish failure.
	Permalink string
}

// NewFacebookPublisher creates a new FacebookPublisher client.
func NewFacebookPublisher() *FacebookPublisher {
	version := strings.TrimSpace(os.Getenv("FACEBOOK_API_VERSION"))
	if version == "" {
		version = defaultAPIVersion
	}

	return &FacebookPublisher{
		Client:     &http.Client{Timeout: 15 * time.Second},
		BaseURL:    "https://graph.facebook.com",
		APIVersion: version,
	}
}

// version returns the configured Graph version, defaulting when the struct was
// built directly rather than through NewFacebookPublisher (as tests do).
func (f *FacebookPublisher) version() string {
	if f.APIVersion == "" {
		return defaultAPIVersion
	}
	return f.APIVersion
}

// PublishPagePost publishes a text post directly to a Facebook Page feed.
func (f *FacebookPublisher) PublishPagePost(pageID, token, message string) (PublishResult, error) {
	if pageID == "" || token == "" {
		return PublishResult{}, fmt.Errorf("missing facebook_page_id or facebook_access_token")
	}

	apiURL := fmt.Sprintf("%s/%s/%s/feed", f.BaseURL, f.version(), pageID)

	// Send parameters using standard form-urlencoded payload
	data := url.Values{}
	data.Set("message", message)
	data.Set("access_token", token)

	resp, err := f.Client.PostForm(apiURL, data)
	if err != nil {
		return PublishResult{}, fmt.Errorf("facebook api network error: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return PublishResult{}, f.decodeError(resp.StatusCode, body)
	}

	var fbSuccess struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &fbSuccess); err != nil {
		return PublishResult{}, fmt.Errorf("failed to parse facebook response: %w", err)
	}

	return PublishResult{
		PostID:    fbSuccess.ID,
		Permalink: f.fetchPermalink(fbSuccess.ID, token),
	}, nil
}

// fetchPermalink resolves a post's canonical URL.
//
// The permalink cannot be reconstructed from the composite post ID: pages on
// the New Pages Experience are addressed by an actor id distinct from the page
// id, so "facebook.com/{page-id}/posts/{post-id}" produces a dead link. Graph
// is the only source of the real URL.
//
// Errors are swallowed deliberately - the post is already published, and
// failing the publish over a missing display URL would be the wrong trade.
func (f *FacebookPublisher) fetchPermalink(postID, token string) string {
	if postID == "" {
		return ""
	}

	endpoint := fmt.Sprintf("%s/%s/%s", f.BaseURL, f.version(), url.PathEscape(postID))
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return ""
	}

	query := req.URL.Query()
	query.Set("fields", "permalink_url")
	query.Set("access_token", token)
	req.URL.RawQuery = query.Encode()

	resp, err := f.Client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var payload struct {
		PermalinkURL string `json:"permalink_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return ""
	}

	return payload.PermalinkURL
}

// decodeError unwraps a Graph error body into a readable message, so failures
// surface as "(#190) Error validating access token" rather than raw JSON.
func (f *FacebookPublisher) decodeError(status int, body []byte) error {
	var fbErr struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    int    `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &fbErr)

	errMsg := "facebook api error"
	if fbErr.Error.Message != "" {
		errMsg = fbErr.Error.Message
	} else if len(body) > 0 {
		errMsg = string(body)
	}

	return fmt.Errorf("facebook api HTTP %d: %s (code %d)", status, errMsg, fbErr.Error.Code)
}
