package engines

import "testing"

// Stage 41. NormalizePhone is a pure function - no DB, no settings read -
// which is exactly why the country is passed in rather than looked up inside.
// That makes the table below the whole specification of the cleaning rules,
// runnable without a database, and it is where every "but what about..."
// case found during the build is pinned down.

func TestNormalizePhone(t *testing.T) {
	cases := []struct {
		name        string
		raw         string
		defaultISO2 string
		wantNat     string
		wantCountry string
		wantValid   bool
		wantForeign bool
	}{
		// --- the cleaning the OMS actually needs ---------------------------
		{"plain 10-digit", "9876543210", "IN", "9876543210", "IN", true, false},
		{"spaces", "98765 43210", "IN", "9876543210", "IN", true, false},
		{"hyphens", "98765-43210", "IN", "9876543210", "IN", true, false},
		{"parens and spaces", "+91 (98765) 43210", "IN", "9876543210", "IN", true, false},
		{"dots", "9876.543.210", "IN", "9876543210", "IN", true, false},
		{"leading and trailing space", "  9876543210  ", "IN", "9876543210", "IN", true, false},
		{"dial code, no plus", "919876543210", "IN", "9876543210", "IN", true, false},
		{"00 exit code", "00919876543210", "IN", "9876543210", "IN", true, false},
		{"trunk prefix", "09876543210", "IN", "9876543210", "IN", true, false},
		{"tel scheme punctuation", "+91-98765-43210", "IN", "9876543210", "IN", true, false},

		// --- foreign numbers: accepted, cleaned, and tagged ----------------
		{"US number into an IN tenant", "+1 212 555 1234", "IN", "2125551234", "US", true, true},
		{"UK number with trunk 0 after code", "+44 7400 123456", "IN", "7400123456", "GB", true, true},
		{"UAE number", "+971 50 123 4567", "IN", "501234567", "AE", true, true},
		{"Singapore number", "0065 8123 4567", "IN", "81234567", "SG", true, true},
		// The home country is not special-cased: an Indian number reaching a
		// UAE-configured tenant is the same "foreign" case in reverse.
		{"IN number into a UAE tenant", "+91 98765 43210", "AE", "9876543210", "IN", true, true},

		// --- rejected shapes: cleaned as far as possible, flagged invalid ---
		{"too short", "98765", "IN", "98765", "IN", false, false},
		{"too long", "98765432109", "IN", "98765432109", "IN", false, false},
		{"letters only", "not a number", "IN", "", "IN", false, false},

		// --- edge cases -----------------------------------------------------
		{"empty", "", "IN", "", "", false, false},
		{"whitespace only", "   ", "IN", "", "", false, false},
		// An unknown default country falls back to India rather than erroring,
		// so a corrupted setting can never break a save path.
		{"unknown default country", "9876543210", "ZZ", "9876543210", "IN", true, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := NormalizePhone(c.raw, c.defaultISO2)
			if got.National != c.wantNat {
				t.Errorf("National = %q, want %q", got.National, c.wantNat)
			}
			if got.CountryISO2 != c.wantCountry {
				t.Errorf("CountryISO2 = %q, want %q", got.CountryISO2, c.wantCountry)
			}
			if got.Valid != c.wantValid {
				t.Errorf("Valid = %v, want %v (reason: %s)", got.Valid, c.wantValid, got.Reason)
			}
			if got.Foreign != c.wantForeign {
				t.Errorf("Foreign = %v, want %v", got.Foreign, c.wantForeign)
			}
			// An invalid result must still explain itself - a rejection with no
			// reason is what the old India-only regexp produced, and it is why
			// a correctly-formatted number written with spaces used to be
			// refused with nothing the user could act on.
			if !got.Valid && got.Raw != "" && got.Reason == "" {
				t.Errorf("invalid result carries no Reason")
			}
		})
	}
}

// The two guards inside NormalizePhone that stop a valid number being eaten.
// Both were real risks: a national number that legitimately begins with its
// own country's dial code, and one that begins with the trunk digit.
func TestNormalizePhoneDoesNotEatValidDigits(t *testing.T) {
	// "9198765432" is a real 10-digit Indian number that happens to start with
	// "91". Stripping the dial code would leave 8 digits and break it.
	got := NormalizePhone("9198765432", "IN")
	if got.National != "9198765432" || !got.Valid {
		t.Errorf("dial-code-shaped prefix was stripped: got %q valid=%v", got.National, got.Valid)
	}

	// China has no trunk prefix and an 11-digit national number; nothing may
	// be removed from it.
	cn := NormalizePhone("13812345678", "CN")
	if cn.National != "13812345678" || !cn.Valid {
		t.Errorf("CN number altered: got %q valid=%v", cn.National, cn.Valid)
	}
}

func TestNormalizePhoneE164(t *testing.T) {
	got := NormalizePhone("98765 43210", "IN")
	if got.E164 != "+919876543210" {
		t.Errorf("E164 = %q, want +919876543210", got.E164)
	}
}

// IsPhoneField drives which document fields get cleaned at all, so a miss here
// means a field silently keeps its raw channel formatting forever.
func TestIsPhoneField(t *testing.T) {
	for _, f := range []string{"phone", "contact_phone", "mobile", "mobile_number", "whatsapp_number", "telephone", "alt_contact_no"} {
		if !IsPhoneField(f) {
			t.Errorf("IsPhoneField(%q) = false, want true", f)
		}
	}
	// Deliberate non-matches: a fax is not reachable by SMS and is not a
	// targeting signal; phone_country is this engine's OWN derived companion
	// and holds "US", not a number - treating it as a phone made the server
	// reject a value it had just written itself.
	for _, f := range []string{"fax", "email", "address", "name", "code", "pincode", "phone_country", "mobile_country"} {
		if IsPhoneField(f) {
			t.Errorf("IsPhoneField(%q) = true, want false", f)
		}
	}
}

// NormalizeDocumentPhones is the choke-point call ValidateDocument makes. Its
// contract is: mutate in place, only phone fields, never reject.
func TestNormalizeDocumentPhones(t *testing.T) {
	payload := map[string]interface{}{
		"name":          "Test Customer",
		"phone":         "+91 (98765) 43210",
		"contact_phone": "098765 43211",
		"email":         "a@b.c",
		"fax":           "022 1234 5678",
		"nonsense":      "hello world",
	}
	// The declared field list includes phone_country but NOT
	// contact_phone_country, which is the opt-in: only the former gets a
	// country stamped.
	fields := []string{"name", "phone", "phone_country", "contact_phone", "email", "fax"}
	found := NormalizeDocumentPhones("", fields, payload)

	if payload["phone"] != "9876543210" {
		t.Errorf("phone = %v, want 9876543210", payload["phone"])
	}
	if payload["contact_phone"] != "9876543211" {
		t.Errorf("contact_phone = %v, want 9876543211", payload["contact_phone"])
	}
	if payload["phone_country"] != "IN" {
		t.Errorf("phone_country = %v, want IN", payload["phone_country"])
	}
	if _, exists := payload["contact_phone_country"]; exists {
		t.Errorf("contact_phone_country was written despite not being a declared field")
	}
	// Untouched fields must be byte-identical - this runs on every document
	// save in the product, so collateral damage here would be everywhere.
	if payload["fax"] != "022 1234 5678" || payload["email"] != "a@b.c" || payload["name"] != "Test Customer" {
		t.Errorf("a non-phone field was modified: %+v", payload)
	}
	if len(found) != 2 {
		t.Errorf("found %d phone fields, want 2", len(found))
	}
}

// The bug this pins down, found in live verification: a "+1 212 555 1234"
// saved on an India-configured tenant was stored as a plain 10-digit number
// and then tagged IN, because the second validation pass re-derived the
// country from digits the first pass had already stripped the "+1" from. The
// country must be captured in the pass that can still see it.
func TestNormalizeDocumentPhonesKeepsForeignCountry(t *testing.T) {
	payload := map[string]interface{}{"phone": "+1 (212) 555-1234"}
	NormalizeDocumentPhones("", []string{"phone", "phone_country"}, payload)
	if payload["phone"] != "2125551234" {
		t.Errorf("phone = %v, want 2125551234", payload["phone"])
	}
	if payload["phone_country"] != "US" {
		t.Errorf("phone_country = %v, want US - a foreign number was retagged as domestic", payload["phone_country"])
	}
}

func TestCountryPhoneRuleLabels(t *testing.T) {
	if got := PhoneRuleFor("IN").LengthLabel(); got != "10 digits" {
		t.Errorf("IN LengthLabel = %q, want %q", got, "10 digits")
	}
	if got := PhoneRuleFor("MY").LengthLabel(); got != "9 or 10 digits" {
		t.Errorf("MY LengthLabel = %q, want %q", got, "9 or 10 digits")
	}
	if got := PhoneRuleFor("ID").LengthLabel(); got != "9, 10 or 11 digits" {
		t.Errorf("ID LengthLabel = %q, want %q", got, "9, 10 or 11 digits")
	}
	if got := PhoneRuleFor("IN").MaxLength(); got != 10 {
		t.Errorf("IN MaxLength = %d, want 10", got)
	}
}

// The setting's option list is generated from this table, so a country with a
// missing/duplicated code would render a broken dropdown - and, worse, could
// make resolveByDialCode pick the wrong country for a foreign order.
func TestPhoneCountryTableIsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range phoneCountryRules {
		if r.ISO2 == "" || len(r.ISO2) != 2 {
			t.Errorf("bad ISO2 %q", r.ISO2)
		}
		if seen[r.ISO2] {
			t.Errorf("duplicate ISO2 %q", r.ISO2)
		}
		seen[r.ISO2] = true
		if r.DialCode == "" || len(r.DialCode) > 3 {
			t.Errorf("%s: bad dial code %q", r.ISO2, r.DialCode)
		}
		if len(r.Lengths) == 0 {
			t.Errorf("%s: no accepted lengths", r.ISO2)
		}
		// The example must satisfy the country's own rule, or the validation
		// message tells the user to copy a shape the validator would reject.
		if !r.accepts(len(r.Example)) {
			t.Errorf("%s: example %q (%d digits) does not match its own lengths %v", r.ISO2, r.Example, len(r.Example), r.Lengths)
		}
	}
	if !seen[DefaultPhoneCountry] {
		t.Errorf("DefaultPhoneCountry %q is not in the table", DefaultPhoneCountry)
	}
}
