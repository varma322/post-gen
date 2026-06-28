package utils

import "testing"

func TestNormalizeAmazonURLCanonicalizesDPLink(t *testing.T) {
	input := "https://www.amazon.in/Some-Product/dp/B0F7QR75X2?tag=abc&ref=something"
	got := NormalizeAmazonURL(input)
	want := "https://www.amazon.in/dp/B0F7QR75X2"

	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestNormalizeAmazonURLSupportsLowercaseASIN(t *testing.T) {
	input := "https://www.amazon.in/product-name/dp/b0f7qr75x2"
	got := NormalizeAmazonURL(input)
	want := "https://www.amazon.in/dp/B0F7QR75X2"

	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestNormalizeAmazonURLSupportsGPProductPath(t *testing.T) {
	input := "https://www.amazon.in/gp/product/B0F7QR75X2/ref=something"
	got := NormalizeAmazonURL(input)
	want := "https://www.amazon.in/dp/B0F7QR75X2"

	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestNormalizeURLKeepsShortAmazonLinkUnchanged(t *testing.T) {
	originalResolver := shortURLResolver
	shortURLResolver = func(raw string) string {
		return "https://www.amazon.in/some-title/dp/B0F7QR75X2?ref=abc"
	}
	defer func() {
		shortURLResolver = originalResolver
	}()

	got := NormalizeURL("https://amzn.in/d/xyz123")
	want := "https://amzn.in/d/xyz123"

	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestNormalizeURLKeepsAmazonURLUnchanged(t *testing.T) {
	input := "https://www.amazon.in/Some-Product/dp/B0F7QR75X2?tag=abc&ref=something"
	got := NormalizeURL(input)

	if got != input {
		t.Fatalf("expected unchanged URL %q, got %q", input, got)
	}
}

func TestNormalizeURLLeavesNonAmazonURL(t *testing.T) {
	input := "https://example.com/product/123"
	got := NormalizeURL(input)

	if got != input {
		t.Fatalf("expected unchanged URL %q, got %q", input, got)
	}
}

func TestAddAffiliateTagAddsWhenMissing(t *testing.T) {
	base := "https://www.amazon.in/dp/B0F7QR75X2"
	got := AddAffiliateTag(base, "zonrushdeals-21", nil)
	want := "https://www.amazon.in/dp/B0F7QR75X2?th=1&tag=zonrushdeals-21"

	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestAddAffiliateTagOverridesExistingTag(t *testing.T) {
	base := "https://www.amazon.in/dp/B0F7QR75X2?ref=abc&tag=oldtag-21"
	got := AddAffiliateTag(base, "newtag-21", nil)
	want := "https://www.amazon.in/dp/B0F7QR75X2?ref=abc&th=1&tag=newtag-21"

	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestAddAffiliateTagLeavesURLWhenTagEmpty(t *testing.T) {
	base := "https://www.amazon.in/dp/B0F7QR75X2"
	got := AddAffiliateTag(base, "", nil)

	if got != base {
		t.Fatalf("expected unchanged URL %q, got %q", base, got)
	}
}

func TestAddAffiliateTagExtraParamsBeforeTag(t *testing.T) {
	base := "https://www.amazon.in/dp/B0F7QR75X2"
	got := AddAffiliateTag(base, "hurrydeals06-21", map[string]string{"th": "1"})
	want := "https://www.amazon.in/dp/B0F7QR75X2?th=1&tag=hurrydeals06-21"

	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
