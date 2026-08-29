package deals

import (
	"sort"
	"strings"
	"time"

	"post-gen/internal/models"
)

// DefaultPriceWindow is how far back an observed price is trusted as a
// reference: long enough to catch the price before a sale started, short enough
// that a year-old figure does not make a normal price look like a bargain.
const DefaultPriceWindow = 60 * 24 * time.Hour

// Thresholds for what happens to a scored deal.
const (
	// AutoQueueThreshold and above goes straight into the publishing queue.
	AutoQueueThreshold = 70
	// ReviewThreshold and above waits for an operator to approve it.
	ReviewThreshold = 50
)

// Decision is what a score means for a deal.
type Decision string

const (
	// DecisionQueue means queue it without asking.
	DecisionQueue Decision = "queue"
	// DecisionReview means show it to an operator.
	DecisionReview Decision = "review"
	// DecisionIgnore means it is not worth posting.
	DecisionIgnore Decision = "ignore"
)

// tier is one band of a scoring term: at or above Min, award Points.
type tier struct {
	Min    float64
	Points int
}

// discountTiers score the headline percentage off.
//
// This is the term most exposed to inflated list prices - Amazon's savingBasis
// is frequently optimistic - so a high discount score alone is weaker evidence
// than the savings term beside it. Price history in a later sprint is what
// actually fixes that.
var discountTiers = []tier{
	{Min: 70, Points: 70},
	{Min: 50, Points: 50},
	{Min: 30, Points: 30},
	{Min: 20, Points: 20},
	{Min: 10, Points: 10},
}

// savingsTiers score the absolute amount off, in rupees. A 60% discount on a
// ₹200 phone case is worth less attention than 25% off a ₹20,000 television.
var savingsTiers = []tier{
	{Min: 2000, Points: 30},
	{Min: 1000, Points: 20},
	{Min: 500, Points: 10},
	{Min: 100, Points: 5},
}

// categoryScores weight categories by how well they convert. An unlisted
// category scores zero rather than a default, so adding a category to discovery
// is a deliberate act of also scoring it.
var categoryScores = map[string]int{
	"electronics": 25,
	"home":        20,
	"kitchen":     20,
	"fashion":     15,
	"books":       5,
}

// Score rates a deal out of a possible 125.
//
// Ratings and review counts are deliberately not used: they are not available
// on the discovery path without a second lookup per product, and they measure
// the product rather than the offer.
func Score(deal models.Deal) int {
	return ScoreAgainst(deal, 0)
}

// ScoreAgainst rates a deal using an observed high price where one is known.
//
// observedHigh is the highest price the product was actually seen at, from
// deal_price_history. When it is available the discount term is measured
// against it instead of against Amazon's savingBasis, which is a list price
// and frequently inflated well above anything anyone paid. Pass zero when
// there is no history and the reported figure stands.
//
// This is the one term that can move a deal across the auto-queue threshold on
// its own, so it is also the one most worth grounding in observed prices.
func ScoreAgainst(deal models.Deal, observedHigh float64) int {
	discount := deal.DiscountPercent
	savings := deal.Savings()

	if trueDiscount, ok := deal.TrueDiscountPercent(observedHigh); ok {
		discount = trueDiscount
		savings = observedHigh - deal.Price
	}

	return discountScore(discount) + savingsScore(savings) + CategoryScore(deal.Category)
}

// Decide maps a score onto what should happen to the deal.
func Decide(score int) Decision {
	switch {
	case score >= AutoQueueThreshold:
		return DecisionQueue
	case score >= ReviewThreshold:
		return DecisionReview
	default:
		return DecisionIgnore
	}
}

// CategoryScore is the weight for one category, matched case-insensitively.
func CategoryScore(category string) int {
	return categoryScores[strings.ToLower(strings.TrimSpace(category))]
}

// ScoredCategories lists the categories that carry a weight, for the settings
// UI and for checking that a newly added discovery category is also scored.
func ScoredCategories() []string {
	names := make([]string, 0, len(categoryScores))
	for name := range categoryScores {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func discountScore(percent int) int { return scoreTiers(float64(percent), discountTiers) }

func savingsScore(amount float64) int { return scoreTiers(amount, savingsTiers) }

// scoreTiers awards the points of the highest tier the value clears. Tiers are
// declared highest-first so the first match wins.
func scoreTiers(value float64, tiers []tier) int {
	for _, t := range tiers {
		if value >= t.Min {
			return t.Points
		}
	}
	return 0
}
