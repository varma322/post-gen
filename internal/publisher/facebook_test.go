package publisher

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublishPagePostSuccess(t *testing.T) {
	var sawPermalinkFetch bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The permalink lookup is a follow-up GET on the new post.
		if r.Method == http.MethodGet {
			sawPermalinkFetch = true
			if !strings.HasSuffix(r.URL.Path, "/post_98765") {
				t.Errorf("permalink fetched for unexpected post: %s", r.URL.Path)
			}
			if got := r.URL.Query().Get("fields"); got != "permalink_url" {
				t.Errorf("expected fields=permalink_url, got %q", got)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"permalink_url":"https://www.facebook.com/122390686364020707/posts/98765"}`))
			return
		}

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

	result, err := pub.PublishPagePost("12345", "token123", "cool deal!")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.PostID != "post_98765" {
		t.Fatalf("expected post ID 'post_98765', got '%s'", result.PostID)
	}
	if !sawPermalinkFetch {
		t.Error("expected the permalink to be fetched after publishing")
	}
	// The permalink must come from Graph, not be reconstructed from the page
	// id - New Pages Experience permalinks use a different actor id entirely.
	want := "https://www.facebook.com/122390686364020707/posts/98765"
	if result.Permalink != want {
		t.Fatalf("expected permalink %q, got %q", want, result.Permalink)
	}
}

func TestPublishSucceedsWhenPermalinkLookupFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			// The post is already live at this point; a failure here must not
			// turn a successful publish into a reported failure.
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"post_555"}`))
	}))
	defer server.Close()

	pub := NewFacebookPublisher()
	pub.BaseURL = server.URL

	result, err := pub.PublishPagePost("12345", "token123", "hello")
	if err != nil {
		t.Fatalf("permalink failure must not fail the publish, got: %v", err)
	}
	if result.PostID != "post_555" {
		t.Fatalf("expected post ID 'post_555', got '%s'", result.PostID)
	}
	if result.Permalink != "" {
		t.Fatalf("expected empty permalink, got %q", result.Permalink)
	}
}

func TestPublishUsesConfiguredAPIVersion(t *testing.T) {
	var seenPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			seenPath = r.URL.Path
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"post_1"}`))
	}))
	defer server.Close()

	pub := NewFacebookPublisher()
	pub.BaseURL = server.URL
	pub.APIVersion = "v25.0"

	if _, err := pub.PublishPagePost("12345", "token", "msg"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.HasPrefix(seenPath, "/v25.0/") {
		t.Fatalf("expected the configured version in the path, got %q", seenPath)
	}
}

func TestPublishDefaultsAPIVersionWhenUnset(t *testing.T) {
	// A zero-value struct (as tests and any direct construction produce) must
	// still send a version rather than requesting "//12345/feed".
	pub := &FacebookPublisher{}
	if got := pub.version(); got != defaultAPIVersion {
		t.Fatalf("expected %q, got %q", defaultAPIVersion, got)
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
