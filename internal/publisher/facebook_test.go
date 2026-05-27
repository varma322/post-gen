package publisher

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublishPagePostSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST request, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/12345/feed") {
			t.Fatalf("unexpected request URL: %s", r.URL.Path)
		}

		err := r.ParseForm()
		if err != nil {
			t.Fatal("failed to parse form data")
		}

		if r.Form.Get("message") != "cool deal!" {
			t.Fatalf("expected message 'cool deal!', got '%s'", r.Form.Get("message"))
		}
		if r.Form.Get("link") != "" {
			t.Fatalf("expected no link parameter, got '%s'", r.Form.Get("link"))
		}
		if r.Form.Get("access_token") != "token123" {
			t.Fatalf("expected token 'token123', got '%s'", r.Form.Get("access_token"))
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"post_98765"}`))
	}))
	defer server.Close()

	pub := NewFacebookPublisher()
	pub.BaseURL = server.URL

	postID, err := pub.PublishPagePost("12345", "token123", "cool deal!")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if postID != "post_98765" {
		t.Fatalf("expected post ID 'post_98765', got '%s'", postID)
	}
}

func TestPublishPagePostApiError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid OAuth access token.","type":"OAuthException","code":190}}`))
	}))
	defer server.Close()

	pub := NewFacebookPublisher()
	pub.BaseURL = server.URL

	_, err := pub.PublishPagePost("12345", "badtoken", "cool deal!")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(err.Error(), "Invalid OAuth access token.") {
		t.Fatalf("unexpected error message: %v", err)
	}
	if !strings.Contains(err.Error(), "code 190") {
		t.Fatalf("expected error code 190, got: %v", err)
	}
}

func TestPublishPagePostMissingParams(t *testing.T) {
	pub := NewFacebookPublisher()
	_, err := pub.PublishPagePost("", "token", "msg")
	if err == nil || !strings.Contains(err.Error(), "missing facebook_page_id") {
		t.Fatalf("expected missing params error, got: %v", err)
	}
}
