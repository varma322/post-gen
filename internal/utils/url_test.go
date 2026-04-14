package utils

import "testing"

func TestAddAffiliateTagAddsWhenMissing(t *testing.T) {
	base := "https://www.amazon.in/dp/B0F7QR75X2"
	got := AddAffiliateTag(base, "zonrushdeals-21")
	want := "https://www.amazon.in/dp/B0F7QR75X2?tag=zonrushdeals-21"

	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestAddAffiliateTagOverridesExistingTag(t *testing.T) {
	base := "https://www.amazon.in/dp/B0F7QR75X2?ref=abc&tag=oldtag-21"
	got := AddAffiliateTag(base, "newtag-21")
	want := "https://www.amazon.in/dp/B0F7QR75X2?ref=abc&tag=newtag-21"

	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestAddAffiliateTagLeavesURLWhenTagEmpty(t *testing.T) {
	base := "https://www.amazon.in/dp/B0F7QR75X2"
	got := AddAffiliateTag(base, "")

	if got != base {
		t.Fatalf("expected unchanged URL %q, got %q", base, got)
	}
}
