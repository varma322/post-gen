package publisher

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// FacebookPublisher implements the Page auto-posting integration with Facebook Graph API.
type FacebookPublisher struct {
	Client  *http.Client
	BaseURL string
}

// NewFacebookPublisher creates a new FacebookPublisher client.
func NewFacebookPublisher() *FacebookPublisher {
	return &FacebookPublisher{
		Client:  &http.Client{Timeout: 15 * time.Second},
		BaseURL: "https://graph.facebook.com",
	}
}

// PublishPagePost publishes a text post and associated affiliate link directly to a Facebook Page feed.
func (f *FacebookPublisher) PublishPagePost(pageID, token, message, link string) (string, error) {
	if pageID == "" || token == "" {
		return "", fmt.Errorf("missing facebook_page_id or facebook_access_token")
	}

	apiURL := fmt.Sprintf("%s/v19.0/%s/feed", f.BaseURL, pageID)

	// Send parameters using standard form-urlencoded payload
	data := url.Values{}
	data.Set("message", message)
	data.Set("link", link)
	data.Set("access_token", token)

	resp, err := f.Client.PostForm(apiURL, data)
	if err != nil {
		return "", fmt.Errorf("facebook api network error: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
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
		
		return "", fmt.Errorf("facebook api HTTP %d: %s (code %d)", resp.StatusCode, errMsg, fbErr.Error.Code)
	}

	var fbSuccess struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &fbSuccess); err != nil {
		return "", fmt.Errorf("failed to parse facebook response: %w", err)
	}

	return fbSuccess.ID, nil
}
