package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
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
//
// Stage 148.1: the average is of the stock in hand, not of everything ever
// received: each receipt blends with the units held at that moment. See
// moving_cost.go.

// RecordStockReceiptCost costs a receipt whose stock has already posted - qty
// is the accepted quantity, unitCostPaise is that line's resolved ex-GST base
// cost (see resolvePOBaseUnitCostPaise).
func RecordStockReceiptCost(tenantID, itemCode string, qty float64, unitCostPaise int64) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := blendIntoCostTx(tx, schema, itemCode, qty, unitCostPaise*int64(qty), qty, true); err != nil {
		return err
	}
	return tx.Commit()
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
		qtySum += line.Qty + line.FreeQty
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

// recordGRNReceiptCostingPartTx costs the accepted lines (item_cost) and posts
// the receipt's GL entry (Dr 1200 or the item's expense account / Cr 2100) in
// the caller's transaction - an error fails the caller. A line whose base cost
// cannot be resolved is still left uncosted with a warning, as before: that is
// missing data on the PO / GRN, not a failure.
func recordGRNReceiptCostingPartTx(tx *sql.Tx, tenantID, schema, grnID, part string, acceptedLines []struct {
	SKU string
	Qty float64
}) error {
	poID := grnPurchaseOrderID(schema, grnID)
	// Stage 62: a priced GRN costs at the vendor's invoice rate (stamped on
	// its lines, multi-PO aware); an older GRN still costs from its PO.
	grnCosts := grnLineUnitCostsPaise(tenantID, grnID)
	// 148.57: stock takes whole units per line (as the hold does, 148.56), so
	// costing does too: a part unit adds no value and no quantity to the average.
	whole := acceptedLines[:0:0]
	for _, l := range acceptedLines {
		if l.Qty = math.Trunc(l.Qty); l.Qty > 0 {
			whole = append(whole, l)
		}
	}
	if handled, err := memoReceiptTx(tx, tenantID, schema, grnID, part, whole, grnCosts, poID); err != nil { // 148.16
		return err
	} else if handled {
		return nil
	}
	var totalValuePaise int64
	var inventoryLines []groupLine
	// Stage 72.10: a non-valuated or non-stock item is expensed at receipt -
	// its value goes to its profile's expense account, not 1200 Inventory,
	// and it never enters the moving average (an issue costs it at zero).
	expenseByAccount := map[string]int64{}
	// Stage 148.8: every way into stock comes through here, so charges hook here.
	var chargeLines []receiptChargeLine
	// Stage 148.1: the part's stock has already posted, so the units still to be
	// costed here come off the stock in hand each line blends with. SKU order, for
	// the item_cost locks.
	lines := whole
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].SKU < lines[j].SKU })
	stockIn := map[string]float64{}
	for _, line := range lines {
		stockIn[line.SKU] += max(line.Qty, 0)
	}
	// 170.1: the GRN line each SKU came in on, when the GL names source lines.
	lineNo := grnSKULineNo(tx, schema, grnID)
	var invShares, allShares []sourceLineShare
	expShares := map[string][]sourceLineShare{}
	share := func(sku string, v int64) sourceLineShare {
		if n := lineNo[sku]; n > 0 {
			return sourceLineShare{ID: lineNoID(n), Weight: v}
		}
		return sourceLineShare{Weight: v}
	}
	for _, line := range lines {
		if line.Qty <= 0 {
			continue
		}
		unitCostPaise, ok := grnCosts[line.SKU]
		if !ok {
			unitCostPaise, ok = resolvePOBaseUnitCostPaise(tenantID, poID, line.SKU)
		}
		if !ok {
			LogSystemError(tenantID, "", "WARN", "RecordGRNReceiptCosting",
				fmt.Sprintf("GRN %s: could not resolve a base cost for %s (po_id=%q) - not costed", grnID, line.SKU, poID), "")
			stockIn[line.SKU] -= line.Qty
			chargeLines = append(chargeLines, receiptChargeLine{SKU: line.SKU, Qty: line.Qty})
			continue
		}
		chargeLines = append(chargeLines, receiptChargeLine{SKU: line.SKU, Qty: line.Qty, Value: unitCostPaise * int64(line.Qty), Costed: true})
		if lineNo != nil {
			allShares = append(allShares, share(line.SKU, unitCostPaise*int64(line.Qty)))
		}
		if policy, _, known := ItemPolicyForSKU(tenantID, line.SKU); known && !policy.Valuated() {
			expenseByAccount[policy.ExpenseAccount] += unitCostPaise * int64(line.Qty)
			if lineNo != nil {
				expShares[policy.ExpenseAccount] = append(expShares[policy.ExpenseAccount], share(line.SKU, unitCostPaise*int64(line.Qty)))
			}
			continue
		}
		if err := blendIntoCostTx(tx, schema, line.SKU, line.Qty, unitCostPaise*int64(line.Qty), stockIn[line.SKU], true); err != nil {
			return fmt.Errorf("item cost for %s: %w", line.SKU, err)
		}
		stockIn[line.SKU] -= line.Qty
		totalValuePaise += unitCostPaise * int64(line.Qty)
		inventoryLines = append(inventoryLines, groupLine{Sku: line.SKU, Weight: unitCostPaise * int64(line.Qty)})
		if lineNo != nil {
			invShares = append(invShares, share(line.SKU, unitCostPaise*int64(line.Qty)))
		}
	}
	debits := map[string]int64{}
	var parts []sourceLinePart
	if totalValuePaise > 0 {
		inv := grnInventoryDebits(tenantID, grnID, poID, inventoryLines, totalValuePaise)
		for acct, v := range inv {
			debits[acct] += v
		}
		parts = append(parts, sourceLinePart{Debits: inv, Shares: invShares})
	}
	for acct, v := range expenseByAccount {
		if v > 0 {
			debits[acct] += v
			totalValuePaise += v
			parts = append(parts, sourceLinePart{Debits: map[string]int64{acct: v}, Shares: expShares[acct]})
		}
	}
	if totalValuePaise <= 0 {
		return applyReceiptChargesTx(tx, tenantID, schema, grnID, part, chargeLines)
	}
	key := fmt.Sprintf("GRN:%s:RECEIPT_COST", grnID)
	if part != "" {
		key = fmt.Sprintf("GRN:%s:RECEIPT_COST:%s", grnID, part)
	}
	parts = append(parts, sourceLinePart{Credits: map[string]int64{"2100": totalValuePaise}, Shares: allShares})
	if err := PostDoubleEntryTx(tx, tenantID, schema, "GRN", grnID,
		debits, map[string]int64{"2100": totalValuePaise},
		"", key, withSourceLineParts(schema, nil, parts...)...); err != nil {
		return fmt.Errorf("receipt GL entry: %w", err)
	}
	return applyReceiptChargesTx(tx, tenantID, schema, grnID, part, chargeLines)
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
	grnData, _, err := fetchDocData(tenantID, "GRN", grnID)
	if err != nil {
		return "", fmt.Errorf("grn_reference: %v", err)
	}
	if isMemoReceipt(tenantID, grnID, grnData) { // 148.16: its units carry no value to add to
		return "", memoNoBillOnReceipt()
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
	grnLocation, _ := grnData["location"].(string)
	issueNumberInto(docData, tenantID, "LandedCostVoucher", id, "", grnLocation) // Stage 76.11: the GRN's store
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
// consistent with the receipt costing never having costed it). One-shot:
// a voucher already Applied refuses a second application, since re-running
// it would double the cost every time.
func ApplyLandedCostVoucher(tenantID, voucherID, userID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	// Stage 135.21: one transaction under the voucher's row lock - the status
	// check, the items' cost, the GL and the Applied status. Checked outside
	// it, two applies at once both passed and each raised the cost again.
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var dataStr, status string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = 'LandedCostVoucher' AND id = $1 AND deleted_at IS NULL FOR UPDATE`, schema),
		voucherID).Scan(&dataStr, &status); err != nil {
		return err
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
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
	bases := grnCostBases(tenantID, schema, grnID, receivedItems)
	var totalBaseValue int64
	for _, b := range bases {
		totalBaseValue += b.value
	}
	if totalBaseValue <= 0 {
		return fmt.Errorf("GRN %s has no costed lines to allocate landed cost across", grnID)
	}

	// Proportional allocation, the final line absorbing any paise-rounding
	// remainder - the same "largest/last line takes the remainder" technique
	// ConvertPostingToFunctional (engines/currency_documents.go) already
	// uses, so a landed cost voucher's total always lands exactly on the
	// items it named, never a paisa short or over.
	//
	// Stage 148.1: the share of the units still here goes into their cost, the
	// share of the units already sold to COGS variance.
	legs := map[string]int64{"2110": -totalChargePaise}
	weights := make([]int64, len(bases))
	for i, b := range bases {
		weights[i] = b.value
	}
	shares := spreadPaise(totalChargePaise, weights)
	for i, b := range bases {
		if _, _, err := addCostAdjustmentLegsTx(tx, tenantID, schema, b.sku, grnID, b.qty, shares[i], legs); err != nil {
			return fmt.Errorf("applying landed cost to %s: %v", b.sku, err)
		}
	}

	if err := postSignedLegsTx(tx, tenantID, schema, "LandedCostVoucher", voucherID, legs,
		fmt.Sprintf("LandedCostVoucher:%s:APPLY", voucherID)); err != nil {
		return fmt.Errorf("GL posting failed, voucher not applied: %v", err)
	}

	data["status"] = "Applied"
	marshaled, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = 'Applied', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'LandedCostVoucher' AND id = $2`, schema),
		marshaled, voucherID); err != nil {
		return err
	}
	return tx.Commit()
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
	if grn, _, gerr := fetchDocData(tenantID, "GRN", grnID); gerr == nil && isMemoReceipt(tenantID, grnID, grn) { // 148.16
		return memoNoBillOnReceipt()
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
	// Stage 148.1: an item whose stock is all named pieces is worth their own costs.
	pieces, err := pieceCostTotals(db.DB, schema)
	if err != nil {
		return nil, err
	}
	memoOpen := memoOpenBySKU(db.DB, schema) // 148.16: a vendor's units are not our stock value
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
		value, source := unitCostPaise*max(qty-int64(memoOpen[sku]), 0), costSourceAverage
		if unitCostPaise == 0 {
			source = costSourceNone
		}
		if p, ok := pieces[sku]; ok && p.covers(qty, unitCostPaise) {
			value, source = p.value-int64(memoOpen[sku])*unitCostPaise, costSourceOwn // 148.16
		}
		out = append(out, map[string]interface{}{
			"sku": sku, "qty_on_hand": qty,
			"unit_cost":   PaiseToRupees(unitCostPaise),
			"total_value": PaiseToRupees(value),
			"costed":      unitCostPaise > 0,
			"rate_source": source,
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
			{Key: "rate_source", Label: "Rate From"},
		},
		Params: []ReportParam{},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			return GetInventoryValuation(tenantID)
		},
	})
}
