package engines

import "testing"

// Stage 76 D2: India holds a document number to GST's 16 characters and its
// character rule; another home country gets a product cap of 20 and no GST
// character rule.
func TestSeriesLengthCapFollowsHomeCountry(t *testing.T) {
	series := func(prefix string, digits int) NumberSeries {
		return NumberSeries{DocType: "PurchaseOrder", Prefix: prefix, Separator: "/", PaddingWidth: digits, ResetFrequency: "ANNUAL", ActiveStatus: true}
	}
	cases := []struct {
		name    string
		s       NumberSeries
		country []string
		ok      bool
	}{
		{"19 characters in India", series("INVOICE", 5), nil, false},
		{"19 characters in India, named", series("INVOICE", 5), []string{"IN"}, false},
		{"19 characters in the UAE", series("INVOICE", 5), []string{"AE"}, true},
		{"21 characters in the UAE", series("INVOICES", 6), []string{"AE"}, false},
		{"GST's character rule in India", series("P_O", 5), []string{"IN"}, false},
		{"no GST character rule in the UAE", series("P_O", 5), []string{"AE"}, true},
		{"16 characters in India", series("PO", 5), nil, true},
	}
	for _, c := range cases {
		err := validateNumberSeries(c.s, nil, nil, nil, nil, c.country...)
		if (err == nil) != c.ok {
			t.Errorf("%s: ok=%v, got err %v", c.name, c.ok, err)
		}
	}
}
