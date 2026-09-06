package engines

import "strings"

// Stage 47.6.1 - explicit semantic field metadata (audit finding A-06).
//
// The bug this exists to kill, exactly: the RF picking screen's Wave ID input
// is called `mobile-pick-wave-id`, and DetectFieldFormat matched the substring
// "mobile" against the phone-format tokens. So the field that receives a wave
// identifier was given the phone keystroke filter - digits, +, -, ( ), space
// allowed, letters silently stripped - and a wave id like "WV-2026-0007" lost
// its letters as the operator typed it. On a warehouse floor, on a device
// where nobody can see a subtle rewrite happening, that is data loss with no
// error message.
//
// The general problem is that substring inference has no way to be told it is
// wrong. This file is that way: a small, explicit registry that WINS over
// inference, and whose entries can say "this is a scanned code, not a phone".
//
// Deliberately NOT a wholesale replacement of the inference. The inference's
// own comment (engines/phone.go) explains why it exists: a "Phone" fieldtype
// would need a migration per existing field and would only cover the ones
// somebody remembered to convert, whereas the heuristic covers every phone
// field in the schema today and every one added later for free. That reasoning
// is still right. What was missing was a way to override it - and an override
// list is short, auditable, and only has to name the exceptions.

// Field semantics. These name what an input HOLDS, which is a different
// question from what format it must match - a barcode and a wave id are both
// "a scanned code", but neither is a phone number and neither is free text a
// spell-checker should touch.
const (
	SemanticText    = "text"
	SemanticPhone   = "phone"
	SemanticBarcode = "barcode"
	SemanticWave    = "wave"
	SemanticLPN     = "lpn"
	SemanticLot     = "lot"
	SemanticSerial  = "serial"
	SemanticNumber  = "number"
	SemanticDate    = "date"
)

// scanSemantics are the semantics that describe a code a machine reads. They
// share one input treatment (uppercase, no letter-stripping, scanner-friendly)
// and, crucially, none of them is a phone number.
var scanSemantics = map[string]bool{
	SemanticBarcode: true, SemanticWave: true, SemanticLPN: true,
	SemanticLot: true, SemanticSerial: true,
}

// explicitFieldSemantics maps a field or input identifier to what it holds.
// Keys are matched case-insensitively against the whole identifier first, and
// then against its "-"/"_" separated words, so both `mobile-pick-wave-id` (a
// screen's element id) and `wave_id` (a document fieldname) resolve.
//
// Entries are added only where inference is WRONG or where the semantic is
// worth declaring for the RF shell's own input handling. A field not listed
// here is unaffected and keeps exactly the behaviour it has today.
var explicitFieldSemantics = map[string]string{
	// --- the A-06 case and its siblings -------------------------------
	// Every one of these contains a phone-format token ("mobile") or would
	// otherwise be inferred wrongly, and every one holds a scanned code.
	"mobile-pick-wave-id": SemanticWave,
	"mobile-pick-sku":     SemanticBarcode,
	"mobile-pick-bin":     SemanticBarcode,
	"mobile-pick-lpn":     SemanticLPN,
	"mobile-receive-asn":  SemanticBarcode,
	"mobile-receive-sku":  SemanticBarcode,
	"mobile-putaway-bin":  SemanticBarcode,
	"mobile-putaway-lpn":  SemanticLPN,
	"mobile-batch-no":     SemanticLot,
	"mobile-serial-no":    SemanticSerial,
	"mobile-scan":         SemanticBarcode,
	"mobile-number":       SemanticPhone,
	"mobile-phone":        SemanticPhone,

	// --- document fields worth declaring ------------------------------
	"wave_id":       SemanticWave,
	"wave_code":     SemanticWave,
	"lpn":           SemanticLPN,
	"lpn_code":      SemanticLPN,
	"license_plate": SemanticLPN,
	"batch_no":      SemanticLot,
	"lot_no":        SemanticLot,
	"serial_no":     SemanticSerial,
	"serial_number": SemanticSerial,
	"barcode":       SemanticBarcode,
	"gtin":          SemanticBarcode,
	"bin_code":      SemanticBarcode,
	"sku":           SemanticBarcode,

	// --- bare words, for identifiers a screen composes -----------------
	// A bespoke screen names its inputs by id (rf-putaway-lot,
	// rf-serial-no), so the word match has to resolve the vocabulary word
	// itself, not only the full field name. These are safe to claim because
	// the only consequence of a scan semantic is "do not apply a character
	// filter to this", which is never wrong for a code and never harmful for
	// anything else.
	"lot":    SemanticLot,
	"batch":  SemanticLot,
	"serial": SemanticSerial,
	"wave":   SemanticWave,
	"scan":   SemanticBarcode,
}

// FieldSemantic resolves what an identifier holds, or "" when nothing is
// declared for it. Whole-identifier match first, then word match - so a screen
// that names an input `receive-lpn` gets the LPN semantic without needing its
// own entry, while `mobile-pick-wave-id` gets Wave from its exact entry rather
// than matching the word "mobile" against anything.
func FieldSemantic(identifier string) string {
	id := strings.ToLower(strings.TrimSpace(identifier))
	if id == "" {
		return ""
	}
	if s, ok := explicitFieldSemantics[id]; ok {
		return s
	}
	// Word match, longest word first, so `serial_number` prefers "serial"
	// over a shorter accidental hit.
	words := strings.FieldsFunc(id, func(r rune) bool { return r == '-' || r == '_' || r == '.' })
	for _, w := range words {
		if s, ok := explicitFieldSemantics[w]; ok {
			return s
		}
	}
	return ""
}

// IsScanField reports whether an identifier holds a machine-read code. The RF
// shell uses it to decide scanner focus and input treatment, and
// DetectFieldFormat uses it to refuse to apply a phone/email/number filter to
// something that is neither.
func IsScanField(identifier string) bool {
	return scanSemantics[FieldSemantic(identifier)]
}

// FieldSemanticSpecs is what the frontend fetches alongside the format specs,
// so the browser and the server can never disagree about which input is a
// wave id and which is a phone number - the same reason FieldFormatSpecs is
// served rather than duplicated in app.js.
func FieldSemanticSpecs() map[string]interface{} {
	semantics := make(map[string]string, len(explicitFieldSemantics))
	for k, v := range explicitFieldSemantics {
		semantics[k] = v
	}
	scan := make([]string, 0, len(scanSemantics))
	for s := range scanSemantics {
		scan = append(scan, s)
	}
	return map[string]interface{}{
		"semantics": semantics,
		"scan":      scan,
	}
}
