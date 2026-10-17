package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
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
		info        BatchInfo
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
	// Stage 57.8: Fixed Asset items never become stock - their accepted units
	// are raised as Draft Assets once the rest of the receipt has posted.
	var assetLines []assetReceiptLine

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

		// Stage 57.8: a Fixed Asset item's accepted units go to the Fixed
		// Assets module, not to available stock, 1200 Inventory, or the batch
		// and serial registers. Units QC set aside still land in qc_hold /
		// damaged below, so a rejected asset can go back on a Purchase Return.
		isAsset, err := isFixedAssetSKU(tenantID, sku)
		if err != nil {
			return nil, err
		}
		if isAsset {
			if accepted > 0 {
				line := assetReceiptLine{SKU: sku, Accepted: int(math.Round(accepted))}
				if raw, ok := m["serial_numbers"].([]interface{}); ok {
					for _, v := range raw {
						if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
							line.Serials = append(line.Serials, strings.TrimSpace(s))
						}
					}
				}
				assetLines = append(assetLines, line)
			}
			if rejected > 0 || damaged > 0 {
				qcSplits = append(qcSplits, qcSplit{sku: sku, rejected: rejected, damaged: damaged})
			}
			continue
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
		// Two lines of one SKU on one lot (GRN batch numbers are per SKU,
		// Stage 61.1) register one Batch whose received qty is their sum.
		merged := false
		for i := range receivedBatches {
			if batchNo != "" && accepted > 0 && receivedBatches[i].info.Item == sku && receivedBatches[i].info.BatchNo == batchNo {
				receivedBatches[i].acceptedQty += accepted
				merged = true
				break
			}
		}
		if batchNo != "" && accepted > 0 && !merged {
			receivedBatches = append(receivedBatches, receivedBatch{
				info: BatchInfo{
					BatchNo:       batchNo,
					Item:          sku,
					MfgDate:       strField(m, "mfg_date"),
					ExpiryDate:    strField(m, "expiry_date"),
					SupplierBatch: strField(m, "supplier_batch"),
					AutoNumbered:  m["batch_auto"] == true,
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
		rb.info.GRN = grnID
		rb.info.Location = locationCode
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

	// Stage 57.8: logged, not returned, for the same reason as the two loops
	// above - the rest of the receipt has posted, and failing here would
	// cancel it. The ERROR names the GRN so the asset can be entered by hand.
	if len(assetLines) > 0 {
		if _, err := createAssetsFromReceipt(tenantID, schema, grnID, locationCode, userID, assetLines); err != nil {
			LogSystemError(tenantID, "", "ERROR", "PostGRNReceiptWithQC",
				fmt.Sprintf("GRN %s: fixed asset(s) could not be raised in the Fixed Assets module: %v", grnID, err), "")
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
	// A GRN made from the screen carries no vendor of its own - it is the
	// PO's - so fall back to the PO, or every lot's Supplier stays blank.
	var vendor string
	if err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(NULLIF(g.data->>'vendor', ''), po.data->>'vendor', '')
		FROM %s.documents g
		LEFT JOIN %s.documents po ON po.doctype = 'PurchaseOrder' AND po.id = g.data->>'po_id'
		WHERE g.doctype = 'GRN' AND g.id = $1`, schema, schema),
		grnID).Scan(&vendor); err != nil {
		return ""
	}
	return vendor
}

// GRN batch numbers (Stage 61). The user: "In each GRN, each Combination
// ID/SKU will have a batch number" - so a SKU whose barcode stays the same on
// every receipt can still be told apart by when it came in, and a scan of the
// batch answers "which day did this arrive, how old is it" (the batch-ageing
// reports in traceability_reports.go).
//
// How often a number is issued is a rule like the barcode rules (61.6): the
// client setting inventory.grn_batch_number, overridden by the Item Type and
// then the Item (EffectiveGRNBatchMode). What it looks like is the GRNBatchSeq
// number series (61.7), edited on the Number Series screen like any other:
// prefix, store, period, number and suffix joined by the separator, where the
// period is the receipt date - B-261015-0001 by default.

// GRN batch numbering modes.
const (
	GRNBatchPerGRNSKU = "per_grn_sku" // one per SKU on each GRN (default)
	GRNBatchPerLine   = "per_grn_line"
	GRNBatchPerSKUDay = "per_sku_day" // one per SKU per day, shared by that day's GRNs
	GRNBatchOff       = "off"
)

// GRNBatchMode is the client-wide numbering mode.
func GRNBatchMode(tenantID string) string {
	switch v := GetSettingString(tenantID, "inventory.grn_batch_number"); v {
	case GRNBatchPerLine, GRNBatchPerSKUDay, GRNBatchOff:
		return v
	default:
		return GRNBatchPerGRNSKU
	}
}

// grnBatchPeriodKey is the period GenerateSequence prints and counts by: the
// receipt date for a daily series (shown as ANNUAL in prefix_configs, since a
// batch series restarts with its date), year-month for MONTHLY, none for NEVER.
func grnBatchPeriodKey(tenantID string, day time.Time) string {
	reset := "ANNUAL"
	if schema, err := db.GetTenantSchema(tenantID); err == nil {
		_ = db.DB.QueryRow(fmt.Sprintf(
			`SELECT COALESCE(reset_frequency, 'ANNUAL') FROM %s.prefix_configs WHERE doc_type = 'GRNBatchSeq'`, schema)).Scan(&reset)
	}
	switch strings.ToUpper(strings.TrimSpace(reset)) {
	case "NEVER":
		return ""
	case "MONTHLY":
		return day.Format("06") // sequencePeriod adds "-MM"
	default:
		return day.Format("060102")
	}
}

// GenerateGRNBatchNumber draws the next GRN batch number for a receipt at
// storeCode (used only when the series puts the store in the number).
func GenerateGRNBatchNumber(tenantID, storeCode string, day time.Time) (string, error) {
	return GenerateSequence(tenantID, "GRNBatchSeq", storeCode, grnBatchPeriodKey(tenantID, day))
}

// todaysAutoBatch finds the batch number already issued today for this SKU
// at this location by per-SKU-per-day numbering, if any.
func todaysAutoBatch(tenantID, sku, location string, day time.Time) string {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return ""
	}
	var batchNo string
	_ = db.DB.QueryRow(fmt.Sprintf(`
		SELECT data->>'batch_no' FROM %s.documents
		WHERE doctype = 'Batch' AND deleted_at IS NULL AND data->>'item' = $1
		  AND data->>'auto_numbered' = 'Yes' AND data->>'received_on' = $2
		  AND COALESCE(data->>'location', '') = $3
		ORDER BY created_at DESC LIMIT 1`, schema), sku, localDateString(day), location).Scan(&batchNo)
	return batchNo
}

// PrepareGRNBatchNumbers gives the blank lines of a new GRN their batch
// numbers, before the document is written so the GRN itself, its stickers and
// its stock posting all carry the same value. A typed number is never
// replaced; under the per-SKU modes it also covers that SKU's other blank
// lines. A line numbered here is marked batch_auto, which the Batch record
// keeps (auto_numbered) so per-SKU-per-day numbering can find the day's batch.
//
// Never numbered: a batch-tracked item (Item Traceability = Batch) keeps its
// rule that the clerk types the lot printed on the carton (INVENT-0115) - a
// control the business switched on for that item, where an invented number
// would hide a missed entry; and a Fixed Asset item, which never enters stock
// or the batch register (Stage 57.8).
func PrepareGRNBatchNumbers(tenantID string, isCreate bool, payload map[string]interface{}) error {
	if !isCreate {
		return nil
	}
	raw, ok := payload["received_items"].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return nil
	}
	var lines []interface{}
	if err := json.Unmarshal([]byte(raw), &lines); err != nil {
		return nil // the create handler reports malformed received_items itself
	}
	location := strings.TrimSpace(strField(payload, "location"))

	typed := map[string]string{}
	for _, l := range lines {
		m, ok := l.(map[string]interface{})
		if !ok {
			continue
		}
		sku := strings.TrimSpace(strField(m, "sku"))
		if b := strings.TrimSpace(strField(m, "batch_no")); sku != "" && b != "" && typed[sku] == "" {
			typed[sku] = b
		}
	}

	day := time.Now()
	modes := map[string]string{}
	shared := map[string]string{}
	changed := false
	for _, l := range lines {
		m, ok := l.(map[string]interface{})
		if !ok {
			continue
		}
		sku := strings.TrimSpace(strField(m, "sku"))
		if sku == "" || strings.TrimSpace(strField(m, "batch_no")) != "" {
			continue
		}
		mode, seen := modes[sku]
		if !seen {
			var err error
			if mode, err = grnLineBatchMode(tenantID, sku); err != nil {
				return err
			}
			modes[sku] = mode
		}
		var code string
		switch mode {
		case GRNBatchOff:
			continue
		case GRNBatchPerLine:
			c, err := GenerateGRNBatchNumber(tenantID, location, day)
			if err != nil {
				return err
			}
			code = c
		default: // per GRN x SKU, per SKU per day
			if code = typed[sku]; code == "" {
				code = shared[sku]
			}
			if code == "" && mode == GRNBatchPerSKUDay {
				code = todaysAutoBatch(tenantID, sku, location, day)
			}
			if code == "" {
				c, err := GenerateGRNBatchNumber(tenantID, location, day)
				if err != nil {
					return err
				}
				code = c
			}
			shared[sku] = code
		}
		m["batch_no"] = code
		if code != typed[sku] {
			m["batch_auto"] = true
		}
		changed = true
	}
	if !changed {
		return nil
	}
	out, err := json.Marshal(lines)
	if err != nil {
		return err
	}
	payload["received_items"] = string(out)
	return nil
}

// grnLineBatchMode is how a SKU's blank GRN lines are numbered: off for a
// batch-tracked or Fixed Asset item, else the Item / Item Type / client rule.
func grnLineBatchMode(tenantID, sku string) (string, error) {
	tracking, err := GetItemTracking(tenantID, sku)
	if err != nil {
		return "", err
	}
	if tracking.TracksBatch() {
		return GRNBatchOff, nil
	}
	item, err := ResolveItemBySKU(tenantID, sku)
	if errors.Is(err, ErrItemNotFound) {
		return GRNBatchMode(tenantID), nil // the GRN's own item check reports it
	} else if err != nil {
		return "", err
	}
	if IsFixedAssetItem(tenantID, item.Data) {
		return GRNBatchOff, nil
	}
	return EffectiveGRNBatchMode(tenantID, item.Data), nil
}
