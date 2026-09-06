package engines

import "testing"

// Stage 47.6.1 - "remove substring inference that treats `mobile-pick-wave-id`
// as an India phone field" (audit finding A-06).
//
// The bug was silent and destructive: the RF picking screen's Wave ID input is
// called mobile-pick-wave-id, DetectFieldFormat matched "mobile" against the
// phone tokens, and the phone keystroke filter then stripped the letters out of
// a wave id as the operator typed it - on a device where nobody could see the
// rewrite happen.
func TestFieldSemanticsBeatSubstringInference(t *testing.T) {
	// The exact identifier the audit names.
	if got := FieldSemantic("mobile-pick-wave-id"); got != SemanticWave {
		t.Errorf("FieldSemantic(%q) = %q, want %q", "mobile-pick-wave-id", got, SemanticWave)
	}
	if _, ok := DetectFieldFormat("mobile-pick-wave-id"); ok {
		t.Error("mobile-pick-wave-id still resolves to a character format; a wave id is a scanned code and must not be filtered as a phone number")
	}
	if IsPhoneField("mobile-pick-wave-id") {
		// IsPhoneField is what NormalizeDocumentPhones uses, so a true here
		// would mean the server itself rewriting a stored wave id into E.164.
		t.Error("mobile-pick-wave-id is still treated as a phone field")
	}

	// Every scan-shaped identifier must be free of character filtering, since
	// each of these legitimately contains letters.
	for _, id := range []string{
		"mobile-pick-lpn", "mobile-batch-no", "mobile-serial-no", "mobile-scan",
		"rf-putaway-lot", "rf-serial-no", "batch_no", "serial_no", "lpn", "barcode",
	} {
		if !IsScanField(id) {
			t.Errorf("%q is not recognised as a scanned code", id)
		}
		if _, ok := DetectFieldFormat(id); ok {
			t.Errorf("%q resolves to a character format; a scanned code must not be filtered", id)
		}
	}

	// ...and the inference must still work for everything it was built for.
	// Removing it wholesale, rather than overriding it, would have silently
	// stopped normalising every real phone field in the schema.
	for _, id := range []string{"phone", "contact_phone", "whatsapp_number", "mobile_number", "telephone"} {
		if !IsPhoneField(id) {
			t.Errorf("%q is no longer recognised as a phone field; the override must not break ordinary inference", id)
		}
	}
	// An explicit phone semantic still resolves to the phone format.
	if spec, ok := DetectFieldFormat("mobile-phone"); !ok || spec.Key != "phone" {
		t.Errorf("an explicitly declared phone field lost its format (ok=%v spec=%q)", ok, spec.Key)
	}
	// A derived companion is still excluded.
	if _, ok := DetectFieldFormat("phone_country"); ok {
		t.Error("phone_country resolves to a format again; it holds a country code, not a phone number")
	}
}

// TestFieldSemanticSpecsAreServedToTheBrowser guards the thing that makes the
// two sides agree: app.js resolves the same identifiers, and it can only do so
// if the registry is actually shipped to it.
func TestFieldSemanticSpecsAreServedToTheBrowser(t *testing.T) {
	specs := FieldFormatSpecs()
	semantics, ok := specs["semantics"].(map[string]string)
	if !ok || len(semantics) == 0 {
		t.Fatal("FieldFormatSpecs serves no semantics map; the browser would fall back to token inference and reach the opposite answer from the server")
	}
	if semantics["mobile-pick-wave-id"] != SemanticWave {
		t.Errorf("the served map does not carry the wave semantic for mobile-pick-wave-id (got %q)", semantics["mobile-pick-wave-id"])
	}
	scan, ok := specs["scan"].([]string)
	if !ok || len(scan) == 0 {
		t.Fatal("FieldFormatSpecs serves no scan-semantic list")
	}
}
