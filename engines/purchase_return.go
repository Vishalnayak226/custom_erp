package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// Stage 57.15 (user decision 2026-10-06): Purchase Return / return to vendor.
//
// A PurchaseReturn is raised against the GRN the goods arrived on, never on
// its own. What can go back is what that GRN received, per SKU, lot and stock
// bucket, less what other returns against it already hold:
//
//   - Accepted: the qty QC accepted, which went into available stock and was
//     costed into 1200 Inventory / 2100 GRN Suspense at the PO's ex-GST rate
//     (RecordGRNReceiptCosting). Returning it takes it out of available and
//     is what the vendor owes us for.
//   - Rejected / Damaged: the qty QC set aside into qc_hold / damaged. It was
//     never costed or booked as owed, so returning it moves stock only.
//
// A GRN whose stock never posted (status Cancelled, which is what the GRN
// create hook sets when posting fails) cannot be returned against.
//
// Posting moves the stock out, books Dr 5150 / Cr 1200 for the accepted
// value, and raises a DebitNote for that value and posts it through the
// existing PostDebitNote (Dr 2100 / Cr 5150). Net effect: Dr 2100 / Cr 1200 -
// what we owe the vendor and what we hold both drop by the returned value,
// the exact reverse of the GRN's own booking. If the tenant has an approval
// rule for PurchaseReturn, posting needs the approval engine's Approved.

const (
	PurchaseReturnBucketAccepted = "Accepted"
	PurchaseReturnBucketRejected = "Rejected"
	PurchaseReturnBucketDamaged  = "Damaged"
)

// PurchaseReturnLine is one line of PurchaseReturn.return_items.
type PurchaseReturnLine struct {
	SKU         string  `json:"sku"`
	BatchNo     string  `json:"batch_no,omitempty"`
	StockBucket string  `json:"stock_bucket"`
	Qty         float64 `json:"qty"`
	UnitCost    float64 `json:"unit_cost"`
	LineValue   float64 `json:"line_value"`
}

// PurchaseReturnableLine is what one SKU/lot/bucket of a GRN can still return.
type PurchaseReturnableLine struct {
	SKU         string  `json:"sku"`
	BatchNo     string  `json:"batch_no,omitempty"`
	StockBucket string  `json:"stock_bucket"`
	Received    float64 `json:"received"`
	Returned    float64 `json:"returned"`
	OnOpen      float64 `json:"on_open_returns"`
	Returnable  float64 `json:"returnable"`
	UnitCost    float64 `json:"unit_cost"`
}

// PurchaseReturnable is a GRN's return context.
type PurchaseReturnable struct {
	GRNID    string                   `json:"grn_id"`
	POID     string                   `json:"po_id"`
	VendorID string                   `json:"vendor_id"`
	Location string                   `json:"location"`
	Lines    []PurchaseReturnableLine `json:"lines"`
}

type purchaseReturnKey struct{ sku, batch, bucket string }

// queryRower is satisfied by both *sql.DB and *sql.Tx, so the same readers
// serve the save-time check and the locked re-check inside PostPurchaseReturn.
type queryRower interface {
	QueryRow(query string, args ...interface{}) *sql.Row
	Query(query string, args ...interface{}) (*sql.Rows, error)
}

func normalizePurchaseReturnBucket(s string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "accepted", "available":
		return PurchaseReturnBucketAccepted, true
	case "rejected", "qc-hold", "qc hold":
		return PurchaseReturnBucketRejected, true
	case "damaged":
		return PurchaseReturnBucketDamaged, true
	}
	return "", false
}

// GetPurchaseReturnable lists what grnID can still return. excludeReturnID is
// the return being edited/posted, so its own lines do not count against it.
func GetPurchaseReturnable(tenantID, grnID, excludeReturnID string) (*PurchaseReturnable, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	return purchaseReturnable(db.DB, tenantID, schema, grnID, excludeReturnID, false)
}

func purchaseReturnable(q queryRower, tenantID, schema, grnID, excludeReturnID string, lock bool) (*PurchaseReturnable, error) {
	grnID = strings.TrimSpace(grnID)
	if grnID == "" {
		return nil, &ValidationError{Code: "PURCHA-0118", SubFor: "Goods Receipt (GRN)", Message: "a purchase return must be raised against a goods receipt (GRN)"}
	}
	lockClause := ""
	if lock {
		// Serializes two returns posting against the same GRN, so the
		// re-check below cannot be passed by both at once.
		lockClause = " FOR UPDATE"
	}
	var dataStr, status string
	err := q.QueryRow(fmt.Sprintf(
		`SELECT data, status FROM %s.documents WHERE doctype = 'GRN' AND id = $1 AND deleted_at IS NULL`+lockClause, schema),
		grnID).Scan(&dataStr, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, &ValidationError{Code: "PURCHA-0118", SubFor: "Goods Receipt (GRN)", Message: fmt.Sprintf("goods receipt %s does not exist", grnID)}
	}
	if err != nil {
		return nil, err
	}
	var grn map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &grn); err != nil {
		return nil, fmt.Errorf("GRN %s has corrupt data: %v", grnID, err)
	}
	if status == "Cancelled" {
		return nil, &ValidationError{Code: "PURCHA-0118", SubFor: "Goods Receipt (GRN)", Message: fmt.Sprintf(
			"goods receipt %s is cancelled - its stock was never posted, so there is nothing to return against it", grnID)}
	}

	out := &PurchaseReturnable{GRNID: grnID, POID: strField(grn, "po_id"), VendorID: strField(grn, "vendor"), Location: strField(grn, "location")}
	if out.VendorID == "" && out.POID != "" {
		if po, _, poErr := fetchDocData(tenantID, "PurchaseOrder", out.POID); poErr == nil {
			out.VendorID = strField(po, "vendor")
		}
	}

	var received []grnReceivedLine
	if s, _ := grn["received_items"].(string); s != "" {
		if err := json.Unmarshal([]byte(s), &received); err != nil {
			return nil, fmt.Errorf("GRN %s has malformed received_items: %v", grnID, err)
		}
	}
	got := map[purchaseReturnKey]float64{}
	var order []purchaseReturnKey
	add := func(sku, batch, bucket string, qty float64) {
		if qty <= 0 || sku == "" {
			return
		}
		k := purchaseReturnKey{sku, strings.TrimSpace(batch), bucket}
		if _, seen := got[k]; !seen {
			order = append(order, k)
		}
		got[k] += qty
	}
	for _, l := range received {
		add(l.Sku, l.BatchNo, PurchaseReturnBucketAccepted, l.derivedAcceptedQty())
		if l.RejectedQty != nil {
			add(l.Sku, l.BatchNo, PurchaseReturnBucketRejected, *l.RejectedQty)
		}
		if l.DamagedQty != nil {
			add(l.Sku, l.BatchNo, PurchaseReturnBucketDamaged, *l.DamagedQty)
		}
	}

	posted, open, err := purchaseReturnedQuantities(q, schema, grnID, excludeReturnID)
	if err != nil {
		return nil, err
	}
	costs := map[string]float64{}
	for _, k := range order {
		line := PurchaseReturnableLine{SKU: k.sku, BatchNo: k.batch, StockBucket: k.bucket,
			Received: got[k], Returned: posted[k], OnOpen: open[k]}
		line.Returnable = math.Max(0, line.Received-line.Returned-line.OnOpen)
		if k.bucket == PurchaseReturnBucketAccepted {
			if c, ok := costs[k.sku]; ok {
				line.UnitCost = c
			} else if paise, ok := resolvePOBaseUnitCostPaise(tenantID, out.POID, k.sku); ok {
				costs[k.sku] = PaiseToRupees(paise)
				line.UnitCost = costs[k.sku]
			}
		}
		out.Lines = append(out.Lines, line)
	}
	return out, nil
}

// purchaseReturnedQuantities sums the lines of every other return against
// grnID: posted ones (stock already gone) and open ones (Draft, awaiting or
// granted approval - not yet gone, but already claimed).
func purchaseReturnedQuantities(q queryRower, schema, grnID, excludeReturnID string) (posted, open map[purchaseReturnKey]float64, err error) {
	posted, open = map[purchaseReturnKey]float64{}, map[purchaseReturnKey]float64{}
	rows, err := q.Query(fmt.Sprintf(`
		SELECT status, COALESCE(data->>'return_items', '') FROM %s.documents
		WHERE doctype = 'PurchaseReturn' AND data->>'grn_id' = $1 AND id <> $2
		  AND deleted_at IS NULL AND status NOT IN ('Cancelled', 'Rejected')`, schema), grnID, excludeReturnID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var status, itemsStr string
		if err := rows.Scan(&status, &itemsStr); err != nil {
			return nil, nil, err
		}
		var lines []PurchaseReturnLine
		if itemsStr == "" || json.Unmarshal([]byte(itemsStr), &lines) != nil {
			continue
		}
		target := open
		if status == "Posted" {
			target = posted
		}
		for _, l := range lines {
			bucket, ok := normalizePurchaseReturnBucket(l.StockBucket)
			if !ok {
				continue
			}
			target[purchaseReturnKey{l.SKU, strings.TrimSpace(l.BatchNo), bucket}] += l.Qty
		}
	}
	return posted, open, rows.Err()
}

// parsePurchaseReturnLines reads return_items, which the form sends as a JSON
// string (JSONTable) and an API caller may send as an array.
func parsePurchaseReturnLines(raw interface{}) ([]PurchaseReturnLine, error) {
	var b []byte
	switch v := raw.(type) {
	case nil:
		return nil, nil
	case string:
		if strings.TrimSpace(v) == "" {
			return nil, nil
		}
		b = []byte(v)
	default:
		var err error
		if b, err = json.Marshal(v); err != nil {
			return nil, err
		}
	}
	// qty arrives as a number from the API and as a string from the form's
	// line editor, so read it loosely and convert.
	var rows []map[string]interface{}
	if err := json.Unmarshal(b, &rows); err != nil {
		return nil, fmt.Errorf("return lines must be a JSON array of line items")
	}
	lines := make([]PurchaseReturnLine, 0, len(rows))
	for _, r := range rows {
		lines = append(lines, PurchaseReturnLine{
			SKU: strings.TrimSpace(strField(r, "sku")), BatchNo: strings.TrimSpace(strField(r, "batch_no")),
			StockBucket: strField(r, "stock_bucket"), Qty: numFromInterface(r["qty"]),
		})
	}
	return lines, nil
}

// resolvePurchaseReturnLines checks lines against what the GRN can still
// return and prices them. The returned lines are normalized (bucket spelled
// out, lot filled in when the GRN has exactly one for that SKU and bucket,
// unit cost and line value set) and are what gets stored.
func resolvePurchaseReturnLines(ctx *PurchaseReturnable, lines []PurchaseReturnLine) ([]PurchaseReturnLine, float64, error) {
	if len(lines) == 0 {
		return nil, 0, &ValidationError{Code: "GLOBAL-0001", SubFor: "Return Lines", Message: "add at least one line to return"}
	}
	avail := map[purchaseReturnKey]PurchaseReturnableLine{}
	lotsBySKUBucket := map[[2]string][]string{}
	for _, l := range ctx.Lines {
		k := purchaseReturnKey{l.SKU, l.BatchNo, l.StockBucket}
		avail[k] = l
		sb := [2]string{l.SKU, l.StockBucket}
		lotsBySKUBucket[sb] = append(lotsBySKUBucket[sb], l.BatchNo)
	}

	asked := map[purchaseReturnKey]float64{}
	out := make([]PurchaseReturnLine, 0, len(lines))
	var total float64
	for i, l := range lines {
		bucket, ok := normalizePurchaseReturnBucket(l.StockBucket)
		if !ok {
			return nil, 0, &ValidationError{Code: "GLOBAL-0002", SubFor: "Return Lines", Message: fmt.Sprintf(
				"line %d: stock must be Accepted, Rejected or Damaged, not %q", i+1, l.StockBucket)}
		}
		l.StockBucket = bucket
		if l.SKU == "" {
			return nil, 0, &ValidationError{Code: "GLOBAL-0001", SubFor: "Return Lines", Message: fmt.Sprintf("line %d is missing its SKU", i+1)}
		}
		if l.Qty <= 0 {
			return nil, 0, &ValidationError{Code: "GLOBAL-0002", SubFor: "Return Lines", Message: fmt.Sprintf("line %d: quantity must be more than zero", i+1)}
		}
		lots := lotsBySKUBucket[[2]string{l.SKU, bucket}]
		if len(lots) == 0 {
			return nil, 0, &ValidationError{Code: "PURCHA-0117", SubFor: "Return Lines", Message: fmt.Sprintf(
				"line %d: goods receipt %s received no %s stock of %s", i+1, ctx.GRNID, strings.ToLower(bucket), l.SKU)}
		}
		if l.BatchNo == "" && len(lots) == 1 {
			l.BatchNo = lots[0]
		}
		k := purchaseReturnKey{l.SKU, l.BatchNo, bucket}
		a, ok := avail[k]
		if !ok {
			if l.BatchNo == "" {
				return nil, 0, &ValidationError{Code: "GLOBAL-0001", SubFor: "Return Lines", Message: fmt.Sprintf(
					"line %d: %s arrived on more than one lot on %s - say which lot (%s)", i+1, l.SKU, ctx.GRNID, strings.Join(lots, ", "))}
			}
			return nil, 0, &ValidationError{Code: "PURCHA-0117", SubFor: "Return Lines", Message: fmt.Sprintf(
				"line %d: lot %s of %s was not received as %s on %s", i+1, l.BatchNo, l.SKU, strings.ToLower(bucket), ctx.GRNID)}
		}
		asked[k] += l.Qty
		if a.Returnable <= 0 {
			return nil, 0, &ValidationError{Code: "PURCHA-0119", SubFor: "Return Lines", Message: fmt.Sprintf(
				"line %d: all %v of %s%s (%s) on %s is already on a purchase return", i+1, a.Received, l.SKU, lotSuffix(l.BatchNo), strings.ToLower(bucket), ctx.GRNID)}
		}
		if asked[k] > a.Returnable+1e-9 {
			return nil, 0, &ValidationError{Code: "PURCHA-0117", SubFor: "Return Lines", Message: fmt.Sprintf(
				"line %d: only %v of %s%s (%s) can still be returned on %s - received %v, already returned %v, on other open returns %v",
				i+1, a.Returnable, l.SKU, lotSuffix(l.BatchNo), strings.ToLower(bucket), ctx.GRNID, a.Received, a.Returned, a.OnOpen)}
		}
		if bucket == PurchaseReturnBucketAccepted {
			l.UnitCost = a.UnitCost
			l.LineValue = round2(a.UnitCost * l.Qty)
		} else {
			l.UnitCost, l.LineValue = 0, 0
		}
		total += l.LineValue
		out = append(out, l)
	}
	return out, round2(total), nil
}

func lotSuffix(batch string) string {
	if batch == "" {
		return ""
	}
	return " lot " + batch
}

// validatePurchaseReturnRules runs at ValidateTransactionalRules, the generic
// doc API's choke point, on every create and edit.
func validatePurchaseReturnRules(tenantID, docID, priorStatus string, priorData, payload map[string]interface{}) error {
	if priorData != nil && priorStatus == "Posted" {
		return &ValidationError{Code: "GLOBAL-0019", Message: "a posted purchase return cannot be changed - its stock and debit note are already booked"}
	}
	status := strings.TrimSpace(strField(payload, "status"))
	if status == "" {
		status = "Draft"
		if priorStatus != "" {
			status = priorStatus
		}
		payload["status"] = status
	}
	// Posted is reached only through PostPurchaseReturn, which moves the
	// stock; the approval states only through the approval engine.
	if status != priorStatus && status != "Draft" && status != "Cancelled" {
		return &ValidationError{Code: "GLOBAL-0019", Message: fmt.Sprintf(
			"a purchase return cannot be set to %s here - use Post (or Submit for approval) on the Purchase Return screen", status)}
	}
	if status == "Cancelled" {
		return nil
	}
	if strings.TrimSpace(strField(payload, "reason")) == "" {
		return &ValidationError{Code: "PURCHA-0116", SubFor: "Reason for Return", Message: "a reason for returning the goods to the vendor is required"}
	}

	ctx, err := GetPurchaseReturnable(tenantID, strField(payload, "grn_id"), docID)
	if err != nil {
		return err
	}
	lines, err := parsePurchaseReturnLines(payload["return_items"])
	if err != nil {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Return Lines", Message: err.Error()}
	}
	resolved, total, err := resolvePurchaseReturnLines(ctx, lines)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(resolved)
	if err != nil {
		return err
	}
	payload["return_items"] = string(encoded)
	payload["total_amount"] = total
	payload["vendor_id"] = ctx.VendorID
	payload["po_id"] = ctx.POID
	payload["location"] = ctx.Location
	return nil
}

// PurchaseReturnPostResult is what PostPurchaseReturn did.
type PurchaseReturnPostResult struct {
	ReturnID         string  `json:"id"`
	ReturnValue      float64 `json:"return_value"`
	DebitNoteID      string  `json:"debit_note_id,omitempty"`
	DebitNotePosted  bool    `json:"debit_note_posted"`
	DebitNoteWarning string  `json:"debit_note_warning,omitempty"`
}

// PostPurchaseReturn moves a return's stock out, books its value, and raises
// and posts its debit note. Stock, the 1200 credit, the debit note document
// and the return's own Posted status commit together; the debit note's GL
// posting runs straight after through the unchanged PostDebitNote, and if
// that alone fails the return stays posted and says so (PURCHA-0120) rather
// than undoing stock that has been handed back to the vendor.
func PostPurchaseReturn(tenantID, returnID, userID, storeCode string) (*PurchaseReturnPostResult, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	gated, err := IsApprovalGated(tenantID, "PurchaseReturn")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(storeCode) == "" {
		storeCode = "HQ"
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return nil, err
	}

	var dataStr, status string
	err = tx.QueryRow(fmt.Sprintf(
		`SELECT data, status FROM %s.documents WHERE doctype = 'PurchaseReturn' AND id = $1 AND deleted_at IS NULL FOR UPDATE`, schema),
		returnID).Scan(&dataStr, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, &ValidationError{Code: "GLOBAL-0002", Message: fmt.Sprintf("purchase return %s does not exist", returnID)}
	}
	if err != nil {
		return nil, err
	}
	switch {
	case status == "Posted":
		return nil, &ValidationError{Code: "PURCHA-0119", Message: fmt.Sprintf("purchase return %s is already posted", returnID)}
	case gated && status != "Approved":
		return nil, &ValidationError{Code: "GLOBAL-0019", Message: fmt.Sprintf(
			"purchase return %s needs approval before it can be posted (status: %s) - submit it for approval first", returnID, status)}
	case !gated && status != "Draft" && status != "Approved":
		return nil, &ValidationError{Code: "GLOBAL-0019", Message: fmt.Sprintf("purchase return %s cannot be posted from status %s", returnID, status)}
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return nil, err
	}
	if strings.TrimSpace(strField(data, "reason")) == "" {
		return nil, &ValidationError{Code: "PURCHA-0116", Message: "a reason for returning the goods to the vendor is required"}
	}

	// Re-check against the GRN under its row lock: another return may have
	// posted since this one was saved.
	ctx, err := purchaseReturnable(tx, tenantID, schema, strField(data, "grn_id"), returnID, true)
	if err != nil {
		return nil, err
	}
	stored, err := parsePurchaseReturnLines(data["return_items"])
	if err != nil {
		return nil, err
	}
	lines, total, err := resolvePurchaseReturnLines(ctx, stored)
	if err != nil {
		return nil, err
	}
	if ctx.Location == "" {
		return nil, &ValidationError{Code: "PURCHA-0118", Message: fmt.Sprintf("goods receipt %s has no receiving location to return stock from", ctx.GRNID)}
	}

	// Stock out. Accepted stock leaves available through the same floor-
	// checked posting every outbound movement uses; set-aside stock leaves
	// its own bucket, refused if that bucket no longer holds it.
	var accepted []interface{}
	var ledger []StockLedgerEntry
	for _, l := range lines {
		qty := int(math.Round(l.Qty))
		switch l.StockBucket {
		case PurchaseReturnBucketAccepted:
			line := map[string]interface{}{"sku": l.SKU, "qty": float64(-qty)}
			if l.BatchNo != "" {
				line["batch_no"] = l.BatchNo
			}
			accepted = append(accepted, line)
		default:
			column, from := "qc_hold", "QC-Hold"
			if l.StockBucket == PurchaseReturnBucketDamaged {
				column, from = "damaged", "Damaged"
			}
			res, err := tx.Exec(fmt.Sprintf(`
				UPDATE %s.inventory_availability SET on_hand = on_hand - $3, %s = %s - $3, updated_at = CURRENT_TIMESTAMP
				WHERE sku = $1 AND location_code = $2 AND %s >= $3`, schema, column, column, column),
				l.SKU, ctx.Location, qty)
			if err != nil {
				return nil, err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return nil, &ValidationError{Code: "PURCHA-0117", Message: fmt.Sprintf(
					"%s at %s no longer holds %d %s unit(s) to return", l.SKU, ctx.Location, qty, strings.ToLower(l.StockBucket))}
			}
			ledger = append(ledger, StockLedgerEntry{
				ItemID: l.SKU, WarehouseID: ctx.Location, Qty: float64(-qty), VoucherType: "PurchaseReturn", VoucherID: returnID,
				FromStatus: from, UserID: userID, BatchNo: l.BatchNo,
				IdempotencyKey: fmt.Sprintf("PurchaseReturn:%s:%s:%s:%s:%s", returnID, ctx.Location, l.SKU, l.StockBucket, l.BatchNo),
			})
		}
	}
	var postedLines []PostedStockLine
	if len(accepted) > 0 {
		if _, postedLines, err = PostInventoryLedgerWithVoucherTx(tx, schema, ctx.Location, accepted, false); err != nil {
			return nil, err
		}
	}

	valuePaise := RupeesToPaise(total)
	var dnID string
	if valuePaise > 0 {
		// GenerateSequence commits on its own connection, like every generated
		// number: a rolled-back post leaves a gap in the DN series, never a
		// duplicate. Only drawn when there is a value to raise a note for.
		if dnID, err = GenerateSequence(tenantID, "DN", storeCode, documentFinancialYear(time.Now())); err != nil {
			return nil, fmt.Errorf("could not number the debit note: %w", err)
		}
		if err := PostDoubleEntryTx(tx, tenantID, schema, "PurchaseReturn", returnID,
			map[string]int64{"5150": valuePaise}, map[string]int64{"1200": valuePaise},
			"", fmt.Sprintf("PurchaseReturn:%s:STOCK_OUT", returnID)); err != nil {
			return nil, fmt.Errorf("GL posting failed, return not posted: %v", err)
		}
		dn := map[string]interface{}{
			"code": dnID, "note_number": dnID, "vendor_id": ctx.VendorID, "reference_po": ctx.POID,
			"amount": total, "reason": fmt.Sprintf("Purchase return %s (GRN %s): %s", returnID, ctx.GRNID, strField(data, "reason")),
			"status": "Draft", "source_doctype": "PurchaseReturn", "source_doc_id": returnID,
		}
		dnBytes, err := json.Marshal(dn)
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(fmt.Sprintf(
			`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'DebitNote', $2, 'Draft', $3)`, schema),
			dnID, dnBytes, userID); err != nil {
			return nil, fmt.Errorf("could not raise the debit note: %v", err)
		}
		data["debit_note_id"] = dnID
	}

	encoded, err := json.Marshal(lines)
	if err != nil {
		return nil, err
	}
	data["return_items"] = string(encoded)
	data["total_amount"] = total
	data["status"] = "Posted"
	data["posted_by"] = userID
	data["posted_at"] = time.Now().UTC().Format(time.RFC3339)
	updated, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = 'Posted', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'PurchaseReturn' AND id = $2`, schema),
		updated, returnID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// After the commit, best-effort like the GRN's own costing and ledger
	// writes: the goods have physically gone back.
	WriteStockLedgerLines(tenantID, ctx.Location, "PurchaseReturn", returnID, userID, postedLines)
	for _, e := range ledger {
		if lerr := WriteStockLedgerEntry(tenantID, e); lerr != nil {
			LogSystemError(tenantID, "", "WARN", "PostPurchaseReturn", fmt.Sprintf("stock ledger write failed for %s: %v", e.ItemID, lerr), "")
		}
	}
	for _, l := range lines {
		if l.StockBucket == PurchaseReturnBucketAccepted && l.LineValue > 0 {
			if cerr := reverseItemCostReceipt(schema, l.SKU, l.Qty, RupeesToPaise(l.LineValue)); cerr != nil {
				LogSystemError(tenantID, "", "WARN", "PostPurchaseReturn", fmt.Sprintf("item cost not reduced for %s: %v", l.SKU, cerr), "")
			}
		}
	}

	result := &PurchaseReturnPostResult{ReturnID: returnID, ReturnValue: total}
	if valuePaise > 0 {
		result.DebitNoteID = dnID
		if _, derr := PostDebitNote(tenantID, dnID, userID); derr != nil {
			result.DebitNoteWarning = fmt.Sprintf("PURCHA-0120: debit note %s was raised but could not be posted (%v) - post it from Debit / Credit Notes", dnID, derr)
			LogSystemError(tenantID, "", "ERROR", "PostPurchaseReturn", result.DebitNoteWarning, "")
		} else {
			result.DebitNotePosted = true
		}
	}
	LogAuditEvent(tenantID, userID, "POST_PURCHASE_RETURN", "SUCCESS", fmt.Sprintf(
		"Posted purchase return %s against GRN %s value=%.2f debit_note=%s", returnID, ctx.GRNID, total, result.DebitNoteID))
	return result, nil
}

// reverseItemCostReceipt takes a returned receipt back out of the moving
// average: the qty and value come off cumulative received, and the average
// is recomputed from what is left (kept as it was if nothing is left).
func reverseItemCostReceipt(schema, itemCode string, qty float64, valuePaise int64) error {
	if qty <= 0 || valuePaise <= 0 {
		return nil
	}
	_, err := db.DB.Exec(fmt.Sprintf(`
		WITH input AS (SELECT $1::varchar AS item_code, $2::numeric AS qty, $3::bigint AS value_paise)
		UPDATE %s.item_cost c SET
			cumulative_qty_received = GREATEST(c.cumulative_qty_received - i.qty, 0),
			cumulative_value_received_paise = GREATEST(c.cumulative_value_received_paise - i.value_paise, 0),
			avg_unit_cost_paise = CASE
				WHEN c.cumulative_qty_received - i.qty <= 0 THEN c.avg_unit_cost_paise
				ELSE (GREATEST(c.cumulative_value_received_paise - i.value_paise, 0)::numeric / (c.cumulative_qty_received - i.qty))::bigint
				END,
			updated_at = CURRENT_TIMESTAMP
		FROM input i WHERE c.item_code = i.item_code`, schema),
		itemCode, qty, valuePaise)
	return err
}

// GetPurchaseReturn reads one return, for the post handler's scope check.
func GetPurchaseReturn(tenantID, returnID string) (map[string]interface{}, error) {
	data, _, err := fetchDocData(tenantID, "PurchaseReturn", returnID)
	return data, err
}

// postedPurchaseReturnAcceptedQty is the accepted qty per SKU that posted
// returns have taken back off grnID - what the vendor can no longer bill for
// (MatchVendorInvoice).
func postedPurchaseReturnAcceptedQty(q queryRower, schema, grnID string) (map[string]float64, error) {
	posted, _, err := purchaseReturnedQuantities(q, schema, grnID, "")
	if err != nil {
		return nil, err
	}
	out := map[string]float64{}
	for k, qty := range posted {
		if k.bucket == PurchaseReturnBucketAccepted {
			out[k.sku] += qty
		}
	}
	return out, nil
}
