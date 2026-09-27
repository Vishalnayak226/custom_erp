package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"strings"
)

// Stage 36.7.4: UPC/EAN generation and check-digit validation. Implements
// the one GS1 mod-10 algorithm (GS1 General Specifications) that both
// verifies and mints the check digit for every standard linear retail
// barcode length this ERP is likely to see - EAN-8, UPC-A and EAN-13 - so
// there is one formula, not three near-identical ones that could drift
// apart. Not a code-authority integration: a generated code is
// numerically valid and scans correctly, but is not a licensed GS1 GTIN
// unless the tenant separately holds the company prefix used to seed it -
// stated scope limit, not a guess.

// gs1CheckDigit implements the canonical GS1 mod-10 algorithm: numbering
// digit positions from the right (the position immediately left of where
// the check digit goes is position 1), odd positions are weighted 3 and
// even positions 1. This right-to-left definition is what makes one
// implementation correct for every GTIN length (EAN-8's 7-digit base,
// UPC-A/EAN-13's 12-digit base) - a naive left-to-right weighting flips
// which positions get which weight depending on whether the base has an
// even or odd digit count, which is exactly the kind of off-by-one this
// stage's own tests pin down.
func gs1CheckDigit(data string) (int, error) {
	if data == "" {
		return 0, fmt.Errorf("no digits to compute a check digit from")
	}
	sum := 0
	weight := 3
	for i := len(data) - 1; i >= 0; i-- {
		d := data[i]
		if d < '0' || d > '9' {
			return 0, fmt.Errorf("barcode must be all digits")
		}
		sum += int(d-'0') * weight
		if weight == 3 {
			weight = 1
		} else {
			weight = 3
		}
	}
	return (10 - (sum % 10)) % 10, nil
}

// gs1StandardBarcodeLengths is the closed set of lengths this ERP treats as
// "shaped like a standard linear barcode" and therefore check-digit-
// validates: EAN-8 (8), UPC-A (12) and EAN-13 (13). A barcode of any other
// length - an internal SKU-style code, a QR payload, free text - is left
// exactly as before this stage: most of this codebase's own Item fixtures
// use one of those and must keep validating unchanged.
var gs1StandardBarcodeLengths = map[int]bool{8: true, 12: true, 13: true}

func looksLikeGS1Barcode(barcode string) bool {
	if !gs1StandardBarcodeLengths[len(barcode)] {
		return false
	}
	for i := 0; i < len(barcode); i++ {
		if barcode[i] < '0' || barcode[i] > '9' {
			return false
		}
	}
	return true
}

// ValidateBarcodeCheckDigit (36.7.4) refuses a barcode that is shaped like a
// standard EAN-8/UPC-A/EAN-13 (all-digit, one of the three standard
// lengths) but carries the wrong check digit - the same "the field looks
// like X, so it must actually be a valid X" discipline the GSTIN/IFSC
// format checks already apply elsewhere in this file family. Anything else
// is left alone: not every tenant's "barcode" field holds a real GS1
// number, and this stage does not require one.
func ValidateBarcodeCheckDigit(barcode string) error {
	if !looksLikeGS1Barcode(barcode) {
		return nil
	}
	data := barcode[:len(barcode)-1]
	want := int(barcode[len(barcode)-1] - '0')
	got, err := gs1CheckDigit(data)
	if err != nil {
		return nil // unreachable: looksLikeGS1Barcode already confirmed all-digit
	}
	if got != want {
		return fmt.Errorf("barcode %q has an invalid check digit for its length (expected %d, got %d) - use POST /api/v1/pim/barcode/generate for a valid one", barcode, got, want)
	}
	return nil
}

// GenerateEANBarcode (36.7.4) mints a fresh, check-digit-correct EAN-13
// seeded with GS1's own "restricted circulation / internal use" prefix
// range (020-029) - a code range a business may assign itself with no
// registration, valid for internal scanning (POS, WMS) but never
// resellable as a real product's public GTIN, which is exactly the
// distinction the doc comment above states. The sequence is drawn from the
// existing document-numbering counter (engines/numbering.go) rather than a
// second counter mechanism, keyed by a dedicated "PIMBarcodeSeq" series
// (db/migrations_stage36_7_enrichment_quality.sql) configured with an
// empty prefix/separator and reset_frequency NEVER, so GenerateSequence's
// own formatting collapses to exactly nine zero-padded digits with nothing
// else - the one thing that reuse gives for free is the same row-locked
// atomic increment every other document series already relies on, so two
// concurrent generate calls can never mint the same code.
func GenerateEANBarcode(tenantID string) (string, error) {
	seq, err := GenerateSequence(tenantID, "PIMBarcodeSeq", "", "")
	if err != nil {
		return "", err
	}
	if len(seq) != 9 {
		return "", fmt.Errorf("PIMBarcodeSeq numbering configuration is not set up for a 9-digit sequence (got %q) - check prefix_configs", seq)
	}
	base := "020" + seq
	check, err := gs1CheckDigit(base)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%d", base, check), nil
}

// EnsureItemBarcodes (Stage 51.6) auto-generates a barcode for every
// distinct Item received on a GRN that doesn't already have one, so goods
// receipt is the point items get barcoded without a separate manual PIM
// step (previously GenerateEANBarcode/handlePIMGenerateBarcode only ever
// ran from a standalone, manually-triggered action, never from receiving).
// Best-effort by design: a generation/save failure for one SKU is logged
// and skipped rather than failing the receipt - stock has already posted by
// the time this runs (see its call site in handlers_core_doc_engine.go), so
// a missing barcode is a smaller problem than an inconsistent GRN, and one
// bad SKU must not block barcoding the rest of the line items.
func EnsureItemBarcodes(tenantID string, items []interface{}) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	seen := map[string]bool{}
	for _, raw := range items {
		m, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		sku, _ := m["sku"].(string)
		if sku == "" || seen[sku] {
			continue
		}
		seen[sku] = true

		var existing sql.NullString
		if errQ := db.DB.QueryRow(fmt.Sprintf(
			`SELECT data->>'barcode' FROM %s.documents WHERE doctype = 'Item' AND id = $1 AND deleted_at IS NULL`, schema), sku,
		).Scan(&existing); errQ != nil {
			continue // unknown/missing Item - nothing to barcode
		}
		if existing.Valid && strings.TrimSpace(existing.String) != "" {
			continue // already has one
		}

		barcode, errGen := GenerateEANBarcode(tenantID)
		if errGen != nil {
			LogSystemError(tenantID, "", "ERROR", "EnsureItemBarcodes", fmt.Sprintf("could not auto-generate a barcode for Item %s at GRN receipt: %v", sku, errGen), "")
			continue
		}
		if _, errUpd := db.DB.Exec(fmt.Sprintf(
			`UPDATE %s.documents SET data = jsonb_set(data::jsonb, '{barcode}', to_jsonb($1::text), true), updated_at = CURRENT_TIMESTAMP WHERE doctype = 'Item' AND id = $2`, schema),
			barcode, sku); errUpd != nil {
			LogSystemError(tenantID, "", "ERROR", "EnsureItemBarcodes", fmt.Sprintf("could not save auto-generated barcode for Item %s: %v", sku, errUpd), "")
		}
	}
}
