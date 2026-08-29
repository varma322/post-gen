package deals

import (
	"testing"

	"post-gen/internal/models"
)

func deal(discount int, price, oldPrice float64, category string) models.Deal {
	return models.Deal{
		ASIN:            "B0SCORE0001",
		Title:           "Test",
		URL:             "https://www.amazon.in/dp/B0SCORE0001",
		DiscountPercent: discount,
		Price:           price,
		OldPrice:        oldPrice,
		Category:        category,
		Provider:        models.DealProviderCreatorAPI,
		Status:          models.DealNew,
	}
}

func TestScoreCombinesAllThreeTerms(t *testing.T) {
	// 70% off, ₹3,500 saved, Electronics: 70 + 30 + 25.
	got := Score(deal(70, 1500, 5000, "Electronics"))
	if got != 125 {
		t.Errorf("Score = %d, want 125", got)
	}
}

func TestDiscountTiers(t *testing.T) {
	cases := []struct {
		percent int
		want    int
	}{
		{0, 0}, {9, 0},
		{10, 10}, {19, 10},
		{20, 20}, {29, 20},
		{30, 30}, {49, 30},
		{50, 50}, {69, 50},
		{70, 70}, {95, 70},
	}

	for _, tc := range cases {
		// Isolate the discount term: no savings, unscored category.
		got := Score(deal(tc.percent, 0, 0, "Unlisted"))
		if got != tc.want {
			t.Errorf("%d%% off scored %d, want %d", tc.percent, got, tc.want)
		}
	}
}

func TestSavingsTiers(t *testing.T) {
	cases := []struct {
		saved float64
		want  int
	}{
		{0, 0}, {99, 0},
		{100, 5}, {499, 5},
		{500, 10}, {999, 10},
		{1000, 20}, {1999, 20},
		{2000, 30}, {50000, 30},
	}

	for _, tc := range cases {
		got := Score(deal(0, 0, tc.saved, "Unlisted"))
		if got != tc.want {
			t.Errorf("₹%.0f saved scored %d, want %d", tc.saved, got, tc.want)
		}
	}
}

func TestCategoryScoreIsCaseInsensitive(t *testing.T) {
	for _, name := range []string{"Electronics", "electronics", "  ELECTRONICS  "} {
		if got := CategoryScore(name); got != 25 {
			t.Errorf("CategoryScore(%q) = %d, want 25", name, got)
		}
	}
}

func TestUnlistedCategoryScoresZero(t *testing.T) {
	// Adding a category to discovery should be a deliberate act of also
	// scoring it, rather than silently inheriting a default weight.
	if got := CategoryScore("Automotive"); got != 0 {
		t.Errorf("CategoryScore = %d, want 0 for an unlisted category", got)
	}
	if got := CategoryScore(""); got != 0 {
		t.Errorf("CategoryScore = %d, want 0 for a blank category", got)
	}
}

func TestCategoryWeights(t *testing.T) {
	want := map[string]int{
		"Electronics": 25,
		"Home":        20,
		"Kitchen":     20,
		"Fashion":     15,
		"Books":       5,
	}
	for name, points := range want {
		if got := CategoryScore(name); got != points {
			t.Errorf("CategoryScore(%s) = %d, want %d", name, got, points)
		}
	}
}

func TestSavingsIgnoresAnInvertedOldPrice(t *testing.T) {
	// An inflated-MRP feed sometimes reports an old price below the current
	// one. That is not a negative saving, and must not drag the score down.
	got := Score(deal(0, 1500, 1200, "Unlisted"))
	if got != 0 {
		t.Errorf("Score = %d, want 0 rather than a negative saving", got)
	}
}

func TestDecideThresholds(t *testing.T) {
	cases := []struct {
		score int
		want  Decision
	}{
		{125, DecisionQueue},
		{70, DecisionQueue},
		{69, DecisionReview},
		{50, DecisionReview},
		{49, DecisionIgnore},
		{0, DecisionIgnore},
	}

	for _, tc := range cases {
		if got := Decide(tc.score); got != tc.want {
			t.Errorf("Decide(%d) = %q, want %q", tc.score, got, tc.want)
		}
	}
}

func TestRealisticDealsLandWhereExpected(t *testing.T) {
	cases := []struct {
		name string
		deal models.Deal
		want Decision
	}{
		{
			// A steep discount on a cheap accessory: high percentage, but only
			// ₹300 saved. Still queues on the discount term alone.
			name: "cheap accessory, 85% off",
			deal: deal(85, 50, 350, "Electronics"),
			want: DecisionQueue,
		},
		{
			// Modest percentage but real money off a big-ticket item.
			name: "television, 25% off",
			deal: deal(25, 45000, 60000, "Electronics"),
			want: DecisionQueue,
		},
		{
			// Small discount, small saving, low-weight category.
			name: "paperback, 15% off",
			deal: deal(15, 340, 400, "Books"),
			want: DecisionIgnore,
		},
		{
			name: "kitchen kettle, 30% off ₹600",
			deal: deal(30, 1400, 2000, "Kitchen"),
			want: DecisionReview,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			score := Score(tc.deal)
			if got := Decide(score); got != tc.want {
				t.Errorf("scored %d -> %q, want %q", score, got, tc.want)
			}
		})
	}
}

func TestScoredCategoriesIsSorted(t *testing.T) {
	names := ScoredCategories()
	if len(names) != 5 {
		t.Fatalf("got %d categories, want 5", len(names))
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Errorf("ScoredCategories is not sorted: %v", names)
			break
		}
	}
}

func TestEveryDiscoveryCategoryIsAlsoScored(t *testing.T) {
	// A category that discovery searches but scoring does not weight would
	// quietly lose 25 points against its peers.
	for _, category := range VerifiedCategories {
		if CategoryScore(category.Name) == 0 {
			t.Errorf("discovery category %q has no scoring weight", category.Name)
		}
	}
}

func TestTrueDiscountBeatsAnInflatedListPrice(t *testing.T) {
	// The headline case: a product that has never sold above ₹1,200 listed
	// with an MRP of ₹5,000. Amazon reports 80% off; against a price anyone
	// actually paid it is 17%.
	inflated := deal(80, 999, 5000, "Books")

	reported := Score(inflated)
	grounded := ScoreAgainst(inflated, 1200)

	if reported <= grounded {
		t.Errorf("reported score %d should exceed the grounded one %d", reported, grounded)
	}
	if Decide(reported) != DecisionQueue {
		t.Errorf("test setup: the inflated deal should have queued at %d", reported)
	}
	if Decide(grounded) == DecisionQueue {
		t.Errorf("grounded score %d still queues; the observed high should have stopped it", grounded)
	}
}

func TestTrueDiscountRewardsAGenuineDrop(t *testing.T) {
	// The mirror case: a product seen at ₹4,000 now selling at ₹1,000, but
	// with no list price to report it against.
	genuine := deal(0, 1000, 0, "Electronics")

	if got := Score(genuine); got != 25 {
		t.Fatalf("test setup: unreported discount should score category only, got %d", got)
	}

	grounded := ScoreAgainst(genuine, 4000)
	if Decide(grounded) != DecisionQueue {
		t.Errorf("grounded score %d should queue a real 75%% drop", grounded)
	}
}

func TestScoreAgainstFallsBackWithoutHistory(t *testing.T) {
	// No history must leave the reported figure standing, not score at zero.
	subject := deal(50, 1000, 2000, "Electronics")

	for _, high := range []float64{0, -1, 500} {
		if got, want := ScoreAgainst(subject, high), Score(subject); got != want {
			t.Errorf("ScoreAgainst(high=%v) = %d, want the reported score %d", high, got, want)
		}
	}
}

func TestTrueDiscountPercent(t *testing.T) {
	subject := models.Deal{Price: 750}

	if pct, ok := subject.TrueDiscountPercent(1000); !ok || pct != 25 {
		t.Errorf("got %d/%v, want 25/true", pct, ok)
	}
	// A high at or below the current price is not a discount.
	if _, ok := subject.TrueDiscountPercent(750); ok {
		t.Error("an observed high equal to the price should not report a discount")
	}
	if _, ok := subject.TrueDiscountPercent(0); ok {
		t.Error("no history should not report a discount")
	}
}
