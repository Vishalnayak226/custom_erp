package engines

import "testing"

// Stage 40.1. These cover the pure arithmetic and string logic added for PO
// lines - no DB fixture needed, so they run in any environment and are the
// fastest place to catch a rounding or grouping regression. The DB-backed
// halves (PreviewPurchaseOrder's Item resolution, ResolvePlaceOfSupply's
// Location->LegalEntity walk) are exercised by the existing gst_test.go
// fixtures, which already provision a tenant.

func TestStateCodeFromGSTIN(t *testing.T) {
	cases := []struct {
		name, gstin, want string
	}{
		{"Maharashtra", "27AAPFU0939F1ZV", "27"},
		{"Karnataka", "29AABCU9603R1ZM", "29"},
		{"lower case and padding are tolerated", "  07aabcu9603r1zm ", "07"},
		{"Ladakh, the highest issued code", "38AABCU9603R1ZM", "38"},
		{"Other Territory", "97AABCU9603R1ZM", "97"},
		{"a code the GSTN does not issue", "45AABCU9603R1ZM", ""},
		{"zero is not a state", "00AABCU9603R1ZM", ""},
		{"empty", "", ""},
		{"too short to carry a code", "2", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := StateCodeFromGSTIN(c.gstin); got != c.want {
				t.Errorf("StateCodeFromGSTIN(%q) = %q, want %q", c.gstin, got, c.want)
			}
		})
	}
}

func TestStateCodeFromName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Maharashtra", "27"},
		{"maharashtra", "27"},
		{"TAMIL NADU", "33"},
		{"tamil-nadu", "33"},
		{"Orissa", "21"}, // the older spelling still in use on masters
		{"Odisha", "21"}, // and the current one
		{"27", "27"},     // a bare code passes through
		{"Atlantis", ""}, // nothing plausible
		{"", ""},
	}
	for _, c := range cases {
		if got := StateCodeFromName(c.in); got != c.want {
			t.Errorf("StateCodeFromName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPlaceOfSupplySummaryOnlyWhenDerived(t *testing.T) {
	// An underived place of supply must produce an empty summary rather than a
	// confident-looking "Intra-state: vendor  -> entity ", which is what a
	// zero-value struct would print.
	if got := (PlaceOfSupply{}).Summary(); got != "" {
		t.Errorf("undetermined place of supply summarised as %q, want empty", got)
	}
	inter := PlaceOfSupply{BuyerStateCode: "27", VendorStateCode: "29", Interstate: true, Derived: true}
	if got := inter.Summary(); got != "Inter-state: vendor 29 -> entity 27" {
		t.Errorf("interstate summary = %q", got)
	}
	intra := PlaceOfSupply{BuyerStateCode: "27", VendorStateCode: "27", Derived: true}
	if got := intra.Summary(); got != "Intra-state: vendor 27 -> entity 27" {
		t.Errorf("intrastate summary = %q", got)
	}
}

func TestComputeGSTForLinesModeExclusiveAddsTaxOnTop(t *testing.T) {
	// The whole point of Stage 40.1's mode split: at 18% on 1000, inclusive
	// treatment yields a taxable value of 847.46 while exclusive yields 1000.
	// This asserts the arithmetic directly through CalculateGST rather than
	// through the DB-backed line loop, so it needs no Item fixture.
	inclusiveTaxable := 1000.0 / (1 + 18.0/100)
	inc, err := CalculateGST(round2(inclusiveTaxable), 18, false)
	if err != nil {
		t.Fatalf("inclusive: %v", err)
	}
	if inc.TotalAmount != 1000 {
		t.Errorf("inclusive total = %.2f, want the original 1000 back", inc.TotalAmount)
	}

	exc, err := CalculateGST(1000, 18, false)
	if err != nil {
		t.Fatalf("exclusive: %v", err)
	}
	if exc.TaxableAmount != 1000 || exc.TotalAmount != 1180 {
		t.Errorf("exclusive = taxable %.2f / total %.2f, want 1000 / 1180", exc.TaxableAmount, exc.TotalAmount)
	}
	if exc.CGST != 90 || exc.SGST != 90 || exc.IGST != 0 {
		t.Errorf("intra-state split = CGST %.2f / SGST %.2f / IGST %.2f, want 90/90/0", exc.CGST, exc.SGST, exc.IGST)
	}
}

func TestAmountInWords(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "Rupees Zero Only"},
		{1, "Rupees One Only"},
		{19, "Rupees Nineteen Only"},
		{20, "Rupees Twenty Only"},
		{99, "Rupees Ninety Nine Only"},
		{100, "Rupees One Hundred Only"},
		{101, "Rupees One Hundred One Only"},
		{1000, "Rupees One Thousand Only"},
		{100000, "Rupees One Lakh Only"},
		{10000000, "Rupees One Crore Only"},
		{1234567, "Rupees Twelve Lakh Thirty Four Thousand Five Hundred Sixty Seven Only"},
		{1180.50, "Rupees One Thousand One Hundred Eighty and Fifty Paise Only"},
		// Rounds to the paise before splitting, so the words cannot disagree
		// with a figures column that shows 1234.57.
		{1234.565, "Rupees One Thousand Two Hundred Thirty Four and Fifty Seven Paise Only"},
		{-500, "Minus Rupees Five Hundred Only"},
	}
	for _, c := range cases {
		if got := AmountInWords(c.in); got != c.want {
			t.Errorf("AmountInWords(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatIndianCurrency(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "0.00"},
		{999, "999.00"},
		{1000, "1,000.00"},
		{99999, "99,999.00"},
		{100000, "1,00,000.00"},
		{1234567.89, "12,34,567.89"},
		{10000000, "1,00,00,000.00"},
		{-1234567.89, "-12,34,567.89"},
	}
	for _, c := range cases {
		if got := FormatIndianCurrency(c.in); got != c.want {
			t.Errorf("FormatIndianCurrency(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParsePOLines(t *testing.T) {
	if lines, err := ParsePOLines(""); err != nil || lines != nil {
		t.Errorf("empty items = %v, %v; want nil, nil (a Draft PO starts with no lines)", lines, err)
	}
	if lines, err := ParsePOLines("   "); err != nil || lines != nil {
		t.Errorf("whitespace items = %v, %v; want nil, nil", lines, err)
	}
	if _, err := ParsePOLines("not json"); err == nil {
		t.Error("malformed items JSON parsed without error")
	}
	lines, err := ParsePOLines(`[{"sku":"SKU-1","qty":3,"rate":450.5,"mrp":699}]`)
	if err != nil {
		t.Fatalf("valid items: %v", err)
	}
	if len(lines) != 1 || lines[0].SKU != "SKU-1" || lines[0].Qty != 3 || lines[0].Rate != 450.5 || lines[0].MRP != 699 {
		t.Errorf("parsed line = %+v", lines[0])
	}
}

func TestTruthyReadsCheckFieldShapes(t *testing.T) {
	// A Check field arrives as a real bool from a JSON body but as a string
	// from a CSV import or a form post; before Stage 40.1 a string here meant
	// intra-state regardless of what it said.
	for _, v := range []interface{}{true, "true", "TRUE", " on ", "1", "yes", 1.0} {
		if !truthy(v) {
			t.Errorf("truthy(%#v) = false, want true", v)
		}
	}
	for _, v := range []interface{}{false, "false", "", "0", "no", 0.0, nil} {
		if truthy(v) {
			t.Errorf("truthy(%#v) = true, want false", v)
		}
	}
}
