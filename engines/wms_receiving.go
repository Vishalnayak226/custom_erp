package engines

import (
	"custom_erp/db"
	"fmt"
	"strings"
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
func PostGRNReceiptWithQC(tenantID, locationCode string, items []interface{}, userID, grnID string) ([]NegativeStockEvent, error) {
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
	// 42.1.4: lots seen on this receipt, registered after the stock posts. A
	// receipt of a batch-tracked item that names no lot is rejected below
	// before anything is written, so this list only ever contains lots whose
	// stock genuinely arrived.
	type receivedBatch struct {
		info     BatchInfo
		acceptedQty float64
	}
	var receivedBatches []receivedBatch
	// 42.1.8: units seen on this receipt, registered after the stock posts -
	// same ordering and same-file-defence-in-depth reasoning as the batch
	// list above.
	type receivedSerial struct {
		info SerialInfo
	}
	var receivedSerials []receivedSerial

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

		// 42.1.4: batch capture. The gate itself is ValidateReceiptBatchLine,
		// the same function validateGRNRules runs BEFORE the document is
		// written - so on the normal path this call has already passed and is
		// pure defence for a caller that reached this function without going
		// through the document validator. Anything it can reject, it rejects
		// before a single write below.
		batchNo := strField(m, "batch_no")
		if err := ValidateReceiptBatchLine(tenantID, sku, batchNo, strField(m, "expiry_date")); err != nil {
			return nil, err
		}
		// A lot number typed against an item nobody asked to trace is kept
		// rather than dropped - it costs nothing, and switching that item to
		// Batch later then finds real history instead of a blank past.
		if batchNo != "" && accepted > 0 {
			receivedBatches = append(receivedBatches, receivedBatch{
				info: BatchInfo{
					BatchNo:       batchNo,
					Item:          sku,
					MfgDate:       strField(m, "mfg_date"),
					ExpiryDate:    strField(m, "expiry_date"),
					SupplierBatch: strField(m, "supplier_batch"),
				},
				acceptedQty: accepted,
			})
		}

		// 42.1.8: serial capture. Same defence-in-depth call as the batch check
		// above, against the same accepted-qty this function itself derived
		// (not the caller's, which validateGRNRules already checked matches).
		var serialNumbers []string
		if raw, ok := m["serial_numbers"].([]interface{}); ok {
			for _, v := range raw {
				if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
					serialNumbers = append(serialNumbers, strings.TrimSpace(s))
				}
			}
		}
		if err := ValidateReceiptSerialLine(tenantID, sku, serialNumbers, accepted); err != nil {
			return nil, err
		}
		for _, sn := range serialNumbers {
			receivedSerials = append(receivedSerials, receivedSerial{
				info: SerialInfo{SerialNo: sn, Item: sku, BatchNo: batchNo},
			})
		}

		if accepted > 0 {
			// batch_no rides along on the line PostInventoryLedgerWithVoucher
			// already posts, so the receipt produces ONE ledger entry carrying
			// the lot - not a second, quantity-duplicating one. Serial numbers
			// cannot ride along the same way (a line can carry many), so each
			// one gets its own zero-qty ledger entry from RegisterSerial below,
			// once the stock itself has posted.
			acceptedLine := map[string]interface{}{"sku": sku, "qty": accepted}
			if batchNo != "" {
				acceptedLine["batch_no"] = batchNo
			}
			acceptedItems = append(acceptedItems, acceptedLine)
		}
		if rejected > 0 || damaged > 0 {
			qcSplits = append(qcSplits, qcSplit{sku: sku, rejected: rejected, damaged: damaged})
		}
	}

	var negativeEvents []NegativeStockEvent
	if len(acceptedItems) > 0 {
		negativeEvents, err = PostInventoryLedgerWithVoucher(tenantID, locationCode, acceptedItems, false, "GRN", grnID, userID)
		if err != nil {
			return negativeEvents, err
		}

		// Stage 37.3: cost the receipt and post the GRN's real GL entry
		// (Dr 1200 Inventory / Cr 2100 GRN Suspense) - see
		// RecordGRNReceiptCosting's own comment for why this closes a
		// previously-total gap (nothing has ever credited 2100) and why a
		// failure here must never unwind stock that has already posted
		// above, the identical "goods are already in the building" posture
		// the batch/serial registration loops below already take.
		costingLines := make([]struct {
			SKU string
			Qty float64
		}, 0, len(acceptedItems))
		for _, raw := range acceptedItems {
			m, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			sku, _ := m["sku"].(string)
			qty := numFromInterface(m["qty"])
			if sku == "" || qty <= 0 {
				continue
			}
			costingLines = append(costingLines, struct {
				SKU string
				Qty float64
			}{SKU: sku, Qty: qty})
		}
		RecordGRNReceiptCosting(tenantID, schema, grnID, costingLines)
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
		// 26.10.1: on_hand moved (into qc_hold/damaged, not available) for
		// each of these lines, so the stock ledger records it too - two
		// entries per line when both a rejected and a damaged split exist,
		// each tagged with the bucket it landed in via ToStatus.
		for _, s := range qcSplits {
			if s.rejected > 0 {
				if lerr := WriteStockLedgerEntry(tenantID, StockLedgerEntry{
					ItemID: s.sku, WarehouseID: locationCode, Qty: s.rejected,
					VoucherType: "GRN", VoucherID: grnID, UserID: userID, ToStatus: "QC-Hold",
					IdempotencyKey: fmt.Sprintf("GRN:%s:%s:%s:rejected", grnID, locationCode, s.sku),
				}); lerr != nil {
					LogSystemError(tenantID, "", "WARN", "PostGRNReceiptWithQC", fmt.Sprintf("stock ledger write failed for %s rejected split: %v", s.sku, lerr), "")
				}
			}
			if s.damaged > 0 {
				if lerr := WriteStockLedgerEntry(tenantID, StockLedgerEntry{
					ItemID: s.sku, WarehouseID: locationCode, Qty: s.damaged,
					VoucherType: "GRN", VoucherID: grnID, UserID: userID, ToStatus: "Damaged",
					IdempotencyKey: fmt.Sprintf("GRN:%s:%s:%s:damaged", grnID, locationCode, s.sku),
				}); lerr != nil {
					LogSystemError(tenantID, "", "WARN", "PostGRNReceiptWithQC", fmt.Sprintf("stock ledger write failed for %s damaged split: %v", s.sku, lerr), "")
				}
			}
		}
		LogAuditEvent(tenantID, userID, "WMS_GRN_QC_SPLIT", "SUCCESS",
			fmt.Sprintf("Routed QC-sampled receipt qty to qc_hold/damaged buckets at %s for %d line(s)", locationCode, len(qcSplits)))
	}

	// 42.1.4: register the lots last, once the stock they describe has actually
	// posted. The ordering matters and is the reason this is not folded into the
	// loop above: every rejection this function can raise (a batch-tracked line
	// with no lot number, short-dated goods) happens BEFORE any write, and every
	// Batch document created here therefore describes stock that is now
	// genuinely on hand.
	//
	// A failure here is logged rather than returned. The goods are in the
	// building and the receipt has posted, so failing the caller would trip the
	// GRN cancel-on-post-failure path in handlers_core_doc_engine.go and cancel
	// a receipt whose stock had already moved - strictly worse than a batch
	// master that has to be created by hand. The lot is still recoverable: the
	// ledger entry carrying batch_no was written by the accepted-items post
	// above regardless.
	vendor := grnVendor(schema, grnID)
	for _, rb := range receivedBatches {
		rb.info.Vendor = vendor
		if _, err := EnsureBatch(tenantID, rb.info, rb.acceptedQty, userID); err != nil {
			LogSystemError(tenantID, "", "WARN", "PostGRNReceiptWithQC",
				fmt.Sprintf("batch %s of %s could not be registered from GRN %s: %v", rb.info.BatchNo, rb.info.Item, grnID, err), "")
		}
	}
	// 42.1.8: register the units last, same ordering and same logged-not-
	// returned failure handling as the batch loop immediately above, and for
	// the identical reason.
	for _, rs := range receivedSerials {
		rs.info.Vendor = vendor
		if _, err := RegisterSerial(tenantID, rs.info, locationCode, "GRN", grnID, userID); err != nil {
			LogSystemError(tenantID, "", "WARN", "PostGRNReceiptWithQC",
				fmt.Sprintf("serial %s of %s could not be registered from GRN %s: %v", rs.info.SerialNo, rs.info.Item, grnID, err), "")
		}
	}

	return negativeEvents, nil
}

// grnVendor reads the supplier off a GRN so a batch registered from a receipt
// carries who it came from - the first thing a recall asks and the one field a
// receiving clerk should never have to retype. Best-effort: a GRN without a
// vendor (a stock-in with no purchase behind it) simply registers the batch
// without one.
func grnVendor(schema, grnID string) string {
	if grnID == "" {
		return ""
	}
	var vendor string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(data->>'vendor', '') FROM %s.documents WHERE doctype = 'GRN' AND id = $1`, schema),
		grnID).Scan(&vendor); err != nil {
		return ""
	}
	return vendor
}
