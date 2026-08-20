package generator

import "strings"

// FormatMoney normalises a scraped price for display in a post.
//
// Prices reach the templates from two sources that disagree on formatting:
// the Creator API returns Amazon's DisplayAmount, which keeps a zero paise
// fraction ("1,100.00"), while the HTML scraper usually yields the bare rupee
// figure ("1,079"). Rendered side by side in an M.R.P./offer block, that reads
// as a formatting bug - "₹1,079" struck through against "₹1,100.00".
//
// Only an all-zero fraction is dropped; a genuine paise value is preserved,
// because silently rounding a price shown next to an affiliate link is worse
// than an untidy one.
func FormatMoney(price string) string {
	price = strings.TrimSpace(price)
	price = strings.TrimPrefix(price, "₹")
	price = strings.TrimSpace(price)

	if dot := strings.LastIndex(price, "."); dot != -1 {
		fraction := price[dot+1:]
		if fraction == "" || strings.Trim(fraction, "0") == "" {
			price = price[:dot]
		}
	}

	return strings.TrimSuffix(price, ".")
}
