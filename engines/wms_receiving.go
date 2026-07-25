package engines

import (
	"custom_erp/db"
	"fmt"
)

// Stage 26.5 (WMS Enterprise Maturity Sprint): 26.5.1 ASN capture ahead of a
// GRN, and 26.5.2 QC sampling (accept/reject/damage sub-quantities) actually
// driving *differential* stock posting instead of a GRN's whole received
// qty always landing in `available` regardless of what the received_items
// JSON's accepted_qty/rejected_qty/damaged_qty fields say. Layered next to
// engines/wms.go (Stage 20 Track B.2) and engines/transactional_validation.go
// (grnReceivedLine, GOODSR-0089/0090) rather than merged into either: this
// file owns *posting effect*, the validation file owns *field-shape
// checking*, wms.go owns bin-level putaway - three different concerns that
// already lived separately before this Stage.

// PostGRNReceiptWithQC (26.5.2) is the QC-aware replacement for calling
// PostInventoryLedger directly from the GRN create hook
// (handlers_core_doc_engine.go). For each received_items line:
//   - accepted_qty (defaulting to the full qty when the line never set an
//     accepted/rejected/damaged split at all - the pre-26.5 whole-line-accept
//     behavior, so any existing caller that only ever sends {sku, qty} keeps
//     posting exactly as it always has) flows into `available` via the
//     existing PostInventoryLedger path.
//   - rejected_qty flows into inventory_availability.qc_hold (held for a QC
//     disposition decision) and damaged_qty into .damaged - both real ATP
//     buckets computeATS already reads (Stage 26.12 foundation) but that
//     nothing wrote to before this function existed.
//
// on_hand increases by the FULL received qty in every case - all of it is
// physically in the building the moment it's received, regardless of
// disposition; only `available` reflects the accepted portion.
func PostGRNReceiptWithQC(tenantID, locationCode string, items []interface{}, userID string) ([]NegativeStockEvent, error) {
	if locationCode == "" || len(items) == 0 {
		return nil, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	// acceptedItems is handed to the existing, already-tested
	// PostInventoryLedger exactly as before - accepted stock is the only
	// portion that should ever land in `available`.
	var acceptedItems []interface{}
	type qcSplit struct {
		sku      string
		rejected float64
		damaged  float64
	}
	var qcSplits []qcSplit

	for _, raw := range items {
		m, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		sku, _ := m["sku"].(string)
		if sku == "" {
			continue
		}
		qty := numFromInterface(m["qty"])
		rejected := 0.0
		if v, exists := m["rejected_qty"]; exists {
			rejected = numFromInterface(v)
		}
		damaged := 0.0
		if v, exists := m["damaged_qty"]; exists {
			damaged = numFromInterface(v)
		}
		accepted := qty - rejected - damaged
		if v, exists := m["accepted_qty"]; exists {
			// An explicit accepted_qty (already this repo's UI convention,
			// public/app.js's GRN Workbench) wins over the derived value -
			// GOODSR-0089 already guarantees it can't exceed qty.
			accepted = numFromInterface(v)
		}
		if accepted < 0 {
			accepted = 0
		}

		if accepted > 0 {
			acceptedItems = append(acceptedItems, map[string]interface{}{"sku": sku, "qty": accepted})
		}
		if rejected > 0 || damaged > 0 {
			qcSplits = append(qcSplits, qcSplit{sku: sku, rejected: rejected, damaged: damaged})
		}
	}

	var negativeEvents []NegativeStockEvent
	if len(acceptedItems) > 0 {
		negativeEvents, err = PostInventoryLedger(tenantID, locationCode, acceptedItems, false)
		if err != nil {
			return negativeEvents, err
		}
	}

	if len(qcSplits) > 0 {
		tx, err := db.DB.Begin()
		if err != nil {
			return negativeEvents, err
		}
		defer tx.Rollback()
		if err := db.SetSearchPath(tx, schema); err != nil {
			return negativeEvents, err
		}
		for _, s := range qcSplits {
			onHandDelta := s.rejected + s.damaged
			if _, err := tx.Exec(fmt.Sprintf(`
				INSERT INTO %s.inventory_availability (sku, location_code, on_hand, available, qc_hold, damaged)
				VALUES ($1, $2, $3, 0, $4, $5)
				ON CONFLICT (sku, location_code) DO UPDATE SET
					on_hand = %s.inventory_availability.on_hand + EXCLUDED.on_hand,
					qc_hold = %s.inventory_availability.qc_hold + EXCLUDED.qc_hold,
					damaged = %s.inventory_availability.damaged + EXCLUDED.damaged,
					updated_at = CURRENT_TIMESTAMP`, schema, schema, schema, schema),
				s.sku, locationCode, int(onHandDelta), int(s.rejected), int(s.damaged)); err != nil {
				return negativeEvents, err
			}
		}
		if err := tx.Commit(); err != nil {
			return negativeEvents, err
		}
		LogAuditEvent(tenantID, userID, "WMS_GRN_QC_SPLIT", "SUCCESS",
			fmt.Sprintf("Routed QC-sampled receipt qty to qc_hold/damaged buckets at %s for %d line(s)", locationCode, len(qcSplits)))
	}

	return negativeEvents, nil
}
