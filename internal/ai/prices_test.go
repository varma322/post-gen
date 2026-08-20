package ai

import (
	"testing"

	"post-gen/internal/generator"
)

func TestMentionsPrice(t *testing.T) {
	shouldMatch := []string{
		"Only ₹1,999.00 today",
		"Get it for Rs. 1999",
		"INR 1,999 flat",
		"Just 1999 rupees",
		"56% OFF right now",
		"save 41%",
		"2 % discount",
	}
	for _, s := range shouldMatch {
		if !mentionsPrice(s) {
			t.Errorf("mentionsPrice(%q) = false, want true", s)
		}
	}

	// Must not fire on ordinary product copy that happens to carry digits.
	shouldNotMatch := []string{
		"4 spinner wheels for 360° movement",
		"Medium 66cm size suits 3 to 5 day trips",
		"20000 mAh capacity, 22.5W fast charging",
		"Backed by a 10-year warranty",
		"Charge 3 devices simultaneously",
	}
	for _, s := range shouldNotMatch {
		if mentionsPrice(s) {
			t.Errorf("mentionsPrice(%q) = true, want false", s)
		}
	}
}

func TestStripPriceMentions(t *testing.T) {
	cases := []struct{ in, want string }{
		// Observed live on smartbuy.tmpl.
		{"Genius 66cm Spinner Trolley - Black, Only ₹1,999.00", "Genius 66cm Spinner Trolley - Black, Only"},
		{"Massive Backup at just ₹899", "Massive Backup at just"},
		// Observed with gemma3: stripping must not leave the punctuation
		// stranded a space away from the word before it.
		{"Travel Smart, Travel Safe – Massive 56% OFF!", "Travel Smart, Travel Safe – Massive!"},
		{"Best value ₹1,999 , grab it", "Best value, grab it"},
		// Nothing to strip - returned untouched.
		{"Two Mattresses In One. Flip For Your Mood.", "Two Mattresses In One. Flip For Your Mood."},
		// Nothing but a price claim: keep the original rather than blank it.
		{"₹1,999", "₹1,999"},
		{"56% OFF", "56% OFF"},
	}

	for _, tc := range cases {
		if got := stripPriceMentions(tc.in); got != tc.want {
			t.Errorf("stripPriceMentions(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeFeaturesDropsPriceBullets(t *testing.T) {
	// afficart renders a price and an M.R.P. line, so a bullet repeating them
	// is redundant at best and contradictory at worst.
	profile := generator.TemplateProfile{UsedFields: []string{"DealPrice", "MRP", "Discount", "Features"}}

	got := normalizeFeatures([]string{
		"🧳 Hard polypropylene resists airport handling",
		"💰 56% off - get it for just ₹1,999.00 now",
		"🔒 Secure combination lock built in",
	}, profile)

	if len(got) != 2 {
		t.Fatalf("expected the price bullet dropped, got %v", got)
	}
	for _, feature := range got {
		if mentionsPrice(feature) {
			t.Errorf("price bullet survived: %q", feature)
		}
	}
}

func TestNormalizeFeaturesKeepsPriceBulletsWhenLayoutShowsNoPrice(t *testing.T) {
	// A layout with no price line of its own has nothing to duplicate.
	profile := generator.TemplateProfile{UsedFields: []string{"Features"}}

	got := normalizeFeatures([]string{"💰 56% off today"}, profile)
	if len(got) != 1 {
		t.Fatalf("bullet dropped despite no price in the layout: %v", got)
	}
}
