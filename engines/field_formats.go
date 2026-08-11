package engines

import (
	"fmt"
	"regexp"
	"strings"
)

// Field formats (Stage 40.2).
//
// Every field in this product accepted any string. A phone number took
// letters, an email address took "asdf", a GSTIN took whatever fitted in the
// box - and the record saved happily, so the mistake surfaced weeks later on
// an invoice or an undeliverable notification.
//
// Three deliberate properties, all asked for explicitly:
//
//   1. Nothing here makes a field mandatory. A blank optional field stays
//      perfectly valid. This only says: IF you put something in, it has to be
//      the kind of thing the field is for.
//   2. The message tells you the shape and shows an example, because "invalid
//      email" helps nobody fix anything.
//   3. It is detected from the FIELD NAME, not from a fieldtype. A fieldtype
//      would need a migration per field and would only ever cover the ones
//      somebody remembered to convert; this covers every gstin/email/phone/
//      pan/ifsc/pincode field in the schema today and every one added later,
//      for free. Same reasoning - and the same mechanism - as Stage 41's
//      IsPhoneField, which this generalises.
//
// Wired into ValidateDocument, the one choke point every document write in
// the product already passes through, so no screen or API caller needs
// touching. The specs are also served to the frontend
// (GET /api/v1/meta/field-formats) so the input hints and keystroke filtering
// are driven by these same declarations rather than a second copy of the
// regexes in JavaScript.

// FieldFormat declares one recognisable kind of field.
type FieldFormat struct {
	// Key is the stable identifier the frontend switches on.
	Key string `json:"key"`
	// Label names the format in an error message ("GSTIN", "email address").
	Label string `json:"label"`
	// Tokens are the fieldname substrings that identify this format.
	Tokens []string `json:"-"`
	// Pattern is the server-side gate. Empty means "no regex gate" (the phone
	// format is validated by the country-aware phone engine instead).
	Pattern *regexp.Regexp `json:"-"`
	// Hint is shown under the input and inside the error. Written as the rule
	// plus a real example, never as "invalid X".
	Hint string `json:"hint"`
	// Placeholder is the greyed-out example in the empty input - this is the
	// "it should suggest it should be like this" half.
	Placeholder string `json:"placeholder"`
	// AllowedChars is a JS-safe character class the frontend uses to refuse
	// keystrokes outright. Empty means accept any character and rely on the
	// on-save check. This is what stops a phone field taking letters.
	AllowedChars string `json:"allowed_chars,omitempty"`
	// Uppercase marks formats stored upper-case (GSTIN, PAN, IFSC); the
	// frontend upper-cases as you type so the value cannot fail the check
	// purely for case.
	Uppercase bool `json:"uppercase,omitempty"`
	// ErrorCode is the catalog code to raise, where the Standard Message
	// Control Matrix already has a row for this exact scenario. Empty means
	// the generic 422 fallback, which ValidateDocument already handles for
	// any uncataloged validation message.
	ErrorCode string `json:"-"`
	// MaxLen bounds the value where the format has a fixed width.
	MaxLen int `json:"max_len,omitempty"`
}

// The patterns. gstinPattern is deliberately NOT redeclared - it already
// exists in master_data_validation.go and this file reuses that exact
// definition, so the two can never drift.
var (
	// Requires a dot-bearing domain, which is the "@ and . minimum" rule
	// asked for, and refuses the whitespace/comma mistakes that actually
	// happen when someone pastes two addresses into one box.
	emailPattern   = regexp.MustCompile(`^[^\s@,;]+@[^\s@,;]+\.[A-Za-z]{2,}$`)
	panPattern     = regexp.MustCompile(`^[A-Z]{5}[0-9]{4}[A-Z]$`)
	ifscPattern    = regexp.MustCompile(`^[A-Z]{4}0[A-Z0-9]{6}$`)
	pincodePattern = regexp.MustCompile(`^[1-9][0-9]{5}$`)
	// Letters are the thing a phone field must never take. Length and country
	// are left to the phone engine (Stage 41), which deliberately cleans
	// rather than rejects - a real order must never be lost over a contact
	// field's formatting.
	phoneLetterPattern = regexp.MustCompile(`[A-Za-z]`)
	urlPattern         = regexp.MustCompile(`^https?://[^\s]+\.[^\s]+$`)
)

// fieldFormats is the registry, in detection order. Order matters: a field
// called "gst_email" would match both, and the first entry wins, so the more
// specific tokens are listed first.
var fieldFormats = []FieldFormat{
	{
		Key: "gstin", Label: "GSTIN",
		Tokens:       []string{"gstin", "gst_no", "gst_number", "gst_in"},
		Pattern:      gstinPattern,
		Hint:         "15 characters: 2-digit state code, 10-character PAN, then 3 more. Example: 27AAPFU0939F1ZV",
		Placeholder:  "27AAPFU0939F1ZV",
		AllowedChars: "0-9A-Za-z",
		Uppercase:    true,
		ErrorCode:    "MASTER-0049",
		MaxLen:       15,
	},
	{
		Key: "pan", Label: "PAN",
		Tokens:       []string{"pan_number", "pan_no", "pan"},
		Pattern:      panPattern,
		Hint:         "10 characters: 5 letters, 4 digits, 1 letter. Example: AAPFU0939F",
		Placeholder:  "AAPFU0939F",
		AllowedChars: "0-9A-Za-z",
		Uppercase:    true,
		MaxLen:       10,
	},
	{
		Key: "ifsc", Label: "IFSC code",
		Tokens:       []string{"ifsc"},
		Pattern:      ifscPattern,
		Hint:         "11 characters: 4 bank letters, a 0, then 6 more. Example: HDFC0001234",
		Placeholder:  "HDFC0001234",
		AllowedChars: "0-9A-Za-z",
		Uppercase:    true,
		MaxLen:       11,
	},
	{
		Key: "email", Label: "email address",
		Tokens:      []string{"email", "e_mail", "mail_id"},
		Pattern:     emailPattern,
		Hint:        "Needs an @ and a dot in the domain. Example: buyer@company.com",
		Placeholder: "name@company.com",
	},
	{
		Key: "pincode", Label: "PIN code",
		Tokens:       []string{"pincode", "pin_code", "postal_code", "postcode", "zip"},
		Pattern:      pincodePattern,
		Hint:         "6 digits, not starting with 0. Example: 400051",
		Placeholder:  "400051",
		AllowedChars: "0-9",
		MaxLen:       6,
	},
	{
		Key: "url", Label: "web address",
		Tokens:      []string{"website", "url", "webhook"},
		Pattern:     urlPattern,
		Hint:        "Must start with http:// or https://. Example: https://vendor.example.com",
		Placeholder: "https://example.com",
	},
	{
		// Last, so a field named "phone_email" is treated as the email it is.
		// Pattern is nil: the country-aware phone engine owns the real check,
		// and this entry exists for the letters gate plus the frontend's
		// keystroke filter - "+, -, space allowed, alphabet not".
		Key: "phone", Label: "phone number",
		Tokens:       phoneFieldTokens,
		Hint:         "Digits only. + - ( ) and spaces are fine; letters are not. Example: +91 98765 43210",
		Placeholder:  "98765 43210",
		AllowedChars: "0-9+\\-() ",
	},
}

// fieldFormatExcludedSuffixes are the DERIVED companions of a formatted
// field: fields whose name contains a format token only because they describe
// one, and which hold something else entirely.
//
// The case that forced this: Stage 41 stamps the resolved country of a
// phone number onto "<field>_country", so Customer gains "phone_country"
// holding "US". Substring matching sees "phone" in that name and refuses to
// save it, because "US" is not a phone number - a field the server itself
// writes being rejected by the server's own validator.
//
// A suffix list rather than a per-field opt-out, because the same shape will
// recur ("email_verified_at", "gstin_status") and each would otherwise have to
// be discovered by hitting the bug.
var fieldFormatExcludedSuffixes = []string{"_country", "_verified", "_verified_at", "_status", "_type", "_code_type"}

// IsDerivedCompanionField reports whether a fieldname is a companion of a
// formatted field rather than one itself. Shared by DetectFieldFormat and
// IsPhoneField so the two cannot disagree about the same field, and served to
// the frontend alongside the format tokens for the same reason.
func IsDerivedCompanionField(fieldname string) bool {
	f := strings.ToLower(strings.TrimSpace(fieldname))
	for _, suf := range fieldFormatExcludedSuffixes {
		if strings.HasSuffix(f, suf) {
			return true
		}
	}
	return false
}

// DetectFieldFormat returns the format a fieldname implies, and whether one
// was recognised. Unrecognised fields are simply not format-checked, which is
// the behaviour every field had before this.
func DetectFieldFormat(fieldname string) (FieldFormat, bool) {
	f := strings.ToLower(strings.TrimSpace(fieldname))
	if f == "" || IsDerivedCompanionField(f) {
		return FieldFormat{}, false
	}
	for _, spec := range fieldFormats {
		for _, tok := range spec.Tokens {
			if strings.Contains(f, tok) {
				return spec, true
			}
		}
	}
	return FieldFormat{}, false
}

// FieldFormatSpecs returns every declared format for the frontend, keyed by
// format key, plus the tokens it should match fieldnames against. Serving the
// tokens rather than duplicating them in JavaScript is what keeps one list.
func FieldFormatSpecs() map[string]interface{} {
	specs := make([]map[string]interface{}, 0, len(fieldFormats))
	for _, f := range fieldFormats {
		specs = append(specs, map[string]interface{}{
			"key":           f.Key,
			"label":         f.Label,
			"tokens":        f.Tokens,
			"hint":          f.Hint,
			"placeholder":   f.Placeholder,
			"allowed_chars": f.AllowedChars,
			"uppercase":     f.Uppercase,
			"max_len":       f.MaxLen,
		})
	}
	return map[string]interface{}{
		"formats": specs,
		// The frontend does its own substring matching against the tokens
		// above, so it needs the same exclusions or it would put a phone
		// keystroke filter on a country-code field the user never types in.
		"excluded_suffixes": fieldFormatExcludedSuffixes,
	}
}

// NormalizeFieldFormatValue applies the storage convention for a format -
// today only "these are stored upper-case". Applied before validation so a
// GSTIN typed in lower case saves rather than failing on case alone.
func NormalizeFieldFormatValue(spec FieldFormat, value string) string {
	v := strings.TrimSpace(value)
	if spec.Uppercase {
		v = strings.ToUpper(v)
	}
	return v
}

// ValidateFieldFormat checks one value against one format. An empty value is
// always fine - that is the "not mandatory" guarantee.
func ValidateFieldFormat(spec FieldFormat, label, value string) error {
	v := strings.TrimSpace(value)
	if v == "" {
		return nil
	}

	name := label
	if name == "" {
		name = spec.Label
	}

	if spec.Key == "phone" {
		if phoneLetterPattern.MatchString(v) {
			return &ValidationError{
				Code:    "MASTER-0051",
				SubFor:  name,
				Message: fmt.Sprintf("%s must be a phone number, not text. %s", name, spec.Hint),
			}
		}
		return nil
	}

	if spec.Pattern == nil || spec.Pattern.MatchString(v) {
		return nil
	}
	return &ValidationError{
		Code:    spec.ErrorCode,
		SubFor:  name,
		Message: fmt.Sprintf("%s %q is not a valid %s. %s", name, value, spec.Label, spec.Hint),
	}
}

// ValidateDocumentFieldFormats normalises and format-checks every recognised
// field on a document payload, in place.
//
// Called from ValidateDocument after the mandatory and length passes, so a
// genuinely required-but-empty field still reports as missing rather than as
// badly formatted. Iterates the doctype's declared fields rather than the
// payload keys, so a caller cannot dodge the check by sending an extra key
// the doctype does not declare - and so the field's own Label is what appears
// in the message.
func ValidateDocumentFieldFormats(fields []FieldMeta, docData map[string]interface{}) error {
	if len(docData) == 0 {
		return nil
	}
	for _, f := range fields {
		spec, ok := DetectFieldFormat(f.Fieldname)
		if !ok {
			continue
		}
		raw, isString := docData[f.Fieldname].(string)
		if !isString || strings.TrimSpace(raw) == "" {
			continue
		}
		normalized := NormalizeFieldFormatValue(spec, raw)
		if normalized != raw {
			docData[f.Fieldname] = normalized
		}
		if err := ValidateFieldFormat(spec, f.Label, normalized); err != nil {
			return err
		}
	}
	return nil
}
