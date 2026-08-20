package generator

import (
	"path/filepath"
	"strings"
	"testing"

	"post-gen/internal/models"
)

// discountedFixture is a product on a genuine markdown: the Creator API
// returned a SavingBasis, so MRP is above DealPrice and Discount is set.
func discountedFixture() models.Product {
	return models.Product{
		Title:       "Nilkamal SLEEP Lite Dual Comfort 5-Inch Mattress (Single)",
		Headline:    "Two Mattresses In One",
		Description: "Soft on one side, firm on the other.",
		Features:    []string{"🔄 Flip for soft or firm support", "🛡️ Backed by a 10-year warranty"},
		DealPrice:   "4,590.00",
		MRP:         "7,775",
		Discount:    "41",
		Link:        "https://www.amazon.in/dp/B0EXAMPLE?tag=afficartzone-21",
		Tagline:     "Sleep better tonight.",
		Hashtags:    "#AmazonIndia #MattressDeals",
	}
}

// flatFixture is the case that has to stay silent. With no SavingBasis in the
// API response, amazon_api.go back-fills MRP from DealPrice and Discount comes
// back empty - so a layout must not print "M.R.P. ₹899" beside "Price ₹899".
func flatFixture() models.Product {
	p := discountedFixture()
	p.DealPrice = "899"
	p.MRP = "899"
	p.Discount = ""
	return p
}

func renderAll(t *testing.T, product models.Product) map[string]string {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join("..", "..", "templates", "*.tmpl"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("globbing templates: %v (found %d)", err, len(paths))
	}

	rendered := make(map[string]string, len(paths))
	for _, path := range paths {
		out, err := GeneratePost(product, path)
		if err != nil {
			t.Fatalf("rendering %s: %v", path, err)
		}
		rendered[filepath.Base(path)] = out
	}
	return rendered
}

// TestTemplatesHideMRPWithoutADiscount is the regression that shipped: every
// layout gated its M.R.P. line on `{{if .MRP}}`, which is always true because
// the scraper back-fills it, producing an identical struck-through price.
func TestTemplatesHideMRPWithoutADiscount(t *testing.T) {
	for name, out := range renderAll(t, flatFixture()) {
		if strings.Contains(out, "M.R.P.") {
			t.Errorf("%s prints an M.R.P. line with no discount:\n%s", name, out)
		}
		if strings.Contains(out, "You Save") {
			t.Errorf("%s prints a savings line with no discount:\n%s", name, out)
		}
	}
}

func TestTemplatesShowMRPWhenDiscounted(t *testing.T) {
	rendered := renderAll(t, discountedFixture())

	// The one-line layouts are deliberately price-and-link only.
	terse := map[string]bool{"dealsvault.tmpl": true, "notyoffers.tmpl": true}

	for name, out := range rendered {
		if terse[name] {
			continue
		}
		if !strings.Contains(out, "7,775") {
			t.Errorf("%s omits the M.R.P. on a discounted product:\n%s", name, out)
		}
		if !strings.Contains(out, "41%") {
			t.Errorf("%s omits the discount percentage:\n%s", name, out)
		}
	}
}

// TestTemplatesFormatPricesConsistently guards the mixed-format rendering that
// put "₹1,079" next to "₹1,100.00" in published posts.
func TestTemplatesFormatPricesConsistently(t *testing.T) {
	for name, out := range renderAll(t, discountedFixture()) {
		if strings.Contains(out, "4,590.00") {
			t.Errorf("%s renders a zero paise fraction, want ₹4,590:\n%s", name, out)
		}
	}
}

// TestTemplatesCarryHashtagsAndLink checks the parts every post needs.
func TestTemplatesCarryHashtagsAndLink(t *testing.T) {
	rendered := renderAll(t, discountedFixture())
	terse := map[string]bool{"dealsvault.tmpl": true, "notyoffers.tmpl": true}

	for name, out := range rendered {
		if !strings.Contains(out, "?tag=afficartzone-21") {
			t.Errorf("%s dropped the affiliate link:\n%s", name, out)
		}
		if terse[name] {
			continue
		}
		if !strings.Contains(out, "#AmazonIndia") {
			t.Errorf("%s dropped the hashtags:\n%s", name, out)
		}
	}
}

// TestTemplatesLeaveNoBlankRun catches the whitespace faults that are easy to
// introduce with {{- -}} trimming and invisible in a diff.
func TestTemplatesLeaveNoBlankRun(t *testing.T) {
	for _, product := range []models.Product{discountedFixture(), flatFixture()} {
		for name, out := range renderAll(t, product) {
			if strings.Contains(out, "\n\n\n") {
				t.Errorf("%s has a run of blank lines:\n%q", name, out)
			}
		}
	}
}
