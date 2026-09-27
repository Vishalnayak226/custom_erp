package engines

import "testing"

// FuzzValidateBarcodeCheckDigit targets the one GS1 mod-10 implementation
// every barcode field's validation and generation both share (BLD-018,
// identifiers/parsers bucket). Two properties beyond panic-freedom: an error
// is only ever returned for input that already looks like a standard
// EAN-8/UPC-A/EAN-13 barcode (anything else is intentionally left alone),
// and a barcode this package's own generator would consider correct for a
// given data prefix must never be rejected by the validator - the same
// formula computes and checks, so they cannot disagree on their own output.
func FuzzValidateBarcodeCheckDigit(f *testing.F) {
	for _, seed := range []string{
		"12345678", "123456789012", "1234567890128",
		"", "abcdefgh", "1234567", "999999999999999999999999999999",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, barcode string) {
		if err := ValidateBarcodeCheckDigit(barcode); err != nil && !looksLikeGS1Barcode(barcode) {
			t.Fatalf("ValidateBarcodeCheckDigit(%q) returned an error for input that isn't barcode-shaped: %v", barcode, err)
		}
		for length := range gs1StandardBarcodeLengths {
			if len(barcode) != length-1 {
				continue
			}
			data := barcode
			isDigits := true
			for i := 0; i < len(data); i++ {
				if data[i] < '0' || data[i] > '9' {
					isDigits = false
					break
				}
			}
			if !isDigits {
				continue
			}
			digit, err := gs1CheckDigit(data)
			if err != nil {
				continue
			}
			constructed := data + string(rune('0'+digit))
			if verr := ValidateBarcodeCheckDigit(constructed); verr != nil {
				t.Fatalf("constructed %q from gs1CheckDigit's own output but ValidateBarcodeCheckDigit rejected it: %v", constructed, verr)
			}
		}
	})
}
