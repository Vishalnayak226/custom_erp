package engines

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Stage 41: country-aware phone normalization.
//
// Before this, the only phone rule in the codebase was one hardcoded Indian
// mobile regexp in master_data_validation.go (`^[6-9][0-9]{9}$`), applied to
// the Customer master and nowhere else. That had three problems the business
// actually hits:
//
//  1. Nothing was ever *cleaned*. A number arriving from a channel as
//     "+91 (98765) 43210" or "98765-43210" was rejected outright on a master,
//     and stored verbatim everywhere else - so the same customer appeared
//     under three spellings and duplicate detection (CUSTOM-0133, which
//     compares data->>'phone' as a literal string) silently missed them.
//  2. The rule was India-only, in a product sold as multi-country.
//  3. An order legitimately placed from another country had nowhere to record
//     that fact, which is exactly the signal a marketing team targets on.
//
// The design here separates the two concerns that got conflated:
//
//   - CLEANING is unconditional and lossless-enough to be safe everywhere:
//     strip formatting, resolve an international prefix, store one canonical
//     spelling. This never rejects anything.
//   - VALIDATION is a policy the caller applies to the cleaned result. Master
//     data (a Customer a human is typing) is held to the tenant's configured
//     country. An order arriving from a channel is not - it is cleaned, its
//     origin country is recorded, and it is saved either way, because refusing
//     to accept a real order because of its phone number would be the wrong
//     trade every time.
//
// No dependency: a full libphonenumber port is thousands of rules and a
// monthly metadata refresh, which this repo's "no new dependencies" principle
// rules out and the use case does not need. What is needed is dial code,
// trunk prefix and national length per country, which is a table.

// CountryPhoneRule is one country's phone shape.
type CountryPhoneRule struct {
	ISO2 string `json:"iso2"`
	Name string `json:"name"`
	// DialCode is the international calling code without the leading '+'.
	DialCode string `json:"dial_code"`
	// Lengths are the accepted national-significant-number lengths, i.e. what
	// remains after the dial code and any trunk prefix are removed. For India
	// this is the single value 10 - the "phone must be 10 digits" rule.
	Lengths []int `json:"lengths"`
	// TrunkPrefix is the domestic long-distance digit that is dropped when a
	// number is written internationally ("0" across most of Europe and Asia,
	// "1" in the NANP, "8" in Russia). Empty where the country has none.
	TrunkPrefix string `json:"trunk_prefix"`
	// Example is a correctly formatted national number, shown in UI hints and
	// validation messages so the user is told the shape, not just "invalid".
	Example string `json:"example"`
	// Primary breaks a shared-dial-code tie. Several countries answer to the
	// same code (US and CA both on +1) and, without an area-code table, a
	// bare "+1 212 555 1234" is genuinely ambiguous. Rather than let the
	// answer fall out of the table's ordering - where inserting a country
	// would silently change how existing numbers are tagged - the owner of
	// each shared code is stated. The tenant's own country still wins over
	// this when it is one of the candidates.
	Primary bool `json:"primary,omitempty"`
}

// MaxLength is the longest national number this country accepts - what a UI
// sets an input's maxlength to.
func (r CountryPhoneRule) MaxLength() int {
	m := 0
	for _, l := range r.Lengths {
		if l > m {
			m = l
		}
	}
	return m
}

// LengthLabel renders the accepted lengths for a human: "10 digits", or
// "9 or 10 digits", or "9, 10 or 11 digits".
func (r CountryPhoneRule) LengthLabel() string {
	switch len(r.Lengths) {
	case 0:
		return "digits"
	case 1:
		return fmt.Sprintf("%d digits", r.Lengths[0])
	}
	parts := make([]string, 0, len(r.Lengths))
	for _, l := range r.Lengths {
		parts = append(parts, fmt.Sprintf("%d", l))
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " or " + parts[len(parts)-1] + " digits"
}

// MarshalJSON ships the two derived values (MaxLength, LengthLabel) alongside
// the stored ones, because the browser needs both to enforce the same rule
// this table expresses - a digit cap on the input and a sentence naming it -
// and recomputing them in JavaScript would be a second copy of the logic, in
// a second language, free to drift. Methods don't serialise; this is how they
// reach the client from the one definition that owns them.
func (r CountryPhoneRule) MarshalJSON() ([]byte, error) {
	// A named alias so the marshaller doesn't re-enter this method.
	type rule CountryPhoneRule
	return json.Marshal(struct {
		rule
		MaxLength   int    `json:"max_length"`
		LengthLabel string `json:"length_label"`
	}{rule(r), r.MaxLength(), r.LengthLabel()})
}

func (r CountryPhoneRule) accepts(n int) bool {
	for _, l := range r.Lengths {
		if l == n {
			return true
		}
	}
	return false
}

// DefaultPhoneCountry is the fallback when a tenant has not chosen one. It
// matches the behaviour this codebase already had (the India-only regexp), so
// an untouched tenant sees no change from this Stage.
const DefaultPhoneCountry = "IN"

// phoneCountryRules is the table. Ordered by ISO2 in the literal for
// readability; PhoneCountries() sorts by display name for the UI.
//
// Note US/CA (and the rest of the NANP) share dial code 1, so an incoming
// "+1..." is genuinely ambiguous. resolveByDialCode prefers the tenant's own
// configured country when it is one of the candidates, then the code's stated
// Primary owner - which is the best that can be done without an area-code
// table, and matters little in practice since the national number is
// identical either way.
var phoneCountryRules = []CountryPhoneRule{
	{ISO2: "AE", Name: "United Arab Emirates", DialCode: "971", Lengths: []int{9}, TrunkPrefix: "0", Example: "501234567"},
	{ISO2: "AR", Name: "Argentina", DialCode: "54", Lengths: []int{10}, TrunkPrefix: "0", Example: "1123456789"},
	{ISO2: "AT", Name: "Austria", DialCode: "43", Lengths: []int{10, 11}, TrunkPrefix: "0", Example: "6641234567"},
	{ISO2: "AU", Name: "Australia", DialCode: "61", Lengths: []int{9}, TrunkPrefix: "0", Example: "412345678"},
	{ISO2: "BD", Name: "Bangladesh", DialCode: "880", Lengths: []int{10}, TrunkPrefix: "0", Example: "1712345678"},
	{ISO2: "BE", Name: "Belgium", DialCode: "32", Lengths: []int{8, 9}, TrunkPrefix: "0", Example: "470123456"},
	{ISO2: "BH", Name: "Bahrain", DialCode: "973", Lengths: []int{8}, Example: "36001234"},
	{ISO2: "BR", Name: "Brazil", DialCode: "55", Lengths: []int{10, 11}, TrunkPrefix: "0", Example: "11912345678"},
	{ISO2: "CA", Name: "Canada", DialCode: "1", Lengths: []int{10}, TrunkPrefix: "1", Example: "4165551234"},
	{ISO2: "CH", Name: "Switzerland", DialCode: "41", Lengths: []int{9}, TrunkPrefix: "0", Example: "781234567"},
	{ISO2: "CL", Name: "Chile", DialCode: "56", Lengths: []int{9}, Example: "912345678"},
	{ISO2: "CN", Name: "China", DialCode: "86", Lengths: []int{11}, Example: "13812345678"},
	{ISO2: "CO", Name: "Colombia", DialCode: "57", Lengths: []int{10}, Example: "3211234567"},
	{ISO2: "DE", Name: "Germany", DialCode: "49", Lengths: []int{10, 11}, TrunkPrefix: "0", Example: "15112345678"},
	{ISO2: "DK", Name: "Denmark", DialCode: "45", Lengths: []int{8}, Example: "20123456"},
	{ISO2: "EG", Name: "Egypt", DialCode: "20", Lengths: []int{10}, TrunkPrefix: "0", Example: "1001234567"},
	{ISO2: "ES", Name: "Spain", DialCode: "34", Lengths: []int{9}, Example: "612345678"},
	{ISO2: "FI", Name: "Finland", DialCode: "358", Lengths: []int{9, 10}, TrunkPrefix: "0", Example: "451234567"},
	{ISO2: "FR", Name: "France", DialCode: "33", Lengths: []int{9}, TrunkPrefix: "0", Example: "612345678"},
	{ISO2: "GB", Name: "United Kingdom", DialCode: "44", Lengths: []int{10}, TrunkPrefix: "0", Example: "7400123456"},
	{ISO2: "HK", Name: "Hong Kong", DialCode: "852", Lengths: []int{8}, Example: "51234567"},
	{ISO2: "ID", Name: "Indonesia", DialCode: "62", Lengths: []int{9, 10, 11}, TrunkPrefix: "0", Example: "81234567890"},
	{ISO2: "IE", Name: "Ireland", DialCode: "353", Lengths: []int{9}, TrunkPrefix: "0", Example: "851234567"},
	{ISO2: "IL", Name: "Israel", DialCode: "972", Lengths: []int{9}, TrunkPrefix: "0", Example: "501234567"},
	{ISO2: "IN", Name: "India", DialCode: "91", Lengths: []int{10}, TrunkPrefix: "0", Example: "9876543210"},
	{ISO2: "IT", Name: "Italy", DialCode: "39", Lengths: []int{9, 10}, Example: "3123456789"},
	{ISO2: "JP", Name: "Japan", DialCode: "81", Lengths: []int{10}, TrunkPrefix: "0", Example: "9012345678"},
	{ISO2: "KE", Name: "Kenya", DialCode: "254", Lengths: []int{9}, TrunkPrefix: "0", Example: "712345678"},
	{ISO2: "KR", Name: "South Korea", DialCode: "82", Lengths: []int{9, 10}, TrunkPrefix: "0", Example: "1012345678"},
	{ISO2: "KW", Name: "Kuwait", DialCode: "965", Lengths: []int{8}, Example: "50123456"},
	{ISO2: "LK", Name: "Sri Lanka", DialCode: "94", Lengths: []int{9}, TrunkPrefix: "0", Example: "712345678"},
	{ISO2: "MX", Name: "Mexico", DialCode: "52", Lengths: []int{10}, Example: "5512345678"},
	{ISO2: "MY", Name: "Malaysia", DialCode: "60", Lengths: []int{9, 10}, TrunkPrefix: "0", Example: "123456789"},
	{ISO2: "NG", Name: "Nigeria", DialCode: "234", Lengths: []int{10}, TrunkPrefix: "0", Example: "8021234567"},
	{ISO2: "NL", Name: "Netherlands", DialCode: "31", Lengths: []int{9}, TrunkPrefix: "0", Example: "612345678"},
	{ISO2: "NO", Name: "Norway", DialCode: "47", Lengths: []int{8}, Example: "40612345"},
	{ISO2: "NP", Name: "Nepal", DialCode: "977", Lengths: []int{10}, Example: "9841234567"},
	{ISO2: "NZ", Name: "New Zealand", DialCode: "64", Lengths: []int{8, 9}, TrunkPrefix: "0", Example: "211234567"},
	{ISO2: "OM", Name: "Oman", DialCode: "968", Lengths: []int{8}, Example: "92123456"},
	{ISO2: "PH", Name: "Philippines", DialCode: "63", Lengths: []int{10}, TrunkPrefix: "0", Example: "9171234567"},
	{ISO2: "PK", Name: "Pakistan", DialCode: "92", Lengths: []int{10}, TrunkPrefix: "0", Example: "3012345678"},
	{ISO2: "PL", Name: "Poland", DialCode: "48", Lengths: []int{9}, Example: "512345678"},
	{ISO2: "PT", Name: "Portugal", DialCode: "351", Lengths: []int{9}, Example: "912345678"},
	{ISO2: "QA", Name: "Qatar", DialCode: "974", Lengths: []int{8}, Example: "33123456"},
	{ISO2: "RU", Name: "Russia", DialCode: "7", Lengths: []int{10}, TrunkPrefix: "8", Example: "9123456789"},
	{ISO2: "SA", Name: "Saudi Arabia", DialCode: "966", Lengths: []int{9}, TrunkPrefix: "0", Example: "512345678"},
	{ISO2: "SE", Name: "Sweden", DialCode: "46", Lengths: []int{7, 8, 9}, TrunkPrefix: "0", Example: "701234567"},
	{ISO2: "SG", Name: "Singapore", DialCode: "65", Lengths: []int{8}, Example: "81234567"},
	{ISO2: "TH", Name: "Thailand", DialCode: "66", Lengths: []int{9}, TrunkPrefix: "0", Example: "812345678"},
	{ISO2: "TR", Name: "Turkey", DialCode: "90", Lengths: []int{10}, TrunkPrefix: "0", Example: "5321234567"},
	{ISO2: "TW", Name: "Taiwan", DialCode: "886", Lengths: []int{9}, TrunkPrefix: "0", Example: "912345678"},
	{ISO2: "US", Name: "United States", DialCode: "1", Lengths: []int{10}, TrunkPrefix: "1", Example: "2125551234", Primary: true},
	{ISO2: "VN", Name: "Vietnam", DialCode: "84", Lengths: []int{9}, TrunkPrefix: "0", Example: "912345678"},
	{ISO2: "ZA", Name: "South Africa", DialCode: "27", Lengths: []int{9}, TrunkPrefix: "0", Example: "821234567"},
}

var phoneRulesByISO2 = func() map[string]CountryPhoneRule {
	m := make(map[string]CountryPhoneRule, len(phoneCountryRules))
	for _, r := range phoneCountryRules {
		m[r.ISO2] = r
	}
	return m
}()

// PhoneCountries returns every supported country, sorted by display name -
// the order the Configuration screen's country dropdown renders in.
func PhoneCountries() []CountryPhoneRule {
	out := make([]CountryPhoneRule, len(phoneCountryRules))
	copy(out, phoneCountryRules)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// PhoneRuleFor returns a country's rule, falling back to DefaultPhoneCountry
// for an unknown/empty code. Never fails - a config read must not be able to
// break a save path.
func PhoneRuleFor(iso2 string) CountryPhoneRule {
	if r, ok := phoneRulesByISO2[strings.ToUpper(strings.TrimSpace(iso2))]; ok {
		return r
	}
	return phoneRulesByISO2[DefaultPhoneCountry]
}

// PhoneNumber is the result of normalizing one raw phone string.
type PhoneNumber struct {
	// Raw is exactly what came in, kept so a rejection message can quote it.
	Raw string `json:"raw"`
	// National is the cleaned national significant number: digits only, no
	// dial code, no trunk prefix, no punctuation. This is what is stored on
	// the record and what duplicate detection compares.
	National string `json:"national"`
	// E164 is the international form ("+919876543210"). Empty when no country
	// could be resolved at all.
	E164 string `json:"e164"`
	// CountryISO2 is the country the number belongs to - the tenant's default
	// unless the number carried an explicit international prefix for another.
	CountryISO2 string `json:"country"`
	// Foreign reports CountryISO2 != the default passed in. This is the
	// targeting signal: it is what lets "orders from outside our home market"
	// be segmented without re-parsing every number later.
	Foreign bool `json:"foreign"`
	// Valid reports whether National matches CountryISO2's accepted lengths.
	// A cleaned-but-invalid number still carries its National/Country - the
	// caller decides whether to reject (masters) or keep it (channel orders).
	Valid bool `json:"valid"`
	// Reason explains an invalid result, phrased for an end user.
	Reason string `json:"reason"`
}

// Empty reports whether there was no phone number to begin with. Callers use
// it to skip an optional field rather than treating "" as an invalid number.
func (p PhoneNumber) Empty() bool { return p.Raw == "" || (p.National == "" && p.E164 == "") }

// digitsOnly strips every character that is not 0-9. This is the "clean up
// spaces and special characters" step, and it is deliberately total: spaces,
// hyphens, dots, slashes, parentheses, non-breaking spaces and the assorted
// unicode dashes channel exports are full of all disappear.
func digitsOnly(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// hasInternationalPrefix reports whether the raw string explicitly declared an
// international number, and returns the digits with that marker removed. Both
// spellings are accepted: a leading '+' (possibly after whitespace or a
// wrapping bracket) and the ISO "00" exit code.
func hasInternationalPrefix(raw, digits string) (string, bool) {
	trimmed := strings.TrimLeft(strings.TrimSpace(raw), "(<[ \t")
	if strings.HasPrefix(trimmed, "+") {
		return digits, true
	}
	if strings.HasPrefix(digits, "00") && len(digits) > 4 {
		return digits[2:], true
	}
	return digits, false
}

// resolveByDialCode finds the country whose dial code prefixes `digits`,
// longest code first (so 971/AE wins over 97, and 91/IN over 9). Among equal
// candidates the tenant's own country wins - see the NANP note on the table.
func resolveByDialCode(digits, defaultISO2 string) (CountryPhoneRule, string, bool) {
	for n := 3; n >= 1; n-- {
		if len(digits) <= n {
			continue
		}
		code := digits[:n]
		var candidates []CountryPhoneRule
		for _, r := range phoneCountryRules {
			if r.DialCode == code {
				candidates = append(candidates, r)
			}
		}
		if len(candidates) == 0 {
			continue
		}
		rest := digits[n:]
		// Preference order, applied first among candidates whose national
		// length actually fits and then among all of them:
		//   1. the tenant's own country - a UAE tenant reading "+971..."
		//      should see its own country, not a coincidental other match;
		//   2. the code's stated Primary owner;
		//   3. first in the table, so the result is at least deterministic.
		if r, ok := pickDialCodeCandidate(candidates, defaultISO2, len(rest), true); ok {
			return r, rest, true
		}
		// Dial code matched but the length did not. Still report the country -
		// a wrong-length foreign number is more useful tagged than untagged -
		// and let the caller see Valid=false.
		r, _ := pickDialCodeCandidate(candidates, defaultISO2, len(rest), false)
		return r, rest, true
	}
	return CountryPhoneRule{}, digits, false
}

// pickDialCodeCandidate applies the preference order above. When requireFit is
// true it only considers candidates whose accepted lengths match nationalLen,
// and reports false if none does.
func pickDialCodeCandidate(candidates []CountryPhoneRule, defaultISO2 string, nationalLen int, requireFit bool) (CountryPhoneRule, bool) {
	def := strings.ToUpper(defaultISO2)
	var primary, first *CountryPhoneRule
	for i := range candidates {
		c := candidates[i]
		if requireFit && !c.accepts(nationalLen) {
			continue
		}
		if c.ISO2 == def {
			return c, true
		}
		if c.Primary && primary == nil {
			primary = &candidates[i]
		}
		if first == nil {
			first = &candidates[i]
		}
	}
	if primary != nil {
		return *primary, true
	}
	if first != nil {
		return *first, true
	}
	return CountryPhoneRule{}, false
}

// NormalizePhone is the single cleaning entry point every caller goes through.
// It never returns an error: an unusable number comes back with Valid=false
// and a Reason, and it is the caller's policy whether that is fatal.
//
// defaultISO2 is the tenant's configured country (see the
// "localization.default_country" setting). A number with no international
// prefix is interpreted as belonging to it; a number carrying "+<code>" or
// "00<code>" for a different country is resolved to that country instead and
// flagged Foreign.
func NormalizePhone(raw, defaultISO2 string) PhoneNumber {
	def := PhoneRuleFor(defaultISO2)
	out := PhoneNumber{Raw: strings.TrimSpace(raw)}
	if out.Raw == "" {
		return out
	}

	digits := digitsOnly(out.Raw)
	if digits == "" {
		out.CountryISO2 = def.ISO2
		out.Reason = fmt.Sprintf("%q contains no digits", out.Raw)
		return out
	}

	body, international := hasInternationalPrefix(out.Raw, digits)

	var rule CountryPhoneRule
	var national string

	if international {
		r, rest, ok := resolveByDialCode(body, def.ISO2)
		if !ok {
			// An explicit international number whose calling code this table
			// does not carry. Keep the digits and say so rather than silently
			// mangling them into the default country.
			out.CountryISO2 = ""
			out.National = body
			out.E164 = "+" + body
			out.Reason = fmt.Sprintf("+%s is not a calling code this system recognises", body)
			return out
		}
		rule, national = r, rest
	} else {
		rule = def
		national = body
		// A number typed without '+' can still carry its own country code -
		// "919876543210" pasted out of a channel export is the common case.
		// Only stripped when doing so produces a valid national number AND
		// the number as-typed is too long to be one already, so a genuine
		// 10-digit local number starting with the dial code is never eaten.
		if !rule.accepts(len(national)) && strings.HasPrefix(national, rule.DialCode) {
			if rest := national[len(rule.DialCode):]; rule.accepts(len(rest)) {
				national = rest
			}
		}
	}

	// Trunk prefix, e.g. "09876543210" -> "9876543210". Same guard: only
	// dropped when what remains is a valid length, so a country whose national
	// numbers legitimately begin with the trunk digit is left alone.
	if rule.TrunkPrefix != "" && !rule.accepts(len(national)) && strings.HasPrefix(national, rule.TrunkPrefix) {
		if rest := national[len(rule.TrunkPrefix):]; rule.accepts(len(rest)) {
			national = rest
		}
	}

	out.CountryISO2 = rule.ISO2
	out.National = national
	out.Foreign = rule.ISO2 != def.ISO2
	if rule.DialCode != "" && national != "" {
		out.E164 = "+" + rule.DialCode + national
	}
	if rule.accepts(len(national)) {
		out.Valid = true
		return out
	}
	out.Reason = fmt.Sprintf("a %s phone number must be %s (got %d in %q, cleaned to %q) - e.g. %s",
		rule.Name, rule.LengthLabel(), len(national), out.Raw, national, rule.Example)
	return out
}

// TenantPhoneCountry returns the country code a tenant is configured for.
// Every phone-touching path reads it through here, so changing the setting
// changes behaviour everywhere at once with no restart.
func TenantPhoneCountry(tenantID string) string {
	c := strings.ToUpper(strings.TrimSpace(GetSettingString(tenantID, SettingKeyDefaultCountry)))
	if _, ok := phoneRulesByISO2[c]; ok {
		return c
	}
	return DefaultPhoneCountry
}

// NormalizeTenantPhone is the convenience wrapper almost every caller wants:
// clean `raw` against whatever country the tenant is set up for.
func NormalizeTenantPhone(tenantID, raw string) PhoneNumber {
	return NormalizePhone(raw, TenantPhoneCountry(tenantID))
}

// phoneFieldTokens are the fieldname substrings that mark a document field as
// holding a phone number. A heuristic on the fieldname rather than a new
// "Phone" fieldtype, on purpose: a fieldtype would need a migration per
// existing field and would only ever cover the fields somebody remembered to
// convert, whereas this covers every phone field in the schema today
// (Customer.phone, Location.contact_phone, Vendor/Employee contact numbers,
// the HR and supplier-portal fields) and every one added later, for free.
//
// "fax" is deliberately absent - a fax number is not reachable by SMS and is
// not a targeting signal, so mangling it into E.164 buys nothing.
var phoneFieldTokens = []string{"phone", "mobile", "whatsapp", "contact_number", "contact_no", "telephone"}

// IsPhoneField reports whether a document field holds a phone number.
//
// A derived companion ("phone_country", which holds an ISO country code) is
// excluded through the same shared check the format registry uses, so this
// function and DetectFieldFormat can never disagree about a field - which
// they did once, with the result that the country this engine had just
// stamped on a record was rejected by the validator on the next save.
func IsPhoneField(fieldname string) bool {
	if IsDerivedCompanionField(fieldname) {
		return false
	}
	f := strings.ToLower(fieldname)
	for _, tok := range phoneFieldTokens {
		if strings.Contains(f, tok) {
			return true
		}
	}
	return false
}

// NormalizeDocumentPhones cleans every phone-shaped field on a document
// payload in place, and returns what it found for the caller to inspect.
//
// This is the "clean up automatically" half of the design and it is
// deliberately non-rejecting: it rewrites "+91 (98765) 43210 " to
// "9876543210" and leaves a number it cannot make sense of exactly as it
// found it. Enforcement is a separate decision made per doctype - see
// validateCustomerMasterRules, which is the one master that refuses a
// wrong-length number - because a save path that silently dropped a real
// customer's unusual number would be worse than storing it uncleaned.
//
// Called from ValidateDocument, which is the single choke point every
// document write in the product already passes through, so nothing has to be
// wired up per screen or per API caller.
//
// `fieldnames` is the doctype's FULL declared field list, not a pre-filtered
// phone list, because this function needs to know two things about it: which
// fields hold phone numbers, and whether the doctype also declares a matching
// "<field>_country" to record where a number came from. Declaring that
// companion field on a doctype is the entire opt-in - no code changes here.
func NormalizeDocumentPhones(tenantID string, fieldnames []string, docData map[string]interface{}) map[string]PhoneNumber {
	if len(docData) == 0 {
		return nil
	}
	declared := make(map[string]bool, len(fieldnames))
	for _, fn := range fieldnames {
		declared[fn] = true
	}
	country := TenantPhoneCountry(tenantID)
	var found map[string]PhoneNumber
	for _, fn := range fieldnames {
		if !IsPhoneField(fn) {
			continue
		}
		raw, ok := docData[fn].(string)
		if !ok || strings.TrimSpace(raw) == "" {
			continue
		}
		p := NormalizePhone(raw, country)
		if p.National == "" {
			continue
		}
		// The national number is what is stored: it is what a human reads on
		// screen, what CUSTOM-0133's duplicate query compares, and what the
		// UI's country-derived length rule is expressed in. The country and
		// the international form are recoverable from it plus the record's
		// country field, so storing E.164 here would only make every existing
		// screen display a prefix nobody typed.
		docData[fn] = p.National
		// Record the country HERE, in the pass that can still see the "+1"
		// that revealed it. This is not an optimisation - it is the only
		// place the information exists. Once the dial code has been stripped,
		// a US number and an Indian one are both ten digits and nothing
		// downstream can tell them apart, which is exactly the bug this
		// closes: a "+1 212 555 1234" saved as a 10-digit number and then
		// re-examined would be tagged as domestic, silently making every
		// foreign customer look like a local one.
		if cf := fn + "_country"; declared[cf] && p.CountryISO2 != "" {
			docData[cf] = p.CountryISO2
		}
		if found == nil {
			found = map[string]PhoneNumber{}
		}
		found[fn] = p
	}
	return found
}
