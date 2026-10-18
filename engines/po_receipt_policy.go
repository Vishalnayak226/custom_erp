package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Stage 73.2.4 - the GRN rejected / short quantity policy (decision D05) and
// one view that reconciles a purchase order end to end.
//
// D05: a rejected (or damaged) unit does not fulfil the PO. It stays open for
// the vendor to replace, so only ACCEPTED units count against the ordered
// quantity (validateGRNAgainstPO, purchaseOrderFullyReceived). When the buyer
// decides no replacement is coming, they short-close the PO with a reason:
// the balance is closed (status Closed, no further receipts) and every
// rejected / damaged unit still held is put on a Draft purchase return - the
// claim on the vendor, whose posting moves the goods out (and raises the
// debit note for any accepted value, through the existing PostPurchaseReturn).

// POReconLine is one PO item: what was ordered, what came of it, and what
// is still open.
type POReconLine struct {
	Sku         string  `json:"sku"`
	Name        string  `json:"name,omitempty"`
	Rate        float64 `json:"rate"`
	Ordered     float64 `json:"ordered"`
	Received    float64 `json:"received"`
	Accepted    float64 `json:"accepted"`
	Rejected    float64 `json:"rejected"`
	Damaged     float64 `json:"damaged"`
	Returned    float64 `json:"returned"`
	ShortClosed float64 `json:"short_closed"`
	Open        float64 `json:"open"`
	Invoiced    float64 `json:"invoiced"`
	Paid        float64 `json:"paid"`
}

// POReconGRN is one goods receipt against the PO.
type POReconGRN struct {
	ID       string   `json:"id"`
	Number   string   `json:"number"`
	Status   string   `json:"status"`
	Date     string   `json:"date,omitempty"`
	Invoices []string `json:"invoices,omitempty"`
}

// POReconInvoice is one vendor invoice on the PO's receipts.
type POReconInvoice struct {
	ID     string  `json:"id"`
	Number string  `json:"number"`
	GRNID  string  `json:"grn_id"`
	Amount float64 `json:"amount"`
	Status string  `json:"status"`
}

// POReconciliation is the ordered / accepted / rejected / open / invoiced /
// paid view of one purchase order.
type POReconciliation struct {
	POID           string           `json:"po_id"`
	PONumber       string           `json:"po_number"`
	Vendor         string           `json:"vendor"`
	VendorName     string           `json:"vendor_name,omitempty"`
	Status         string           `json:"status"`
	ShortClosed    bool             `json:"short_closed"`
	ShortCloseInfo string           `json:"short_close_info,omitempty"`
	Lines          []POReconLine    `json:"lines"`
	GRNs           []POReconGRN     `json:"grns"`
	Invoices       []POReconInvoice `json:"invoices"`
	Claims         []string         `json:"claims,omitempty"`
	OrderedValue   float64          `json:"ordered_value"`
	AcceptedValue  float64          `json:"accepted_value"`
	OpenValue      float64          `json:"open_value"`
	InvoicedAmount float64          `json:"invoiced_amount"`
	PaidAmount     float64          `json:"paid_amount"`
}

// invoicedStatuses are the vendor-invoice states that mean "billed".
var invoicedStatuses = map[string]bool{"Matched": true, "MismatchHold": true, "Approved": true, "Paid": true}

// PurchaseOrderReconciliation builds the view. Quantities per SKU come from
// the PO's own lines and the receipts' lines drawn from this PO (a multi-PO
// receipt counts only its lines for this PO); invoiced / paid quantities
// are the accepted units of receipts that carry a billed / paid invoice.
func PurchaseOrderReconciliation(tenantID, poID string) (*POReconciliation, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	po, status, err := fetchDocData(tenantID, "PurchaseOrder", poID)
	if err != nil {
		return nil, fmt.Errorf("purchase order %s not found", poID)
	}
	out := &POReconciliation{POID: poID, PONumber: firstNonEmpty(str(po, "po_number"), str(po, "code"), poID),
		Vendor: str(po, "vendor"), Status: status}
	if v, _, err := fetchDocData(tenantID, "Vendor", out.Vendor); err == nil {
		out.VendorName = str(v, "name")
	}
	if sc, _ := po["short_closed"].(bool); sc {
		out.ShortClosed = true
		out.ShortCloseInfo = fmt.Sprintf("Short-closed %s by %s (%s)%s", str(po, "short_closed_at"), str(po, "short_closed_by"),
			str(po, "short_close_reason_code"), reasonSuffix(str(po, "short_close_note")))
		if raw := str(po, "short_close_claims"); raw != "" {
			_ = json.Unmarshal([]byte(raw), &out.Claims)
		}
	}
	ordered, err := fetchPOItemQuantities(tenantID, poID)
	if err != nil {
		return nil, err
	}
	rates, _, _ := poRatesBySKU(tenantID, poID)
	lines := map[string]*POReconLine{}
	var order []string
	line := func(sku string) *POReconLine {
		if l, ok := lines[sku]; ok {
			return l
		}
		l := &POReconLine{Sku: sku, Rate: rates[sku].Rate}
		if item, _, err := fetchDocData(tenantID, "Item", sku); err == nil {
			l.Name, _ = item["name"].(string)
		}
		lines[sku] = l
		order = append(order, sku)
		return l
	}
	for sku, q := range ordered {
		line(sku).Ordered = q
	}

	grnIDs, err := PurchaseOrderReceipts(tenantID, poID)
	if err != nil {
		return nil, err
	}
	acceptedByGRN := map[string]map[string]float64{}
	for _, gid := range grnIDs {
		gdata, gstatus, err := fetchDocData(tenantID, "GRN", gid)
		if err != nil {
			continue
		}
		g := POReconGRN{ID: gid, Number: firstNonEmpty(str(gdata, "code"), gid), Status: gstatus, Date: str(gdata, "received_date")}
		out.GRNs = append(out.GRNs, g)
		var glines []grnReceivedLine
		if raw := str(gdata, "received_items"); raw != "" {
			_ = json.Unmarshal([]byte(raw), &glines)
		}
		headerPO := str(gdata, "po_id")
		acc := map[string]float64{}
		for _, gl := range glines {
			if !(gl.POID == poID || (gl.POID == "" && headerPO == poID)) {
				continue
			}
			l := line(gl.Sku)
			l.Received += gl.Qty
			a := gl.derivedAcceptedQty()
			l.Accepted += a
			acc[gl.Sku] += a
			if gl.RejectedQty != nil {
				l.Rejected += *gl.RejectedQty
			}
			if gl.DamagedQty != nil {
				l.Damaged += *gl.DamagedQty
			}
		}
		acceptedByGRN[gid] = acc
		if posted, _, err := purchaseReturnedQuantities(db.DB, schema, gid, ""); err == nil {
			for k, q := range posted {
				line(k.sku).Returned += q
			}
		}
	}

	// Vendor invoices on these receipts.
	if len(grnIDs) > 0 {
		rows, err := db.DB.Query(fmt.Sprintf(`
			SELECT id, COALESCE(data->>'invoice_number', id), COALESCE(data->>'grn_id', ''),
			       COALESCE(NULLIF(data->>'invoice_amount', '')::numeric, 0), COALESCE(status, '')
			  FROM %s.documents
			 WHERE doctype = 'VendorInvoice' AND deleted_at IS NULL AND (data->>'grn_id' = ANY($1) OR data->>'po_id' = $2)
			 ORDER BY created_at, id`, schema), pqStringArray(grnIDs), poID)
		if err != nil {
			return nil, err
		}
		billed := map[string]string{} // grn -> best status (Paid wins)
		for rows.Next() {
			var inv POReconInvoice
			if err := rows.Scan(&inv.ID, &inv.Number, &inv.GRNID, &inv.Amount, &inv.Status); err != nil {
				rows.Close()
				return nil, err
			}
			out.Invoices = append(out.Invoices, inv)
			if invoicedStatuses[inv.Status] {
				out.InvoicedAmount += inv.Amount
				if billed[inv.GRNID] != "Paid" {
					billed[inv.GRNID] = inv.Status
				}
			}
			if inv.Status == "Paid" {
				out.PaidAmount += inv.Amount
			}
		}
		rows.Close()
		for i := range out.GRNs {
			for _, inv := range out.Invoices {
				if inv.GRNID == out.GRNs[i].ID {
					out.GRNs[i].Invoices = append(out.GRNs[i].Invoices, inv.Number)
				}
			}
		}
		for gid, st := range billed {
			for sku, q := range acceptedByGRN[gid] {
				line(sku).Invoiced += q
				if st == "Paid" {
					line(sku).Paid += q
				}
			}
		}
	}

	var closedLines []struct {
		Sku    string  `json:"sku"`
		Closed float64 `json:"short_closed"`
	}
	if raw := str(po, "short_closed_lines"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &closedLines)
	}
	for _, c := range closedLines {
		line(c.Sku).ShortClosed += c.Closed
	}
	sort.Strings(order)
	for _, sku := range order {
		l := lines[sku]
		if l.Open = l.Ordered - l.Accepted - l.ShortClosed; l.Open < 0 {
			l.Open = 0
		}
		out.OrderedValue += l.Ordered * l.Rate
		out.AcceptedValue += l.Accepted * l.Rate
		out.OpenValue += l.Open * l.Rate
		out.Lines = append(out.Lines, *l)
	}
	out.OrderedValue, out.AcceptedValue, out.OpenValue = round2(out.OrderedValue), round2(out.AcceptedValue), round2(out.OpenValue)
	out.InvoicedAmount, out.PaidAmount = round2(out.InvoicedAmount), round2(out.PaidAmount)
	return out, nil
}

// pqStringArray renders a Go string slice as a Postgres text[] literal.
func pqStringArray(vals []string) string {
	quoted := make([]string, len(vals))
	for i, v := range vals {
		quoted[i] = `"` + strings.ReplaceAll(strings.ReplaceAll(v, `\`, `\\`), `"`, `\"`) + `"`
	}
	return "{" + strings.Join(quoted, ",") + "}"
}

// POShortCloseResult says what a short-close did.
type POShortCloseResult struct {
	POID        string   `json:"po_id"`
	ClosedUnits float64  `json:"closed_units"`
	Claims      []string `json:"claims,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
}

// ShortClosePurchaseOrder closes an Approved PO's open balance (D05) with a
// reason code (category Cancellation) and puts every rejected / damaged unit
// still held on a Draft purchase return per receipt - the claim.
func ShortClosePurchaseOrder(tenantID, poID, userID, reasonCode, note, storeCode string) (*POShortCloseResult, error) {
	if strings.TrimSpace(reasonCode) == "" {
		return nil, &ValidationError{Code: "PURCHA-0487", Message: "short-closing a purchase order needs a reason code (Reason Codes, category \"Cancellation\")"}
	}
	if err := requireActiveReasonCode(tenantID, reasonCode, "Cancellation"); err != nil {
		return nil, &ValidationError{Code: "PURCHA-0487", Message: fmt.Sprintf("short-closing a purchase order: %v", err)}
	}
	return shortClosePurchaseOrder(tenantID, poID, userID, reasonCode, note, storeCode)
}

// shortClosePurchaseOrder is the short-close after the reason is settled -
// a user's reason code above, or the auto-cancel sweep's system reason.
func shortClosePurchaseOrder(tenantID, poID, userID, reasonCode, note, storeCode string) (*POShortCloseResult, error) {
	recon, err := PurchaseOrderReconciliation(tenantID, poID)
	if err != nil {
		return nil, err
	}
	if recon.Status != "Approved" {
		return nil, &ValidationError{Code: "PURCHA-0488", Message: fmt.Sprintf("only an Approved purchase order can be short-closed (this one is %s)", recon.Status)}
	}
	type closedLine struct {
		Sku      string  `json:"sku"`
		Ordered  float64 `json:"ordered"`
		Accepted float64 `json:"accepted"`
		Closed   float64 `json:"short_closed"`
	}
	var closed []closedLine
	res := &POShortCloseResult{POID: poID}
	for _, l := range recon.Lines {
		if l.Open > 0 {
			closed = append(closed, closedLine{Sku: l.Sku, Ordered: l.Ordered, Accepted: l.Accepted, Closed: l.Open})
			res.ClosedUnits += l.Open
		}
	}
	if res.ClosedUnits == 0 {
		return nil, &ValidationError{Code: "PURCHA-0488", Message: "nothing is open on this purchase order - every ordered unit has been accepted"}
	}

	// The claims first: every rejected / damaged unit still held goes on a
	// Draft purchase return against its receipt. Raised before the PO is
	// closed, so a failure leaves the PO open to try again.
	if strings.TrimSpace(storeCode) == "" {
		storeCode = "HQ"
	}
	for _, g := range recon.GRNs {
		if g.Status == "Cancelled" {
			continue
		}
		ctx, err := GetPurchaseReturnable(tenantID, g.ID, "")
		if err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("receipt %s: %v", g.Number, err))
			continue
		}
		var claimLines []map[string]interface{}
		for _, rl := range ctx.Lines {
			if (rl.StockBucket == PurchaseReturnBucketRejected || rl.StockBucket == PurchaseReturnBucketDamaged) && rl.Returnable > 0 {
				claimLines = append(claimLines, map[string]interface{}{"sku": rl.SKU, "batch_no": rl.BatchNo, "stock_bucket": rl.StockBucket, "qty": rl.Returnable})
			}
		}
		if len(claimLines) == 0 {
			continue
		}
		id, err := raiseShortCloseClaim(tenantID, schemaOf(tenantID), userID, storeCode, recon, g, claimLines, reasonCode)
		if err != nil {
			return nil, err
		}
		res.Claims = append(res.Claims, id)
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var raw, current string
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT data, status FROM %s.documents WHERE doctype = 'PurchaseOrder' AND id = $1 FOR UPDATE`, schema), poID).Scan(&raw, &current); err != nil {
		return nil, err
	}
	if current != "Approved" {
		return nil, &ValidationError{Code: "PURCHA-0488", Message: fmt.Sprintf("only an Approved purchase order can be short-closed (this one is now %s)", current)}
	}
	closedJSON, _ := json.Marshal(closed)
	claimsJSON, _ := json.Marshal(res.Claims)
	patch, _ := json.Marshal(map[string]interface{}{
		"status": "Closed", "short_closed": true, "short_closed_lines": string(closedJSON),
		"short_close_reason_code": reasonCode, "short_close_note": strings.TrimSpace(note),
		"short_closed_by": userID, "short_closed_at": time.Now().UTC().Format(time.RFC3339),
		"short_close_claims": string(claimsJSON),
	})
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = data || $1::jsonb, status = 'Closed', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'PurchaseOrder' AND id = $2`, schema),
		patch, poID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, userID, "PURCHASE_ORDER_SHORT_CLOSED", "SUCCESS", fmt.Sprintf(
		"PO %s short-closed (%s%s): %s; claims %v", recon.PONumber, reasonCode, reasonSuffix(note), string(closedJSON), res.Claims))
	return res, nil
}

func schemaOf(tenantID string) string {
	s, _ := db.GetTenantSchema(tenantID)
	return s
}

// raiseShortCloseClaim writes one Draft purchase return for a receipt's
// rejected / damaged units, through the same validation every purchase
// return passes (validatePurchaseReturnRules fills vendor, totals and
// refuses more than is returnable).
func raiseShortCloseClaim(tenantID, schema, userID, storeCode string, recon *POReconciliation, g POReconGRN, lines []map[string]interface{}, reasonCode string) (string, error) {
	payload := map[string]interface{}{
		"grn_id": g.ID, "status": "Draft", "return_items": lines,
		"reason":         fmt.Sprintf("Claim on short-close of PO %s (%s): rejected / damaged goods on receipt %s go back to the vendor", recon.PONumber, reasonCode, g.Number),
		"source_doctype": "PurchaseOrder", "source_doc_id": recon.POID,
	}
	if err := validatePurchaseReturnRules(tenantID, "", "", nil, payload); err != nil {
		return "", fmt.Errorf("the claim for receipt %s could not be raised: %w", g.Number, err)
	}
	// Stage 76.8: the claim is numbered at its own (the receipt's) store.
	grnDoc, _, _ := fetchDocData(tenantID, "GRN", g.ID)
	storeCode = numberingStore(tenantID, storeCode, strField(payload, "location"), strField(grnDoc, "location"))
	id, err := GenerateSequence(tenantID, "PRT", storeCode, documentFinancialYear(time.Now()))
	if err != nil {
		return "", fmt.Errorf("could not number the claim for receipt %s: %w", g.Number, err)
	}
	payload["code"] = id
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'PurchaseReturn', $2, 'Draft', $3)`, schema),
		id, b, userID); err != nil {
		return "", err
	}
	LogAuditEvent(tenantID, userID, "CREATE_SHORT_CLOSE_CLAIM", "SUCCESS", fmt.Sprintf("Draft purchase return %s raised for receipt %s on short-close of PO %s", id, g.Number, recon.PONumber))
	return id, nil
}
