package engines

import (
	"regexp"
	"strings"
)

// Tax registration numbers per country (Stage 76.4).
//
// The product calls the field "gstin" everywhere (Vendor, Legal Entity, ...),
// and until this stage it checked every value against India's 15-character
// GSTIN shape whatever country the business was in - while also accepting an
// Indian GSTIN whose check digit was wrong. A field is now checked against
// the tenant's home country's own rule (Localization -> Home country): India
// gets the GSTIN shape AND its check digit, the other countries the product
// can be set up for get their registration number's shape, and a country
// with no rule here still refuses spaces, punctuation and silly lengths.
//
// Only rules that are certain are listed. A wrong pattern would refuse a real
// supplier's real number, which is worse than the generic rule.

type taxIDRule struct {
	Label        string
	Pattern      *regexp.Regexp
	Hint         string
	Placeholder  string
	AllowedChars string
	MaxLen       int
	// Checksum names a check-digit algorithm the frontend also runs
	// ("gstin", "abn"); Check is its server-side implementation.
	Checksum string
	Check    func(string) bool
}

// gstinChars is the GSTIN character set in code-point order.
const gstinChars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"

// ValidGSTINChecksum reports whether a GSTIN's 15th character is the check
// digit of the first 14 - the Luhn mod 36 scheme GSTN issues them with. Only
// meaningful on a value that already matches the GSTIN shape.
func ValidGSTINChecksum(g string) bool {
	g = strings.ToUpper(strings.TrimSpace(g))
	if len(g) != 15 {
		return false
	}
	total, factor := 0, 1
	for i := 0; i < 14; i++ {
		v := strings.IndexByte(gstinChars, g[i])
		if v < 0 {
			return false
		}
		d := v * factor
		total += d/36 + d%36
		if factor == 1 {
			factor = 2
		} else {
			factor = 1
		}
	}
	return gstinChars[(36-total%36)%36] == g[14]
}

// validABN checks an Australian Business Number: subtract 1 from the first
// digit, weight the eleven digits, and the sum must divide by 89.
func validABN(v string) bool {
	if len(v) != 11 {
		return false
	}
	weights := []int{10, 1, 3, 5, 7, 9, 11, 13, 15, 17, 19}
	sum := 0
	for i := 0; i < 11; i++ {
		d := int(v[i] - '0')
		if d < 0 || d > 9 {
			return false
		}
		if i == 0 {
			d--
		}
		sum += d * weights[i]
	}
	return sum%89 == 0
}

func vatRule(label, pattern, example string, maxLen int) taxIDRule {
	return taxIDRule{
		Label:        label,
		Pattern:      regexp.MustCompile(pattern),
		Hint:         "Letters and digits only, no spaces. Example: " + example,
		Placeholder:  example,
		AllowedChars: "0-9A-Za-z",
		MaxLen:       maxLen,
	}
}

// taxIDRules is keyed by home country (ISO2). India is not listed: its rule
// is the "gstin" entry in fieldFormats, which this file only adds the check
// digit to, so the format the product has always shown stays the one served.
var taxIDRules = map[string]taxIDRule{
	"AE": vatRule("TRN", `^[0-9]{15}$`, "100123456700003", 15),
	"SA": vatRule("VAT number", `^3[0-9]{13}3$`, "310122393500003", 15),
	"GB": vatRule("VAT number", `^(GB)?([0-9]{9}|[0-9]{12}|GD[0-9]{3}|HA[0-9]{3})$`, "GB123456789", 14),
	"AU": func() taxIDRule {
		r := vatRule("ABN", `^[0-9]{11}$`, "51824753556", 11)
		r.AllowedChars, r.Checksum, r.Check = "0-9", "abn", validABN
		r.Hint = "11 digits. Example: 51824753556"
		return r
	}(),
	"NZ": vatRule("GST number", `^[0-9]{8,9}$`, "123456789", 9),
	"US": func() taxIDRule {
		r := vatRule("EIN", `^[0-9]{9}$`, "123456789", 9)
		r.AllowedChars, r.Hint = "0-9", "9 digits. Example: 123456789"
		return r
	}(),
	"CA": vatRule("GST/HST number", `^[0-9]{9}(RT[0-9]{4})?$`, "123456789RT0001", 15),
	"CH": vatRule("UID / VAT number", `^CHE[0-9]{9}(MWST|TVA|IVA)?$`, "CHE123456789MWST", 16),
	"NO": vatRule("VAT number", `^(NO)?[0-9]{9}(MVA)?$`, "NO123456789MVA", 14),
	"AT": vatRule("VAT number", `^(AT)?U[0-9]{8}$`, "ATU12345678", 11),
	"BE": vatRule("VAT number", `^(BE)?[01][0-9]{9}$`, "BE0123456789", 12),
	"DE": vatRule("VAT number", `^(DE)?[0-9]{9}$`, "DE123456789", 11),
	"DK": vatRule("VAT number", `^(DK)?[0-9]{8}$`, "DK12345678", 10),
	"ES": vatRule("VAT number", `^(ES)?[0-9A-Z][0-9]{7}[0-9A-Z]$`, "ESB12345678", 11),
	"FI": vatRule("VAT number", `^(FI)?[0-9]{8}$`, "FI12345678", 10),
	"FR": vatRule("VAT number", `^(FR)?[0-9A-HJ-NP-Z]{2}[0-9]{9}$`, "FR12345678901", 13),
	"IE": vatRule("VAT number", `^(IE)?([0-9][0-9A-Z][0-9]{5}[A-Z]|[0-9]{7}[A-W][A-I]?)$`, "IE1234567T", 11),
	"IT": vatRule("VAT number", `^(IT)?[0-9]{11}$`, "IT12345678901", 13),
	"NL": vatRule("VAT number", `^(NL)?[0-9]{9}B[0-9]{2}$`, "NL123456789B01", 14),
	"PL": vatRule("VAT number", `^(PL)?[0-9]{10}$`, "PL1234567890", 12),
	"PT": vatRule("VAT number", `^(PT)?[0-9]{9}$`, "PT123456789", 11),
	"SE": vatRule("VAT number", `^(SE)?[0-9]{10}01$`, "SE123456789001", 14),
}

// genericTaxIDRule covers a home country with no rule above: still letters
// and digits only, at a length no registration number falls outside.
var genericTaxIDRule = taxIDRule{
	Label:        "tax registration number",
	Pattern:      regexp.MustCompile(`^[0-9A-Z]{5,20}$`),
	Hint:         "5 to 20 letters and digits, no spaces.",
	AllowedChars: "0-9A-Za-z",
	MaxLen:       20,
}

// TaxIDFormat returns the format the tax-registration field family ("gstin")
// takes for a home country. India returns the shipped GSTIN format, which
// carries the check digit.
func TaxIDFormat(country string) FieldFormat {
	base := fieldFormats[0] // "gstin" - asserted by TestTaxIDFormatsPerCountry
	c := strings.ToUpper(strings.TrimSpace(country))
	if c == "" || c == "IN" {
		return base
	}
	r, ok := taxIDRules[c]
	if !ok {
		r = genericTaxIDRule
	}
	base.Label = r.Label
	base.Pattern = r.Pattern
	base.Hint = r.Hint
	base.Placeholder = r.Placeholder
	base.AllowedChars = r.AllowedChars
	base.MaxLen = r.MaxLen
	base.Checksum = r.Checksum
	base.Check = r.Check
	// The GSTIN's own catalog code names a GSTIN; elsewhere the generic 422
	// with the message above is the honest answer.
	base.ErrorCode = ""
	return base
}

// formatForCountry swaps the tax-ID entry for the home country's own; every
// other format is the same in every country.
func formatForCountry(spec FieldFormat, country string) FieldFormat {
	if spec.Key == "gstin" {
		return TaxIDFormat(country)
	}
	return spec
}
