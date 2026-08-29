package scraper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"post-gen/internal/config"
)

// redirectSharedClient points the scraper's pooled client at a test server for
// the duration of one test.
func redirectSharedClient(t *testing.T, serverURL string) {
	t.Helper()

	original := sharedHTTPClient.Transport
	t.Cleanup(func() { sharedHTTPClient.Transport = original })

	sharedHTTPClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Host, "amazon.") {
			target, _ := url.Parse(serverURL)
			req.URL.Scheme, req.URL.Host = target.Scheme, target.Host
		}
		return original.RoundTrip(req)
	})
}

func listingSelectors() config.ListingSelectors {
	return config.ListingSelectors{
		Item:  ".tile",
		ASIN:  "[data-asin]",
		Title: ".title",
		Price: ".price",
		MRP:   ".mrp",
		Link:  "a",
	}
}

// listingServers serves html for any bestsellers request, recording the paths
// that were asked for.
func listingServers(t *testing.T, html string) (*AmazonListScraper, *[]string) {
	t.Helper()

	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(html))
	}))
	t.Cleanup(server.Close)

	// The listing fetch uses the pooled shared client, not http.DefaultClient,
	// so this is the seam that actually intercepts it. Overriding
	// http.DefaultTransport instead would let the test hit the real Amazon.
	redirectSharedClient(t, server.URL)

	return NewAmazonListScraper(listingSelectors()), &paths
}

const twoTiles = `<html><body>
  <div class="tile" data-asin="B0TILE0001">
    <a href="/dp/B0TILE0001?ref=x"></a>
    <span class="title">Discounted Headphones</span>
    <span class="price">₹999</span>
    <span class="mrp">₹4,999</span>
  </div>
  <div class="tile" data-asin="B0TILE0002">
    <a href="/dp/B0TILE0002"></a>
    <span class="title">Full Price Kettle</span>
    <span class="price">₹1,999</span>
    <span class="mrp">₹2,099</span>
  </div>
</body></html>`

func TestListReadsTiles(t *testing.T) {
	lister, paths := listingServers(t, twoTiles)

	candidates, err := lister.List(context.Background(), ListOptions{
		Slug: "electronics", Category: "Electronics",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("got %d candidates, want 2", len(candidates))
	}

	first := candidates[0]
	if first.ASIN != "B0TILE0001" {
		t.Errorf("ASIN = %q", first.ASIN)
	}
	if first.Title != "Discounted Headphones" {
		t.Errorf("Title = %q", first.Title)
	}
	if first.Price != 999 || first.OldPrice != 4999 {
		t.Errorf("prices = %v / %v, want them read from the tile", first.Price, first.OldPrice)
	}
	if first.DiscountPct != 80 {
		t.Errorf("DiscountPct = %d, want 80 computed from the tile", first.DiscountPct)
	}
	if first.Category != "Electronics" {
		t.Errorf("Category = %q, want the label from the options", first.Category)
	}
	if first.Provider != "scraper" {
		t.Errorf("Provider = %q, want scraper", first.Provider)
	}

	if len(*paths) != 1 || !strings.Contains((*paths)[0], "/gp/bestsellers/electronics") {
		t.Errorf("requested %v, want the bestsellers path", *paths)
	}
}

func TestListBuildsCanonicalURLs(t *testing.T) {
	// The tile's href carries tracking parameters and sometimes a tag that is
	// not ours; the published link must be rebuilt from the ASIN.
	lister, _ := listingServers(t, twoTiles)

	candidates, err := lister.List(context.Background(), ListOptions{Slug: "electronics"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "https://www.amazon.in/dp/B0TILE0001"
	if candidates[0].URL != want {
		t.Errorf("URL = %q, want %q", candidates[0].URL, want)
	}
}

func TestListPreFiltersOnTileDiscount(t *testing.T) {
	// This is what makes the fallback affordable: without it every tile costs a
	// product fetch, and a bestseller list is mostly full-price items.
	lister, _ := listingServers(t, twoTiles)

	candidates, err := lister.List(context.Background(), ListOptions{
		Slug: "electronics", MinDiscount: 50,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("got %d candidates, want only the one clearing 50%%", len(candidates))
	}
	if candidates[0].ASIN != "B0TILE0001" {
		t.Errorf("kept %q, want the discounted tile", candidates[0].ASIN)
	}
}

func TestListKeepsTilesWithUnreadablePrices(t *testing.T) {
	// A tile whose prices could not be parsed may still be a real deal; the
	// product page decides. Dropping it here would lose deals to a selector
	// change rather than to a genuine lack of discount.
	noPrices := `<html><body>
	  <div class="tile" data-asin="B0NOPRICE1">
	    <a href="/dp/B0NOPRICE1"></a>
	    <span class="title">Mystery Item</span>
	  </div>
	</body></html>`

	lister, _ := listingServers(t, noPrices)

	candidates, err := lister.List(context.Background(), ListOptions{
		Slug: "electronics", MinDiscount: 70,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candidates) != 1 {
		t.Errorf("got %d candidates, want the unpriced tile kept", len(candidates))
	}
}

func TestListFallsBackToTheLinkForTheASIN(t *testing.T) {
	noAttr := `<html><body>
	  <div class="tile">
	    <a href="https://www.amazon.in/some-product/dp/B0FROMLNK1/ref=zg_bs">x</a>
	    <span class="title">Linked Product</span>
	  </div>
	</body></html>`

	lister, _ := listingServers(t, noAttr)

	candidates, err := lister.List(context.Background(), ListOptions{Slug: "electronics"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candidates) != 1 || candidates[0].ASIN != "B0FROMLNK1" {
		t.Fatalf("got %+v, want the ASIN recovered from the link", candidates)
	}
}

func TestListSkipsTilesThatAreNotProducts(t *testing.T) {
	junk := `<html><body>
	  <div class="tile"><span class="title">A banner with no product</span></div>
	  <div class="tile" data-asin="B0REALITM1"><a href="/dp/B0REALITM1"></a><span class="title">Real</span></div>
	  <div class="tile" data-asin="B0NOTITLE1"><a href="/dp/B0NOTITLE1"></a></div>
	</body></html>`

	lister, _ := listingServers(t, junk)

	candidates, err := lister.List(context.Background(), ListOptions{Slug: "electronics"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candidates) != 1 || candidates[0].ASIN != "B0REALITM1" {
		t.Errorf("got %+v, want only the complete tile", candidates)
	}
}

func TestListDeduplicatesWithinAPage(t *testing.T) {
	repeated := `<html><body>
	  <div class="tile" data-asin="B0REPEAT01"><a href="/dp/B0REPEAT01"></a><span class="title">Once</span></div>
	  <div class="tile" data-asin="B0REPEAT01"><a href="/dp/B0REPEAT01"></a><span class="title">Twice</span></div>
	</body></html>`

	lister, _ := listingServers(t, repeated)

	candidates, err := lister.List(context.Background(), ListOptions{Slug: "electronics"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candidates) != 1 {
		t.Errorf("got %d candidates, want the repeat collapsed", len(candidates))
	}
}

func TestListHonoursTheLimit(t *testing.T) {
	lister, _ := listingServers(t, twoTiles)

	candidates, err := lister.List(context.Background(), ListOptions{Slug: "electronics", Limit: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candidates) != 1 {
		t.Errorf("got %d candidates, want the limit honoured", len(candidates))
	}
}

func TestListReadsMoversAndShakers(t *testing.T) {
	lister, paths := listingServers(t, twoTiles)

	if _, err := lister.List(context.Background(), ListOptions{
		Slug: "electronics", MoversAndShakers: true,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(*paths) != 1 || !strings.Contains((*paths)[0], "/gp/movers-and-shakers/electronics") {
		t.Errorf("requested %v, want the movers-and-shakers path", *paths)
	}
}

func TestListRequiresConfigurationAndASlug(t *testing.T) {
	unconfigured := NewAmazonListScraper(config.ListingSelectors{})
	if _, err := unconfigured.List(context.Background(), ListOptions{Slug: "electronics"}); err == nil {
		t.Error("expected missing selectors to be reported")
	}

	lister, _ := listingServers(t, twoTiles)
	if _, err := lister.List(context.Background(), ListOptions{}); err == nil {
		t.Error("expected a missing slug to be reported")
	}
}

func TestListAbortsOnAHardBlock(t *testing.T) {
	// A 403 will not relent inside a retry window, and hammering it is how an
	// IP earns a longer block.
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(server.Close)

	// The listing fetch uses the pooled shared client, not http.DefaultClient,
	// so this is the seam that actually intercepts it. Overriding
	// http.DefaultTransport instead would let the test hit the real Amazon.
	redirectSharedClient(t, server.URL)

	lister := NewAmazonListScraper(listingSelectors())
	if _, err := lister.List(context.Background(), ListOptions{Slug: "electronics"}); err == nil {
		t.Fatal("expected a 403 to be reported as an error")
	}
	if calls != 1 {
		t.Errorf("made %d requests on a 403, want exactly 1", calls)
	}
}
