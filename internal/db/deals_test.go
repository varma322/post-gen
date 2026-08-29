//go:build integration
// +build integration

package db

import (
	"context"
	"testing"
	"time"

	"post-gen/internal/models"
)

// Deal tests clean up only the ASINs they create, so running them against a
// real database leaves any existing catalog untouched.
const testASINPrefix = "B0DBTEST"

func cleanupDeals(t *testing.T, p *Pool, asins ...string) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		for _, asin := range asins {
			_, _ = p.pool.Exec(ctx, `DELETE FROM deals WHERE asin = $1`, asin)
		}
	})
}

func sampleDeal(asin string) models.Deal {
	return models.Deal{
		ASIN:            asin,
		Title:           "Test Bluetooth Headphones",
		URL:             "https://www.amazon.in/dp/" + asin,
		Category:        "Electronics",
		ImageURL:        "https://example.test/image.jpg",
		Price:           1499.50,
		OldPrice:        4999.00,
		DiscountPercent: 70,
		Score:           95,
		Provider:        models.DealProviderCreatorAPI,
		Status:          models.DealNew,
	}
}

func TestUpsertDealInsertsThenUpdates(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	asin := testASINPrefix + "01"
	cleanupDeals(t, db, asin)

	created, err := db.UpsertDeal(ctx, sampleDeal(asin))
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if !created {
		t.Error("first upsert should report the deal as newly created")
	}

	created, err = db.UpsertDeal(ctx, sampleDeal(asin))
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if created {
		t.Error("second upsert should report an update, not a creation")
	}
}

func TestUpsertDealRoundTripsNumericPrices(t *testing.T) {
	// NUMERIC columns are the one place a driver mismatch would show up
	// silently, so the exact values are checked rather than assumed.
	db := testDB(t)
	ctx := context.Background()

	asin := testASINPrefix + "02"
	cleanupDeals(t, db, asin)

	if _, err := db.UpsertDeal(ctx, sampleDeal(asin)); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	stored, err := db.GetDeal(ctx, asin)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored == nil {
		t.Fatal("expected the deal to be stored")
	}

	if stored.Price != 1499.50 {
		t.Errorf("price = %v, want 1499.50", stored.Price)
	}
	if stored.OldPrice != 4999.00 {
		t.Errorf("old price = %v, want 4999.00", stored.OldPrice)
	}
	if got := stored.Savings(); got != 3499.50 {
		t.Errorf("savings = %v, want 3499.50", got)
	}
	if stored.Category != "Electronics" || stored.DiscountPercent != 70 || stored.Score != 95 {
		t.Errorf("round trip lost fields: %+v", stored)
	}
	if stored.FirstSeen.IsZero() || stored.LastSeen.IsZero() {
		t.Error("first_seen and last_seen should be populated on insert")
	}
}

func TestUpsertDealRefreshesPriceButKeepsFirstSeen(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	asin := testASINPrefix + "03"
	cleanupDeals(t, db, asin)

	if _, err := db.UpsertDeal(ctx, sampleDeal(asin)); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	first, err := db.GetDeal(ctx, asin)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	dropped := sampleDeal(asin)
	dropped.Price = 999.00
	dropped.DiscountPercent = 80
	if _, err := db.UpsertDeal(ctx, dropped); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	after, err := db.GetDeal(ctx, asin)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}

	if after.Price != 999.00 || after.DiscountPercent != 80 {
		t.Errorf("price should be refreshed, got %v / %d%%", after.Price, after.DiscountPercent)
	}
	if !after.FirstSeen.Equal(first.FirstSeen) {
		t.Errorf("first_seen moved from %v to %v; the age of a deal should stay true",
			first.FirstSeen, after.FirstSeen)
	}
}

func TestUpsertDealDoesNotReviveAnIgnoredDeal(t *testing.T) {
	// Re-discovering a deal that was rejected must not quietly reset it to
	// "new" and put it back in front of an operator.
	db := testDB(t)
	ctx := context.Background()

	asin := testASINPrefix + "04"
	cleanupDeals(t, db, asin)

	if _, err := db.UpsertDeal(ctx, sampleDeal(asin)); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if _, err := db.SetDealStatus(ctx, asin, models.DealIgnored); err != nil {
		t.Fatalf("ignoring: %v", err)
	}

	if _, err := db.UpsertDeal(ctx, sampleDeal(asin)); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}

	after, err := db.GetDeal(ctx, asin)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if after.Status != models.DealIgnored {
		t.Errorf("status = %q, want it to stay %q across re-discovery", after.Status, models.DealIgnored)
	}
}

func TestUpsertDealRejectsAnInvalidDeal(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	broken := sampleDeal(testASINPrefix + "05")
	broken.Provider = "not-a-provider"

	if _, err := db.UpsertDeal(ctx, broken); err == nil {
		t.Error("expected validation to reject an unknown provider before hitting the database")
	}
}

func TestGetDealReturnsNilWhenAbsent(t *testing.T) {
	db := testDB(t)

	deal, err := db.GetDeal(context.Background(), testASINPrefix+"ZZ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deal != nil {
		t.Errorf("expected nil for an unknown ASIN, got %+v", deal)
	}
}

func TestSetDealStatusReportsWhetherARowChanged(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	asin := testASINPrefix + "06"
	cleanupDeals(t, db, asin)

	if _, err := db.UpsertDeal(ctx, sampleDeal(asin)); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	changed, err := db.SetDealStatus(ctx, asin, models.DealQueued)
	if err != nil {
		t.Fatalf("set status: %v", err)
	}
	if !changed {
		t.Error("expected the update to report a changed row")
	}

	changed, err = db.SetDealStatus(ctx, testASINPrefix+"NOPE", models.DealQueued)
	if err != nil {
		t.Fatalf("set status on missing deal: %v", err)
	}
	if changed {
		t.Error("updating an unknown ASIN should report no change")
	}

	if _, err := db.SetDealStatus(ctx, asin, "invented"); err == nil {
		t.Error("expected an unknown status to be rejected")
	}
}

func TestListDealsFiltersAndOrders(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	high := testASINPrefix + "07"
	low := testASINPrefix + "08"
	other := testASINPrefix + "09"
	cleanupDeals(t, db, high, low, other)

	best := sampleDeal(high)
	best.Score = 91
	worst := sampleDeal(low)
	worst.Score = 12
	offCategory := sampleDeal(other)
	offCategory.Category = "Books"
	offCategory.Score = 55
	offCategory.Provider = models.DealProviderScraper

	for _, deal := range []models.Deal{worst, best, offCategory} {
		if _, err := db.UpsertDeal(ctx, deal); err != nil {
			t.Fatalf("upsert %s: %v", deal.ASIN, err)
		}
	}

	electronics, err := db.ListDeals(ctx, models.DealFilter{Category: "Electronics", Status: models.DealNew})
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	var seenHigh, seenLow, seenOther bool
	var lastScore = 1 << 30
	for _, deal := range electronics {
		switch deal.ASIN {
		case high:
			seenHigh = true
		case low:
			seenLow = true
		case other:
			seenOther = true
		}
		if deal.Score > lastScore {
			t.Errorf("results are not ordered by score: %d came after %d", deal.Score, lastScore)
		}
		lastScore = deal.Score
	}

	if !seenHigh || !seenLow {
		t.Error("expected both Electronics deals in the filtered list")
	}
	if seenOther {
		t.Error("a Books deal should not appear under a Electronics filter")
	}

	scoped, err := db.ListDeals(ctx, models.DealFilter{Provider: models.DealProviderScraper, MinScore: 50})
	if err != nil {
		t.Fatalf("list by provider: %v", err)
	}
	for _, deal := range scoped {
		if deal.Provider != models.DealProviderScraper || deal.Score < 50 {
			t.Errorf("filter leaked: %+v", deal)
		}
	}
}

func TestKnownASINs(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	stored := testASINPrefix + "10"
	cleanupDeals(t, db, stored)

	if _, err := db.UpsertDeal(ctx, sampleDeal(stored)); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	known, err := db.KnownASINs(ctx, []string{stored, testASINPrefix + "MISS"})
	if err != nil {
		t.Fatalf("known asins: %v", err)
	}
	if !known[stored] {
		t.Error("a stored ASIN should be reported as known")
	}
	if known[testASINPrefix+"MISS"] {
		t.Error("an unstored ASIN should not be reported as known")
	}

	empty, err := db.KnownASINs(ctx, nil)
	if err != nil {
		t.Fatalf("empty known asins: %v", err)
	}
	if len(empty) != 0 {
		t.Error("an empty request should not query or return anything")
	}
}

func TestExpireDealsLeavesQueuedAndPostedAlone(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	stale := testASINPrefix + "11"
	queued := testASINPrefix + "12"
	cleanupDeals(t, db, stale, queued)

	for _, asin := range []string{stale, queued} {
		if _, err := db.UpsertDeal(ctx, sampleDeal(asin)); err != nil {
			t.Fatalf("upsert %s: %v", asin, err)
		}
	}
	if _, err := db.SetDealStatus(ctx, queued, models.DealQueued); err != nil {
		t.Fatalf("queueing: %v", err)
	}

	// Age both rows past the cutoff.
	if _, err := db.pool.Exec(ctx,
		`UPDATE deals SET last_seen = $1 WHERE asin = ANY($2)`,
		time.Now().Add(-48*time.Hour), []string{stale, queued}); err != nil {
		t.Fatalf("ageing rows: %v", err)
	}

	if _, err := db.ExpireDealsNotSeenSince(ctx, time.Now().Add(-24*time.Hour)); err != nil {
		t.Fatalf("expire: %v", err)
	}

	staleDeal, err := db.GetDeal(ctx, stale)
	if err != nil {
		t.Fatalf("get stale: %v", err)
	}
	if staleDeal.Status != models.DealExpired {
		t.Errorf("status = %q, want an unacted stale deal to expire", staleDeal.Status)
	}

	queuedDeal, err := db.GetDeal(ctx, queued)
	if err != nil {
		t.Fatalf("get queued: %v", err)
	}
	if queuedDeal.Status != models.DealQueued {
		t.Errorf("status = %q, want a queued deal left alone by expiry", queuedDeal.Status)
	}
}

func TestDealCountsByProviderAndStatus(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	api := testASINPrefix + "13"
	scraped := testASINPrefix + "14"
	cleanupDeals(t, db, api, scraped)

	fromScraper := sampleDeal(scraped)
	fromScraper.Provider = models.DealProviderScraper

	if _, err := db.UpsertDeal(ctx, sampleDeal(api)); err != nil {
		t.Fatalf("upsert api deal: %v", err)
	}
	if _, err := db.UpsertDeal(ctx, fromScraper); err != nil {
		t.Fatalf("upsert scraped deal: %v", err)
	}

	byProvider, err := db.DealCountsByProvider(ctx)
	if err != nil {
		t.Fatalf("counts by provider: %v", err)
	}
	if byProvider[models.DealProviderCreatorAPI] < 1 || byProvider[models.DealProviderScraper] < 1 {
		t.Errorf("expected both providers counted, got %v", byProvider)
	}

	byStatus, err := db.DealCountsByStatus(ctx)
	if err != nil {
		t.Fatalf("counts by status: %v", err)
	}
	if byStatus[models.DealNew] < 2 {
		t.Errorf("expected at least the two new deals counted, got %v", byStatus)
	}
}

func TestUpsertDealPromotesBetweenNewAndApproved(t *testing.T) {
	// Scoring must be able to promote a deal whose price dropped, and demote
	// one whose price recovered, without a human touching it.
	db := testDB(t)
	ctx := context.Background()

	asin := testASINPrefix + "15"
	cleanupDeals(t, db, asin)

	if _, err := db.UpsertDeal(ctx, sampleDeal(asin)); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	promoted := sampleDeal(asin)
	promoted.Status = models.DealApproved
	if _, err := db.UpsertDeal(ctx, promoted); err != nil {
		t.Fatalf("promoting: %v", err)
	}

	after, err := db.GetDeal(ctx, asin)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if after.Status != models.DealApproved {
		t.Errorf("status = %q, want a re-scored deal promoted to %q", after.Status, models.DealApproved)
	}

	demoted := sampleDeal(asin)
	demoted.Status = models.DealNew
	if _, err := db.UpsertDeal(ctx, demoted); err != nil {
		t.Fatalf("demoting: %v", err)
	}

	after, err = db.GetDeal(ctx, asin)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if after.Status != models.DealNew {
		t.Errorf("status = %q, want demotion back to %q", after.Status, models.DealNew)
	}
}

func TestUpsertDealPreservesTerminalStatuses(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	for i, terminal := range []string{models.DealIgnored, models.DealQueued, models.DealPosted} {
		asin := testASINPrefix + "2" + string(rune('0'+i))
		cleanupDeals(t, db, asin)

		if _, err := db.UpsertDeal(ctx, sampleDeal(asin)); err != nil {
			t.Fatalf("upsert: %v", err)
		}
		if _, err := db.SetDealStatus(ctx, asin, terminal); err != nil {
			t.Fatalf("setting %s: %v", terminal, err)
		}

		// Re-discovery arrives carrying "new", as every candidate does.
		if _, err := db.UpsertDeal(ctx, sampleDeal(asin)); err != nil {
			t.Fatalf("re-upsert: %v", err)
		}

		after, err := db.GetDeal(ctx, asin)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if after.Status != terminal {
			t.Errorf("status = %q, want %q preserved across re-discovery", after.Status, terminal)
		}
	}
}

func TestRecordPriceObservationOnlyStoresChanges(t *testing.T) {
	// Discovery sees the same deal several times an hour. A row per sighting
	// would bury the handful that say something under thousands that repeat.
	db := testDB(t)
	ctx := context.Background()

	asin := testASINPrefix + "30"
	t.Cleanup(func() {
		_, _ = db.pool.Exec(context.Background(), `DELETE FROM deal_price_history WHERE asin = $1`, asin)
	})

	recorded, err := db.RecordPriceObservation(ctx, asin, 1000)
	if err != nil {
		t.Fatalf("first observation: %v", err)
	}
	if !recorded {
		t.Error("the first observation should be stored")
	}

	recorded, err = db.RecordPriceObservation(ctx, asin, 1000)
	if err != nil {
		t.Fatalf("repeat observation: %v", err)
	}
	if recorded {
		t.Error("an unchanged price should not be stored again")
	}

	recorded, err = db.RecordPriceObservation(ctx, asin, 750)
	if err != nil {
		t.Fatalf("changed observation: %v", err)
	}
	if !recorded {
		t.Error("a changed price should be stored")
	}

	history, err := db.PriceHistory(ctx, asin, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("got %d points, want the two distinct prices", len(history))
	}
	if history[0].Price != 1000 || history[1].Price != 750 {
		t.Errorf("history = %+v, want oldest first", history)
	}
}

func TestRecordPriceObservationIgnoresNonPrices(t *testing.T) {
	db := testDB(t)

	recorded, err := db.RecordPriceObservation(context.Background(), testASINPrefix+"31", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if recorded {
		t.Error("a zero price is not an observation")
	}
}

func TestObservedHighsNeedsMoreThanOneObservation(t *testing.T) {
	// A single observation is just the current price and says nothing about
	// movement, so it must not become a reference the score is measured from.
	db := testDB(t)
	ctx := context.Background()

	lonely := testASINPrefix + "32"
	moved := testASINPrefix + "33"
	t.Cleanup(func() {
		_, _ = db.pool.Exec(context.Background(),
			`DELETE FROM deal_price_history WHERE asin = ANY($1)`, []string{lonely, moved})
	})

	if _, err := db.RecordPriceObservation(ctx, lonely, 500); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RecordPriceObservation(ctx, moved, 2000); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RecordPriceObservation(ctx, moved, 900); err != nil {
		t.Fatal(err)
	}

	highs, err := db.ObservedHighs(ctx, []string{lonely, moved}, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("observed highs: %v", err)
	}

	if _, present := highs[lonely]; present {
		t.Error("a single observation should not yield an observed high")
	}
	if highs[moved] != 2000 {
		t.Errorf("observed high = %v, want the highest of the two", highs[moved])
	}
}

func TestObservedHighsRespectsTheWindow(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	asin := testASINPrefix + "34"
	t.Cleanup(func() {
		_, _ = db.pool.Exec(context.Background(), `DELETE FROM deal_price_history WHERE asin = $1`, asin)
	})

	if _, err := db.RecordPriceObservation(ctx, asin, 5000); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RecordPriceObservation(ctx, asin, 1000); err != nil {
		t.Fatal(err)
	}

	// Age the high out of the window; a year-old price should not make today's
	// normal price look like a bargain.
	if _, err := db.pool.Exec(ctx,
		`UPDATE deal_price_history SET observed_at = $1 WHERE asin = $2 AND price = 5000`,
		time.Now().Add(-365*24*time.Hour), asin); err != nil {
		t.Fatalf("ageing: %v", err)
	}

	highs, err := db.ObservedHighs(ctx, []string{asin}, time.Now().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("observed highs: %v", err)
	}
	if _, present := highs[asin]; present {
		t.Errorf("highs = %v, want the aged-out price excluded (leaving too few observations)", highs)
	}
}
