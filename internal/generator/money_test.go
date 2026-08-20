package generator

import "testing"

func TestFormatMoney(t *testing.T) {
	cases := []struct{ in, want string }{
		// Creator API DisplayAmount keeps a zero paise fraction; the HTML
		// scraper does not. Both must render the same way.
		{"1,100.00", "1,100"},
		{"1,079", "1,079"},
		{"12,995.00", "12,995"},
		{"899.0", "899"},
		{"4,590.", "4,590"},
		// A real paise value is data, not formatting - never round it away.
		{"1,100.50", "1,100.50"},
		{"99.99", "99.99"},
		// Tolerate a stray symbol or padding without mangling the number.
		{"₹7,775", "7,775"},
		{" 598 ", "598"},
		{"", ""},
	}

	for _, tc := range cases {
		if got := FormatMoney(tc.in); got != tc.want {
			t.Errorf("FormatMoney(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
