package engines

import (
	"strings"
	"testing"
)

// Stage 40.2. Pure logic - no DB needed.

func TestDetectFieldFormat(t *testing.T) {
	cases := []struct {
		fieldname string
		wantKey   string
	}{
		{"gstin", "gstin"},
		{"vendor_gstin", "gstin"},
		{"gst_number", "gstin"},
		{"contact_email", "email"},
		{"email", "email"},
		{"mail_id", "email"},
		{"contact_phone", "phone"},
		{"mobile", "phone"},
		{"whatsapp_number", "phone"},
		{"bank_ifsc", "ifsc"},
		{"pan_number", "pan"},
		{"pincode", "pincode"},
		{"postal_code", "pincode"},
		{"website", "url"},
		// Nothing recognisable - must be left alone, exactly as before.
		{"name", ""},
		{"total_amount", ""},
		{"description", ""},
		{"", ""},
	}
	for _, c := range cases {
		spec, ok := DetectFieldFormat(c.fieldname)
		if c.wantKey == "" {
			if ok {
				t.Errorf("DetectFieldFormat(%q) matched %q, want no match", c.fieldname, spec.Key)
			}
			continue
		}
		if !ok || spec.Key != c.wantKey {
			t.Errorf("DetectFieldFormat(%q) = %q/%v, want %q", c.fieldname, spec.Key, ok, c.wantKey)
		}
	}
}

// The central promise: an empty value is always valid, for every format.
// Nothing this stage added may turn an optional field into a required one.
func TestEmptyValueIsAlwaysValid(t *testing.T) {
	for _, spec := range fieldFormats {
		for _, empty := range []string{"", "   ", "\t"} {
			if err := ValidateFieldFormat(spec, "Test Field", empty); err != nil {
				t.Errorf("format %q rejected an empty value %q: %v", spec.Key, empty, err)
			}
		}
	}
}

func TestValidateFieldFormatGSTIN(t *testing.T) {
	spec, _ := DetectFieldFormat("gstin")
	if err := ValidateFieldFormat(spec, "GSTIN", "27AAPFU0939F1ZV"); err != nil {
		t.Errorf("valid GSTIN rejected: %v", err)
	}
	for _, bad := range []string{"27AAPFU0939F1Z", "hello", "123456789012345", "27AAPFU0939F1ZVX"} {
		if err := ValidateFieldFormat(spec, "GSTIN", bad); err == nil {
			t.Errorf("invalid GSTIN %q accepted", bad)
		}
	}
	// Keeps the catalog code the Vendor master already raised for this
	// scenario, so the frontend's inline-field-message handling is unchanged.
	err := ValidateFieldFormat(spec, "GSTIN", "nope")
	verr, ok := err.(*ValidationError)
	if !ok || verr.Code != "MASTER-0049" {
		t.Errorf("expected MASTER-0049, got %#v", err)
	}
	// The message has to teach the shape, not just say "invalid".
	if !strings.Contains(verr.Message, "27AAPFU0939F1ZV") {
		t.Errorf("error message carries no example: %q", verr.Message)
	}
}

func TestValidateFieldFormatEmail(t *testing.T) {
	spec, _ := DetectFieldFormat("contact_email")
	for _, good := range []string{"buyer@company.com", "a.b+tag@sub.domain.co.in", "x@y.io"} {
		if err := ValidateFieldFormat(spec, "Email", good); err != nil {
			t.Errorf("valid email %q rejected: %v", good, err)
		}
	}
	// The user's rule: needs an @ and a dot, or it does not save.
	for _, bad := range []string{"asdf", "no-at-sign.com", "missing@dot", "two@addresses.com, x@y.com", "spaces @x.com"} {
		if err := ValidateFieldFormat(spec, "Email", bad); err == nil {
			t.Errorf("invalid email %q accepted", bad)
		}
	}
}

func TestValidateFieldFormatPhoneRejectsLettersOnly(t *testing.T) {
	spec, _ := DetectFieldFormat("contact_phone")
	// "+, -, space allowed, alphabet not."
	for _, good := range []string{"9876543210", "+91 98765 43210", "98765-43210", "(022) 1234 5678"} {
		if err := ValidateFieldFormat(spec, "Phone", good); err != nil {
			t.Errorf("valid phone %q rejected: %v", good, err)
		}
	}
	for _, bad := range []string{"call me", "N/A", "98765ABC43"} {
		if err := ValidateFieldFormat(spec, "Phone", bad); err == nil {
			t.Errorf("phone %q with letters accepted", bad)
		}
	}
}

func TestValidateFieldFormatPANIFSCPincode(t *testing.T) {
	pan, _ := DetectFieldFormat("pan_number")
	if err := ValidateFieldFormat(pan, "PAN", "AAPFU0939F"); err != nil {
		t.Errorf("valid PAN rejected: %v", err)
	}
	if err := ValidateFieldFormat(pan, "PAN", "AAPFU0939"); err == nil {
		t.Error("short PAN accepted")
	}

	ifsc, _ := DetectFieldFormat("bank_ifsc")
	if err := ValidateFieldFormat(ifsc, "IFSC", "HDFC0001234"); err != nil {
		t.Errorf("valid IFSC rejected: %v", err)
	}
	if err := ValidateFieldFormat(ifsc, "IFSC", "HDFC1001234"); err == nil {
		t.Error("IFSC without the mandatory 5th-character 0 accepted")
	}

	pin, _ := DetectFieldFormat("pincode")
	if err := ValidateFieldFormat(pin, "PIN", "400051"); err != nil {
		t.Errorf("valid pincode rejected: %v", err)
	}
	for _, bad := range []string{"04051", "0400051", "40005"} {
		if err := ValidateFieldFormat(pin, "PIN", bad); err == nil {
			t.Errorf("invalid pincode %q accepted", bad)
		}
	}
}

// A GSTIN typed in lower case must save, not fail on case alone.
func TestValidateDocumentFieldFormatsUppercasesInPlace(t *testing.T) {
	fields := []FieldMeta{
		{Fieldname: "gstin", Label: "GSTIN"},
		{Fieldname: "contact_email", Label: "Contact Email"},
	}
	doc := map[string]interface{}{
		"gstin":         " 27aapfu0939f1zv ",
		"contact_email": "buyer@company.com",
	}
	if err := ValidateDocumentFieldFormats(fields, doc); err != nil {
		t.Fatalf("unexpected rejection: %v", err)
	}
	if doc["gstin"] != "27AAPFU0939F1ZV" {
		t.Errorf("gstin not normalised in place: %#v", doc["gstin"])
	}
}

// Only DECLARED fields are checked - a caller cannot dodge the gate, and a
// stray undeclared key is not invented into a validation error either.
func TestValidateDocumentFieldFormatsIgnoresUndeclaredKeys(t *testing.T) {
	fields := []FieldMeta{{Fieldname: "name", Label: "Name"}}
	doc := map[string]interface{}{"name": "Acme", "gstin": "garbage"}
	if err := ValidateDocumentFieldFormats(fields, doc); err != nil {
		t.Errorf("undeclared field was validated: %v", err)
	}

	declared := []FieldMeta{{Fieldname: "gstin", Label: "GSTIN"}}
	if err := ValidateDocumentFieldFormats(declared, doc); err == nil {
		t.Error("declared bad GSTIN was not rejected")
	}
}

// Non-string values (a Check field's bool, a Number's float) must not panic
// or be coerced into a format check.
func TestValidateDocumentFieldFormatsSkipsNonStrings(t *testing.T) {
	fields := []FieldMeta{{Fieldname: "phone", Label: "Phone"}}
	for _, v := range []interface{}{nil, true, 42.0, []string{"x"}} {
		if err := ValidateDocumentFieldFormats(fields, map[string]interface{}{"phone": v}); err != nil {
			t.Errorf("non-string %#v produced an error: %v", v, err)
		}
	}
}

func TestFieldFormatSpecsAreServable(t *testing.T) {
	out := FieldFormatSpecs()
	formats, ok := out["formats"].([]map[string]interface{})
	if !ok || len(formats) != len(fieldFormats) {
		t.Fatalf("expected %d formats, got %#v", len(fieldFormats), out["formats"])
	}
	for _, f := range formats {
		if f["key"] == "" || f["hint"] == "" {
			t.Errorf("format served without a key or hint: %#v", f)
		}
	}
}
