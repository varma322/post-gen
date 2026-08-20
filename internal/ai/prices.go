package ai

import (
	"regexp"
	"strings"
)

// Prices, the M.R.P. and the discount are scraped facts. They are excluded
// from generatableFields precisely so a model can never restate one, but a 7B
// model reaches them anyway through the fields it can write: observed live as
// the title "Genius 66cm Spinner Trolley - Black, Only ₹1,999.00" and the
// bullet "56% Off - Get it for just ₹1,999.00 now", both against a layout that
// already prints an M.R.P. and savings block of its own.
//
// The prompt asks for this too. As with feature bullet prefixes, only stripping
// after the fact guarantees it - and a stale price beside an affiliate link is
// worth more than a prompt's word.
var (
	// moneyRe matches a rupee amount however the model spells it: "₹1,999.00",
	// "Rs. 1999", "INR 1,999", "1999 rupees".
	moneyRe = regexp.MustCompile(`(?i)(₹|\brs\.?\s*|\binr\s*)\s*\d[\d,]*(\.\d+)?|\b\d[\d,]*(\.\d+)?\s*rupees?\b`)

	// discountRe matches a stated percentage saving, e.g. "56% OFF",
	// "56 % discount", "save 56%".
	discountRe = regexp.MustCompile(`(?i)\b(save\s+)?\d{1,3}\s*%(\s*(off|discount|savings?))?`)

	// danglingRe collects the separators left behind once a clause is removed,
	// so "Black, Only  now" does not keep its orphaned punctuation.
	danglingRe = regexp.MustCompile(`\s*[-–—,;:|]\s*([-–—,;:|]\s*)+`)
	spacesRe   = regexp.MustCompile(`\s{2,}`)

	// orphanPunctRe closes the gap a removed clause leaves in front of its
	// own punctuation: "Massive 56% OFF!" would otherwise strip to
	// "Massive !".
	orphanPunctRe = regexp.MustCompile(`\s+([!?.,;:])`)
)

// mentionsPrice reports whether text states a price or a discount percentage.
func mentionsPrice(text string) bool {
	return moneyRe.MatchString(text) || discountRe.MatchString(text)
}

// stripPriceMentions removes price and discount claims from one generated
// field and tidies the punctuation left behind. It is used for the single-value
// fields, where dropping the whole string would blank the post's headline;
// feature bullets are dropped outright instead, since there are several and a
// bullet reduced to a fragment reads worse than one fewer bullet.
func stripPriceMentions(text string) string {
	if !mentionsPrice(text) {
		return text
	}

	cleaned := moneyRe.ReplaceAllString(text, "")
	cleaned = discountRe.ReplaceAllString(cleaned, "")
	cleaned = danglingRe.ReplaceAllString(cleaned, " - ")
	cleaned = spacesRe.ReplaceAllString(cleaned, " ")
	cleaned = orphanPunctRe.ReplaceAllString(cleaned, "$1")
	cleaned = strings.TrimSpace(cleaned)
	cleaned = strings.Trim(cleaned, "-–—,;:|@ ")
	cleaned = strings.TrimSpace(cleaned)

	// Only punctuation and filler words survived - the field was nothing but a
	// price claim. Returning the original keeps the post rendering rather than
	// leaving a blank line, and applyTo's "" check would have kept the scraped
	// value anyway.
	if len([]rune(cleaned)) < 3 {
		return text
	}

	return cleaned
}
