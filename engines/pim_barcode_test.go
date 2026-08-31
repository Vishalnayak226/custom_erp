package engines

import (
	"custom_erp/db"
	"strconv"
	"strings"
	"testing"
)

// Known real-world GTINs (commonly cited retail examples) - using published
// valid barcodes rather than hand-computed ones catches an implementation
// bug this file's own algorithm couldn't self-confirm.
func TestValidateBarcodeCheckDigitKnownGoodCodes(t *testing.T) {
	for _, barcode := range []string{
		"4006381333931", // EAN-13
		"036000291452",  // UPC-A
		"40170725",      // EAN-8
	} {
		if err := ValidateBarcodeCheckDigit(barcode); err != nil {
			t.Errorf("ValidateBarcodeCheckDigit(%q) = %v, want nil (known-valid GTIN)", barcode, err)
		}
	}
}

func TestValidateBarcodeCheckDigitRejectsWrongDigit(t *testing.T) {
	for _, barcode := range []string{
		"4006381333930", // EAN-13, last digit flipped from the real 1
		"036000291451",  // UPC-A, last digit flipped from the real 2
		"40170724",      // EAN-8, last digit flipped from the real 5
	} {
		if err := ValidateBarcodeCheckDigit(barcode); err == nil {
			t.Errorf("ValidateBarcodeCheckDigit(%q) = nil, want a check-digit error", barcode)
		}
	}
}

// A barcode that isn't shaped like a standard GTIN (wrong length, or not
// all-digit) is left alone - most of this codebase's own Item fixtures use
// exactly this kind of free-text/internal barcode and must keep validating
// unchanged after this stage.
func TestValidateBarcodeCheckDigitIgnoresNonStandardShapes(t *testing.T) {
	for _, barcode := range []string{
		"BC-PP-A",           // free text
		"TESTTT9001",        // free text, 10 chars
		"8901234567890222",  // 16 digits - not a standard GTIN length
		"1234567",           // 7 digits - not a standard GTIN length
		"",                  // blank is handled by the caller, not here
	} {
		if err := ValidateBarcodeCheckDigit(barcode); err != nil {
			t.Errorf("ValidateBarcodeCheckDigit(%q) = %v, want nil (not GTIN-shaped)", barcode, err)
		}
	}
}

func TestGenerateEANBarcodeProducesValidUniqueCodes(t *testing.T) {
	db.InitDB(testConnStr())

	first, err := GenerateEANBarcode("default")
	if err != nil {
		t.Fatalf("GenerateEANBarcode: %v", err)
	}
	if len(first) != 13 || !strings.HasPrefix(first, "020") {
		t.Fatalf("GenerateEANBarcode() = %q, want a 13-digit code starting with 020", first)
	}
	if err := ValidateBarcodeCheckDigit(first); err != nil {
		t.Errorf("generated barcode %q failed its own check digit: %v", first, err)
	}

	second, err := GenerateEANBarcode("default")
	if err != nil {
		t.Fatalf("GenerateEANBarcode (second call): %v", err)
	}
	if second == first {
		t.Errorf("two consecutive calls returned the same barcode %q - the counter is not advancing", first)
	}
	if err := ValidateBarcodeCheckDigit(second); err != nil {
		t.Errorf("generated barcode %q failed its own check digit: %v", second, err)
	}

	// The 9-digit sequence segment (between the "020" prefix and the final
	// check digit) must be strictly increasing - not merely different -
	// since that is exactly what makes two concurrent calls unable to
	// collide (the row-locked increment GenerateSequence already provides).
	firstSeq, _ := strconv.Atoi(first[3:12])
	secondSeq, _ := strconv.Atoi(second[3:12])
	if secondSeq <= firstSeq {
		t.Errorf("sequence did not advance: first=%d second=%d", firstSeq, secondSeq)
	}
}
