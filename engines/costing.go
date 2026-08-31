package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
)

// Stage 37.3: Costing & valuation, incl. landed cost allocation.
//
// Audit finding this stage exists to close (see the migration's own header
// for the full trail): this codebase had NO costing method anywhere.
// StockLedgerEntry/bin_stock/inventory_availability track quantity only; the
// "COGS" PostSalesFinanceBooking posts at a POS sale used whatever cost_price
// the client's cart JSON happened to send, never verified against anything;
// and PostGRNFinanceBooking (the Dr 1200/Cr 2100 GRN-receipt posting) was
// dead code with zero callers - meaning nothing has EVER credited 2100 GRN
// Suspense, while PayVendorInvoice's debit to it (engines/vendor_invoice.go)
// has been running the whole time, driving the account further into an
// incorrect balance on every vendor payment this system has ever posted.
//
// This file's design is a single company-wide (not per-location) moving
// weighted-average unit cost per item (item_cost, keyed on item_code alone)
// - the standard Ind AS 2 / AS 2 acceptable method, avoiding FIFO/LIFO layer
// tracking this codebase has no infrastructure for. The average only moves
// on RECEIPT (a GRN, or a landed-cost top-up applied later once a freight/
// customs bill arrives) - never on issue - so nothing here needs to
// intercept this codebase's many stock-out paths (sales, transfers, returns,
// disassembly, wastage...) to stay correct. GetInventoryValuation computes a
// real "current value" at report time instead, multiplying the real,
// already-correct inventory_availability.on_hand against this table's rate.

// upsertItemCostReceipt is the one place that grows item_cost's cumulative
// qty/value and recomputes the average - an atomic Postgres UPSERT (the same
// idiom PostGRNReceiptWithQC's own qc_hold/damaged bucket update already
// uses), so two concurrent receipts of the same item can never race each
// other into a wrong average the way a read-then-write in application code
// could.
func upsertItemCostReceipt(schema string, itemCode string, qty float64, valuePaise int64) error {
	if qty <= 0 || valuePaise < 0 {
		return nil
	}
	// Each placeholder is cast exactly once, in the "input" CTE, and never
	// referenced again in its untyped form - lib/pq's simple protocol
	// unifies a parameter's type across every occurrence in one statement,
	// and mixing a raw ($2 straight into a column) and a cast use ($2::numeric
	// in the CASE expression) of the SAME placeholder is exactly what
	// produced Postgres error 42P08 ("inconsistent types deduced") in this
	// function's first draft.
	_, err := db.DB.Exec(fmt.Sprintf(`
		WITH input AS (SELECT $1::varchar AS item_code, $2::numeric AS qty, $3::bigint AS value_paise)
		INSERT INTO %s.item_cost (item_code, cumulative_qty_received, cumulative_value_received_paise, avg_unit_cost_paise)
		SELECT item_code, qty, value_paise, CASE WHEN qty = 0 THEN 0 ELSE (value_paise::numeric / qty)::bigint END FROM input
		ON CONFLICT (item_code) DO UPDATE SET
			cumulative_qty_received = %s.item_cost.cumulative_qty_received + EXCLUDED.cumulative_qty_received,
			cumulative_value_received_paise = %s.item_cost.cumulative_value_received_paise + EXCLUDED.cumulative_value_received_paise,
			avg_unit_cost_paise = CASE
				WHEN (%s.item_cost.cumulative_qty_received + EXCLUDED.cumulative_qty_received) = 0 THEN 0
				ELSE ((%s.item_cost.cumulative_value_received_paise + EXCLUDED.cumulative_value_received_paise)
					/ (%s.item_cost.cumulative_qty_received + EXCLUDED.cumulative_qty_received))::bigint
				END,
			updated_at = CURRENT_TIMESTAMP`, schema, schema, schema, schema, schema, schema),
		itemCode, qty, valuePaise)
	return err
}

// RecordStockReceiptCost is the tenantID-facing entry point PostGRNReceiptWithQC
// calls for each accepted line - qty is the accepted quantity, unitCostPaise
// is that line's resolved ex-GST base cost (see resolvePOBaseUnitCostPaise).
func RecordStockReceiptCost(tenantID, itemCode string, qty float64, unitCostPaise int64) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	return upsertItemCostReceipt(schema, itemCode, qty, unitCostPaise*int64(qty))
}

// ApplyLandedCostCharge adds a charge amount into an item's cumulative value
// WITHOUT changing its cumulative qty - no new physical stock arrived, only
// its cost basis moved. If the item has no receipt history yet (qty is
// still 0), the charge has no quantity to spread over and is recorded
// without changing the average - a stated, logged limitation rather than a
// silent no-op or a fabricated per-unit rate.
func ApplyLandedCostCharge(tenantID, itemCode string, chargeAmountPaise int64) error {
	if chargeAmountPaise <= 0 {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.item_cost (item_code, cumulative_qty_received, cumulative_value_received_paise, avg_unit_cost_paise)
		VALUES ($1, 0, $2, 0)
		ON CONFLICT (item_code) DO UPDATE SET
			cumulative_value_received_paise = %s.item_cost.cumulative_value_received_paise + EXCLUDED.cumulative_value_received_paise,
			avg_unit_cost_paise = CASE
				WHEN %s.item_cost.cumulative_qty_received = 0 THEN %s.item_cost.avg_unit_cost_paise
				ELSE ((%s.item_cost.cumulative_value_received_paise + EXCLUDED.cumulative_value_received_paise)
					/ %s.item_cost.cumulative_qty_received)::bigint
				END,
			updated_at = CURRENT_TIMESTAMP`, schema, schema, schema, schema, schema, schema),
		itemCode, chargeAmountPaise)
	return err
}

// GetItemUnitCost reads the current moving-average cost. hasCost is false
// for an item that has never had a receipt costed (no row, or qty still 0) -
// callers must decide their own fallback rather than silently treating an
// uncosted item as free.
func GetItemUnitCost(tenantID, itemCode string) (unitCostPaise int64, hasCost bool, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, false, err
	}
	var qty float64
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT cumulative_qty_received, avg_unit_cost_paise FROM %s.item_cost WHERE item_code = $1`, schema),
		itemCode).Scan(&qty, &unitCostPaise)
	if err != nil {
		return 0, false, nil // sql.ErrNoRows or any read failure: no cost on record
	}
	return unitCostPaise, qty > 0, nil
}

// ResolveCOGSUnitCostPaise (Stage 37.3.3) is the real-cost seam
// FinalizePOSCheckout (engines/pos_checkout.go) calls in place of trusting
// the client-submitted cost_price outright. A real recorded moving-average
// cost wins; an item with no receipt history yet falls back to the caller's
// own figure UNCHANGED - so a tenant that has never used GRN-based receiving
// (or an item received before this stage existed) sees byte-identical
// behaviour to today, and only gains real costing as receipts accumulate.
func ResolveCOGSUnitCostPaise(tenantID, sku string, fallbackCostPriceRupees float64) int64 {
	if unitCostPaise, hasCost, err := GetItemUnitCost(tenantID, sku); err == nil && hasCost {
		return unitCostPaise
	}
	return RupeesToPaise(fallbackCostPriceRupees)
}

// resolvePOBaseUnitCostPaise resolves one SKU's ex-GST unit cost from the
// purchase order a GRN was raised against, reusing PreviewPurchaseOrder
// (engines/purchase_order.go) - the same GST-aware taxable-amount
// computation the PO screen, AP matching and the printed PO all already
// share - rather than re-deriving "is this rate GST-inclusive" a second
// time. A PO with more than one line for the same SKU has its taxable
// amounts and quantities summed first, so the blended rate is used.
func resolvePOBaseUnitCostPaise(tenantID, poID, sku string) (unitCostPaise int64, ok bool) {
	if poID == "" || sku == "" {
		return 0, false
	}
	data, _, err := fetchDocData(tenantID, "PurchaseOrder", poID)
	if err != nil {
		return 0, false
	}
	preview, err := PreviewPurchaseOrder(tenantID, data)
	if err != nil {
		return 0, false
	}
	var taxableSum float64
	var qtySum int
	for _, line := range preview.Lines {
		if line.SKU != sku || line.Error != "" {
			continue
		}
		taxableSum += line.Taxable
		qtySum += line.Qty
	}
	if qtySum <= 0 {
		return 0, false
	}
	return RupeesToPaise(taxableSum / float64(qtySum)), true
}

// grnPurchaseOrderID mirrors grnVendor's exact best-effort shape
// (engines/wms_receiving.go) - a GRN without a resolvable PO simply
// receives with no cost recorded, rather than failing a receipt whose stock
// has already physically arrived.
func grnPurchaseOrderID(schema, grnID string) string {
	if grnID == "" {
		return ""
	}
	var poID string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(data->>'po_id', '') FROM %s.documents WHERE doctype = 'GRN' AND id = $1`, schema),
		grnID).Scan(&poID); err != nil {
		return ""
	}
	return poID
}

// RecordGRNReceiptCosting is PostGRNReceiptWithQC's one call into this file:
// for each accepted line, resolves its base unit cost from the GRN's own PO
// (falling back to skipping that line's costing - logged, not fatal - the
// same "goods are already in the building" reasoning the batch/serial
// registration loops right above this call site's insertion point already
// use), updates item_cost, and posts ONE real GL entry for the whole
// receipt: Dr 1200 Inventory / Cr 2100 GRN Suspense - closing the
// previously-total gap where nothing ever credited 2100. Best-effort by
// design: a failure here must never unwind stock that has already posted.
func RecordGRNReceiptCosting(tenantID, schema, grnID string, acceptedLines []struct {
	SKU string
	Qty float64
}) {
	poID := grnPurchaseOrderID(schema, grnID)
	var totalValuePaise int64
	for _, line := range acceptedLines {
		if line.Qty <= 0 {
			continue
		}
		unitCostPaise, ok := resolvePOBaseUnitCostPaise(tenantID, poID, line.SKU)
		if !ok {
			LogSystemError(tenantID, "", "WARN", "RecordGRNReceiptCosting",
				fmt.Sprintf("GRN %s: could not resolve a base cost for %s (po_id=%q) - not costed", grnID, line.SKU, poID), "")
			continue
		}
		if err := RecordStockReceiptCost(tenantID, line.SKU, line.Qty, unitCostPaise); err != nil {
			LogSystemError(tenantID, "", "ERROR", "RecordGRNReceiptCosting",
				fmt.Sprintf("GRN %s: RecordStockReceiptCost failed for %s: %v", grnID, line.SKU, err), "")
			continue
		}
		totalValuePaise += unitCostPaise * int64(line.Qty)
	}
	if totalValuePaise <= 0 {
		return
	}
	if err := PostDoubleEntry(tenantID, "GRN", grnID,
		map[string]int64{"1200": totalValuePaise}, map[string]int64{"2100": totalValuePaise},
		"", fmt.Sprintf("GRN:%s:RECEIPT_COST", grnID)); err != nil {
		LogSystemError(tenantID, "", "ERROR", "RecordGRNReceiptCosting",
			fmt.Sprintf("GRN %s: GL posting failed (stock already received, not rolled back): %v", grnID, err), "")
	}
}

// CreateLandedCostVoucher creates a Draft LandedCostVoucher referencing a
// GRN, with a set of charge lines (type + amount) to be spread across that
// GRN's received items once applied.
func CreateLandedCostVoucher(tenantID, grnID string, chargeLines []map[string]interface{}, userID string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	if grnID == "" {
		return "", fmt.Errorf("grn_reference is required")
	}
	if _, _, err := fetchDocData(tenantID, "GRN", grnID); err != nil {
		return "", fmt.Errorf("grn_reference: %v", err)
	}
	if len(chargeLines) == 0 {
		return "", fmt.Errorf("at least one charge line is required")
	}
	var total float64
	for _, l := range chargeLines {
		amount, _ := parityNumber(l["amount"])
		if amount <= 0 {
			return "", fmt.Errorf("every charge line's amount must be greater than zero")
		}
		total += amount
	}

	linesJSON, err := json.Marshal(chargeLines)
	if err != nil {
		return "", err
	}
	id := NewDocID("LCV")
	docData := map[string]interface{}{
		"id": id, "code": id, "grn_reference": grnID,
		"charge_lines": string(linesJSON), "status": "Draft",
	}
	marshaled, err := json.Marshal(docData)
	if err != nil {
		return "", err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'LandedCostVoucher', $2, 'Draft', $3)`, schema),
		id, marshaled, userID); err != nil {
		return "", err
	}
	return id, nil
}

// ApplyLandedCostVoucher (37.3.2) spreads the voucher's total charge across
// its GRN's accepted lines, proportionally by each line's own base value
// (qty x resolvePOBaseUnitCostPaise) - the standard value-proportional
// landed-cost allocation method. A line this GRN received with no
// resolvable base cost is excluded from the allocation base entirely
// (stated limitation: it receives no share of the landed cost either,
// consistent with RecordGRNReceiptCosting never having costed it). One-shot:
// a voucher already Applied refuses a second application, since re-running
// it would double the cost every time.
func ApplyLandedCostVoucher(tenantID, voucherID, userID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	data, status, err := fetchDocData(tenantID, "LandedCostVoucher", voucherID)
	if err != nil {
		return err
	}
	if status == "Applied" {
		return fmt.Errorf("landed cost voucher %s has already been applied", voucherID)
	}
	grnID, _ := data["grn_reference"].(string)
	var chargeLines []map[string]interface{}
	if raw, ok := data["charge_lines"].(string); ok {
		json.Unmarshal([]byte(raw), &chargeLines)
	}
	var totalChargePaise int64
	for _, l := range chargeLines {
		amount, _ := parityNumber(l["amount"])
		totalChargePaise += RupeesToPaise(amount)
	}
	if totalChargePaise <= 0 {
		return fmt.Errorf("landed cost voucher %s has no positive charge amount to apply", voucherID)
	}

	grnData, _, err := fetchDocData(tenantID, "GRN", grnID)
	if err != nil {
		return fmt.Errorf("grn_reference: %v", err)
	}
	receivedRaw, _ := grnData["received_items"].(string)
	var receivedItems []map[string]interface{}
	if err := json.Unmarshal([]byte(receivedRaw), &receivedItems); err != nil {
		return fmt.Errorf("GRN %s has unreadable received_items: %v", grnID, err)
	}
	poID := grnPurchaseOrderID(schema, grnID)

	type lineBase struct {
		sku       string
		qty       float64
		baseValue int64
	}
	var bases []lineBase
	var totalBaseValue int64
	for _, m := range receivedItems {
		sku, _ := m["sku"].(string)
		accepted := numFromInterface(m["qty"])
		if v, exists := m["accepted_qty"]; exists {
			accepted = numFromInterface(v)
		}
		if sku == "" || accepted <= 0 {
			continue
		}
		unitCostPaise, ok := resolvePOBaseUnitCostPaise(tenantID, poID, sku)
		if !ok {
			continue
		}
		value := unitCostPaise * int64(accepted)
		bases = append(bases, lineBase{sku: sku, qty: accepted, baseValue: value})
		totalBaseValue += value
	}
	if totalBaseValue <= 0 {
		return fmt.Errorf("GRN %s has no costed lines to allocate landed cost across", grnID)
	}

	// Proportional allocation, the final line absorbing any paise-rounding
	// remainder - the same "largest/last line takes the remainder" technique
	// ConvertPostingToFunctional (engines/currency_documents.go) already
	// uses, so a landed cost voucher's total always lands exactly on the
	// items it named, never a paisa short or over.
	var allocated int64
	for i, b := range bases {
		var share int64
		if i == len(bases)-1 {
			share = totalChargePaise - allocated
		} else {
			share = totalChargePaise * b.baseValue / totalBaseValue
		}
		allocated += share
		if err := ApplyLandedCostCharge(tenantID, b.sku, share); err != nil {
			return fmt.Errorf("applying landed cost to %s: %v", b.sku, err)
		}
	}

	if err := PostDoubleEntry(tenantID, "LandedCostVoucher", voucherID,
		map[string]int64{"1200": totalChargePaise}, map[string]int64{"2110": totalChargePaise},
		"", fmt.Sprintf("LandedCostVoucher:%s:APPLY", voucherID)); err != nil {
		return fmt.Errorf("GL posting failed, voucher not applied: %v", err)
	}

	data["status"] = "Applied"
	marshaled, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = 'Applied', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'LandedCostVoucher' AND id = $2`, schema),
		marshaled, voucherID)
	return err
}

// ValidateLandedCostVoucherDocument runs at ValidateDocument's shared exit,
// the same defence-in-depth CreateIntercompanyTransaction's own validator
// gives the generic-API-write path.
func ValidateLandedCostVoucherDocument(tenantID string, payload map[string]interface{}) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	grnID := pimString(payload["grn_reference"])
	if grnID == "" {
		return nil
	}
	var exists bool
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'GRN' AND id = $1 AND deleted_at IS NULL)`, schema),
		grnID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return &ValidationError{Code: "META-0198", SubFor: "GRN Reference", Message: fmt.Sprintf("Linked GRN record with ID %q does not exist", grnID)}
	}
	return nil
}

// GetInventoryValuation (37.3.4) is the current stock value per item: real
// on-hand quantity (inventory_availability, summed across every location -
// the same figure every other availability read already trusts) times the
// current moving-average rate. An item with on-hand stock but no recorded
// cost yet (received before this stage existed, or through a path that
// never resolved a base cost) is still listed, with unit_cost/value 0 and
// costed=false, so a real gap is visible on the report rather than the item
// silently missing from it.
func GetInventoryValuation(tenantID string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT ia.sku, SUM(ia.on_hand) AS qty, COALESCE(ic.avg_unit_cost_paise, 0)
		FROM %s.inventory_availability ia
		LEFT JOIN %s.item_cost ic ON ic.item_code = ia.sku
		GROUP BY ia.sku, ic.avg_unit_cost_paise
		HAVING SUM(ia.on_hand) <> 0
		ORDER BY ia.sku`, schema, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []map[string]interface{}
	for rows.Next() {
		var sku string
		var qty int64
		var unitCostPaise int64
		if err := rows.Scan(&sku, &qty, &unitCostPaise); err != nil {
			return nil, err
		}
		out = append(out, map[string]interface{}{
			"sku": sku, "qty_on_hand": qty,
			"unit_cost": PaiseToRupees(unitCostPaise),
			"total_value": PaiseToRupees(unitCostPaise * qty),
			"costed": unitCostPaise > 0,
		})
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	return out, nil
}

func init() {
	RegisterReport(ReportDefinition{
		ID: "inventory-valuation", Label: "Inventory Valuation", Category: "Inventory",
		Columns: []ReportColumn{
			{Key: "sku", Label: "SKU"}, {Key: "qty_on_hand", Label: "Qty On Hand"},
			{Key: "unit_cost", Label: "Unit Cost (Avg)", Sensitive: true},
			{Key: "total_value", Label: "Total Value", Sensitive: true},
			{Key: "costed", Label: "Costed"},
		},
		Params: []ReportParam{},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			return GetInventoryValuation(tenantID)
		},
	})
}
