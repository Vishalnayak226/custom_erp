package engines

import (
	"fmt"
	"strings"
)

// Mirrored fields (Stage 30.5.6).
//
// A handful of doctypes carry two mandatory fields that hold the same value,
// registered independently at different points in this project's history:
// db/migration.sql declared PurchaseOrder.vendor as a Link, and
// db/migrations_phase3.sql later declared PurchaseOrder.vendor_id as Data.
// Both stuck, both are mandatory, and different consumers read different ones
// (GetVendorLedgerReport reads `vendor`; the payment-file and vendor-invoice
// paths read `vendor_id`), so neither can simply be dropped without breaking
// something - which is exactly why the 2026-07-30 audit found the generic
// record form asking for "Vendor" and "Vendor Code" as two separate required
// boxes with no way to tell them apart.
//
// The fix mirrors 30.6's: the server fills the derived half itself, at the one
// create/update choke point every generic-doc write already passes through,
// and the form is told not to ask. Storage is unchanged - both keys are still
// written, so every existing reader keeps working - and adding another pair
// is one map entry.
//
// This is deliberately a mirror rather than a rename. Renaming would need a
// data migration over every historical document plus a sweep of every reader,
// for a pair whose only real cost is a confusing form. Mirroring removes that
// cost today and leaves the schema cleanup available later.

type mirrorPair struct {
	// Primary is the field a human (or an API caller) actually fills in.
	Primary string
	// Mirror is the duplicate that is filled from Primary and hidden from
	// the generic record form.
	Mirror string
}

var mirroredFields = map[string][]mirrorPair{
	"PurchaseOrder": {{Primary: "vendor", Mirror: "vendor_id"}},
}

// mirroredFieldNames is the set of a doctype's fields that are filled by
// PrepareMirroredFields rather than by the caller. GetDocTypeMeta stamps this
// onto FieldMeta.Mirrored so the generic form drops them, from the same
// registry that populates them - no second list in JavaScript to drift.
func mirroredFieldNames(doctype string) map[string]bool {
	pairs, ok := mirroredFields[doctype]
	if !ok {
		return nil
	}
	out := make(map[string]bool, len(pairs))
	for _, p := range pairs {
		out[p.Mirror] = true
	}
	return out
}

// PrepareMirroredFields copies each registered pair's value across so both
// keys are populated before validation runs (they are mandatory, so this must
// happen first, same ordering and reasoning as PrepareDocumentNumber).
//
// It copies in whichever direction has a value, so an API caller that has
// always sent only `vendor_id` keeps working unchanged - the contract this
// widens rather than narrows. If both are already set and differ, the primary
// wins: the form no longer offers the mirror, so a differing mirror can only
// come from a caller that also sent the primary, and the primary is the one
// the Link constraint validates.
//
// A no-op for every doctype with no registered pair.
func PrepareMirroredFields(doctype string, payload map[string]interface{}) {
	pairs, ok := mirroredFields[doctype]
	if !ok {
		return
	}
	for _, p := range pairs {
		primary := strings.TrimSpace(payloadString(payload, p.Primary))
		mirror := strings.TrimSpace(payloadString(payload, p.Mirror))
		switch {
		case primary != "":
			payload[p.Mirror] = primary
		case mirror != "":
			payload[p.Primary] = mirror
		}
	}
}

// payloadString reads a payload value as a trimmed string, tolerating the
// non-string types a JSON body can produce.
func payloadString(payload map[string]interface{}, key string) string {
	v, ok := payload[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}
