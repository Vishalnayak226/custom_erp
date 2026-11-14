package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// Stage 168.2: service purchase orders and the service entry sheet.
//
// A PO with po_kind "Service" buys work, not stock: it is never received on a
// GRN (refused where a GRN is checked against its PO). A ServiceEntrySheet
// names the PO and the quantity of each line done; it is Draft until someone
// who may approve a PO of that value accepts it. Accepting
//   - checks, under a per-PO lock, that no line is accepted past what was
//     ordered (less what other accepted sheets already took);
//   - posts the accrual a GRN posts for a non-stock item: Dr the item's
//     expense account, Cr 2100 GRN Suspense, at the accepted value, once
//     (idempotent on the sheet);
//   - moves no stock.
//
// The accepted sheet then takes the GRN's place in the vendor bill's
// three-way match (MatchVendorInvoiceAs, readServiceBilling below).
// A PO without po_kind is a goods PO, exactly as before.

const (
	ServiceEntrySheetDoctype = "ServiceEntrySheet"
	poKindService            = "Service"

	sheetDraft    = "Draft"
	sheetAccepted = "Accepted"

	codeServicePO       = "PURCHA-6120" // a service PO has no goods receipt / kind rule
	codeSheetSave       = "PURCHA-6121" // the sheet is not valid, or is locked
	codeSheetAction     = "PURCHA-6122" // accept refused
	codeServiceBillOver = "PURCHA-6123" // reserved for the bill match's own reasons
)

// POIsService reports whether a PO's data is a service PO.
func POIsService(po map[string]interface{}) bool {
	return strings.EqualFold(strField(po, "po_kind"), poKindService)
}

// validatePOKind: a PO's kind is fixed once it has gone for approval.
func validatePOKind(priorStatus string, priorData, payload map[string]interface{}) error {
	if priorData == nil || priorStatus == "" || priorStatus == "Draft" {
		return nil
	}
	if POIsService(priorData) != POIsService(payload) {
		return contractErr(codeServicePO, "a purchase order's type (Goods or Service) cannot change once it has gone for approval - raise a new one")
	}
	return nil
}

// refuseServiceGRN: a service PO is never received on a GRN.
func refuseServiceGRN(tenantID, poID string) error {
	if strings.TrimSpace(poID) == "" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil
	}
	var kind sql.NullString
	if db.DB.QueryRow(fmt.Sprintf(`SELECT data->>'po_kind' FROM %s.documents WHERE doctype = 'PurchaseOrder' AND id = $1`, schema), poID).Scan(&kind) == nil &&
		strings.EqualFold(strings.TrimSpace(kind.String), poKindService) {
		return contractErr(codeServicePO, "Purchase order %s is a service order: record the work on a service entry sheet, not a goods receipt.", poID)
	}
	return nil
}

// serviceLine is one PO line as the sheet sees it.
type serviceLine struct {
	Sku     string  `json:"sku"`
	Ordered float64 `json:"ordered"`
	Done    float64 `json:"accepted"`
	Open    float64 `json:"remaining"`
	Rate    float64 `json:"rate"` // net unit rate, rupees
}

func poNetUnitRate(l map[string]interface{}) float64 {
	qty := numFromInterface(l["qty"])
	if nv, ok := l["net_value"]; ok && nv != nil && qty > 0 {
		return numFromInterface(nv) / qty
	}
	return numFromInterface(l["rate"])
}

// serviceBalance lists a service PO's lines with what accepted sheets have
// already taken. skipSheet is left out of "accepted" (the sheet being checked).
func serviceBalance(tenantID, poID, skipSheet string) ([]serviceLine, map[string]interface{}, string, error) {
	po, status, err := fetchDocData(tenantID, "PurchaseOrder", poID)
	if err != nil {
		return nil, nil, "", contractErr(codeSheetSave, "purchase order %s was not found", poID)
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, nil, "", err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT data FROM %s.documents
		WHERE doctype = $1 AND deleted_at IS NULL AND status = $2 AND data->>'po_id' = $3 AND id <> $4`, schema),
		ServiceEntrySheetDoctype, sheetAccepted, poID, skipSheet)
	if err != nil {
		return nil, nil, "", err
	}
	defer rows.Close()
	done := map[string]float64{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, nil, "", err
		}
		var d map[string]interface{}
		if json.Unmarshal([]byte(s), &d) != nil {
			continue
		}
		for _, l := range docTableOf(d["lines"]) {
			done[strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", orBlank(l["sku"]))))] += numFromInterface(l["qty"])
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, "", err
	}
	var out []serviceLine
	idx := map[string]int{}
	for _, l := range docTableOf(po["items"]) {
		sku := strings.TrimSpace(fmt.Sprintf("%v", orBlank(l["sku"])))
		if sku == "" {
			continue
		}
		key := strings.ToLower(sku)
		ordered := numFromInterface(l["qty"]) - numFromInterface(l["closed_qty"])
		if i, ok := idx[key]; ok {
			out[i].Ordered += ordered
			continue
		}
		idx[key] = len(out)
		out = append(out, serviceLine{Sku: sku, Ordered: ordered, Rate: poNetUnitRate(l)})
	}
	for i := range out {
		out[i].Done = done[strings.ToLower(out[i].Sku)]
		out[i].Open = math.Max(0, out[i].Ordered-out[i].Done)
	}
	return out, po, status, nil
}

// ServiceBalanceForPO is the sheet screen's starting point for one service PO.
func ServiceBalanceForPO(tenantID, poID string) ([]serviceLine, error) {
	lines, po, _, err := serviceBalance(tenantID, poID, "")
	if err != nil {
		return nil, err
	}
	if !POIsService(po) {
		return nil, contractErr(codeSheetSave, "purchase order %s is not a service order", poID)
	}
	return lines, nil
}

// checkSheetLines holds a sheet's lines to its PO: every item is on the PO and
// no quantity passes what is still open.
func checkSheetLines(lines []map[string]interface{}, balance []serviceLine) error {
	if len(lines) == 0 {
		return contractErr(codeSheetSave, "add at least one line of work done")
	}
	open := map[string]serviceLine{}
	for _, b := range balance {
		open[strings.ToLower(b.Sku)] = b
	}
	seen := map[string]bool{}
	for i, l := range lines {
		sku := strings.TrimSpace(fmt.Sprintf("%v", orBlank(l["sku"])))
		if sku == "" {
			return contractErr(codeSheetSave, "line %d has no item", i+1)
		}
		key := strings.ToLower(sku)
		if seen[key] {
			return contractErr(codeSheetSave, "item %s is on the sheet twice", sku)
		}
		seen[key] = true
		b, ok := open[key]
		if !ok {
			return contractErr(codeSheetSave, "item %s is not on the purchase order", sku)
		}
		qty := numFromInterface(l["qty"])
		if qty <= 0 {
			return contractErr(codeSheetSave, "item %s: enter the quantity done", sku)
		}
		if qty > b.Open+0.0001 {
			return contractErr(codeSheetSave, "item %s: %g done is more than the %g still open (ordered %g, accepted %g)", sku, qty, b.Open, b.Ordered, b.Done)
		}
	}
	return nil
}

// validateServiceEntrySheetSave is the sheet's own save rule.
func validateServiceEntrySheetSave(tenantID, docID, priorStatus string, priorData, payload map[string]interface{}) error {
	written := resolveWrittenStatus(payload)
	if priorData == nil && written != "" && written != sheetDraft {
		return contractErr(codeSheetSave, "a new sheet starts as Draft; Accept it from its page")
	}
	if priorData != nil && written != "" && written != priorStatus {
		return contractErr(codeSheetSave, "a sheet becomes Accepted only through Accept on its page")
	}
	if priorStatus == sheetAccepted {
		return contractErr(codeSheetSave, "sheet %s is accepted and cannot be changed", docID)
	}
	poID := strField(payload, "po_id")
	balance, po, poStatus, err := serviceBalance(tenantID, poID, docID)
	if err != nil {
		return err
	}
	if !POIsService(po) {
		return contractErr(codeSheetSave, "purchase order %s is not a service order", poID)
	}
	if poStatus != "Approved" {
		return contractErr(codeSheetSave, "purchase order %s is %s: work is recorded only against an Approved service order", poID, poStatus)
	}
	if priorData != nil && strField(priorData, "po_id") != poID {
		return contractErr(codeSheetSave, "a sheet cannot be moved to another purchase order")
	}
	if strField(payload, "location") == "" {
		payload["location"] = strField(po, "location")
	}
	return checkSheetLines(docTableOf(payload["lines"]), balance)
}

// sheetValuePaise values a sheet's lines at the PO's net unit rates and groups
// them by the expense account each item's profile names.
func sheetValuePaise(tenantID string, lines []map[string]interface{}, balance []serviceLine) (int64, map[string]int64) {
	rate := map[string]float64{}
	for _, b := range balance {
		rate[strings.ToLower(b.Sku)] = b.Rate
	}
	byAccount := map[string]int64{}
	var total int64
	for _, l := range lines {
		sku := strings.TrimSpace(fmt.Sprintf("%v", orBlank(l["sku"])))
		v := RupeesToPaise(numFromInterface(l["qty"]) * rate[strings.ToLower(sku)])
		if v <= 0 {
			continue
		}
		account := AccountConsumablesExpense
		if policy, _, ok := ItemPolicyForSKU(tenantID, sku); ok && policy.ExpenseAccount != "" {
			account = policy.ExpenseAccount
		}
		byAccount[account] += v
		total += v
	}
	return total, byAccount
}

// AcceptServiceEntrySheet confirms the work on a Draft sheet and posts its
// accrual. Only the role that approves a PO of the accepted value may do it.
func AcceptServiceEntrySheet(tenantID, id, userID, role string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return err
	}
	var dataStr, status string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents
		WHERE doctype = $1 AND id = $2 AND deleted_at IS NULL FOR UPDATE`, schema), ServiceEntrySheetDoctype, id).Scan(&dataStr, &status); err != nil {
		return contractErr(codeSheetAction, "service entry sheet %s was not found", id)
	}
	if status != sheetDraft {
		return contractErr(codeSheetAction, "sheet %s is %s; only a Draft sheet can be accepted", id, status)
	}
	var sheet map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &sheet); err != nil {
		return err
	}
	poID := strField(sheet, "po_id")
	// One acceptance at a time per PO: each reads what the others took.
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1))`, schema+":service-entry:"+poID); err != nil {
		return err
	}
	balance, po, poStatus, err := serviceBalance(tenantID, poID, id)
	if err != nil {
		return err
	}
	if !POIsService(po) || poStatus != "Approved" {
		return contractErr(codeSheetAction, "purchase order %s must be an Approved service order", poID)
	}
	lines := docTableOf(sheet["lines"])
	if err := checkSheetLines(lines, balance); err != nil {
		return err
	}
	total, byAccount := sheetValuePaise(tenantID, lines, balance)
	if !contractApprovalGate(tenantID, role, PaiseToRupees(total)) {
		return contractErr(codeSheetAction, "accepting work of this value needs the role that approves purchase orders of that value")
	}
	if total > 0 {
		if err := PostDoubleEntryTx(tx, tenantID, schema, ServiceEntrySheetDoctype, id, byAccount, map[string]int64{"2100": total},
			"", fmt.Sprintf("ServiceEntrySheet:%s:ACCRUAL", id)); err != nil {
			return fmt.Errorf("service accrual: %w", err)
		}
	}
	stamp, _ := json.Marshal(map[string]interface{}{
		"status": sheetAccepted, "accepted_by": userID, "accepted_on": BusinessToday(), "accepted_value": PaiseToRupees(total)})
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = $1, data = data || $2::jsonb, updated_at = CURRENT_TIMESTAMP
		WHERE doctype = $3 AND id = $4`, schema), sheetAccepted, string(stamp), ServiceEntrySheetDoctype, id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "SERVICE_ENTRY_ACCEPT", "SUCCESS", fmt.Sprintf("service entry sheet %s accepted for PO %s, value %.2f", id, poID, PaiseToRupees(total)))
	return nil
}

// serviceBilling is what a service PO can be billed for: the accepted sheets'
// value (the named sheet, or all of them), grossed up by the PO's own tax ratio.
type serviceBilling struct {
	grnBilling
	AllAcceptedNet float64 // every accepted sheet of the PO
}

func readServiceBilling(tx queryRower, schema, poID, sheetID string) (serviceBilling, error) {
	var b serviceBilling
	var poDataStr string
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'PurchaseOrder' AND id = $1`, schema), poID).Scan(&poDataStr); err != nil {
		return b, fmt.Errorf("purchase order not found: %v", err)
	}
	var poData map[string]interface{}
	if err := json.Unmarshal([]byte(poDataStr), &poData); err != nil {
		return b, err
	}
	poNet := numFromInterface(poData["total_amount"])
	poGross := numFromInterface(poData["grand_total"]) - numFromInterface(poData["round_off"])
	taxRatio := 1.0
	if poGross > 0 && poNet > 0 {
		taxRatio = poGross / poNet
	} else {
		poGross = poNet
	}
	if sheetID != "" {
		var sheetPO string
		var st sql.NullString
		if err := tx.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'po_id',''), status FROM %s.documents
			WHERE doctype = 'ServiceEntrySheet' AND id = $1 AND deleted_at IS NULL`, schema), sheetID).Scan(&sheetPO, &st); err != nil {
			return b, fmt.Errorf("service entry sheet %s not found", sheetID)
		}
		if sheetPO != poID {
			return b, fmt.Errorf("service entry sheet %s is for PO %s, not %s", sheetID, sheetPO, poID)
		}
	}
	sum := func(onlySheet string) (float64, error) {
		var v float64
		err := tx.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(NULLIF(data->>'accepted_value','')::numeric),0)::float8
			FROM %s.documents WHERE doctype = 'ServiceEntrySheet' AND deleted_at IS NULL AND status = 'Accepted'
			  AND data->>'po_id' = $1 AND ($2 = '' OR id = $2)`, schema), poID, onlySheet).Scan(&v)
		return v, err
	}
	all, err := sum("")
	if err != nil {
		return b, err
	}
	net := all
	if sheetID != "" {
		if net, err = sum(sheetID); err != nil {
			return b, err
		}
	}
	b.grnBilling = grnBilling{POData: poData, TaxRatio: taxRatio, POGross: poGross, AcceptedNet: net, RateBySku: map[string]float64{}}
	b.AllAcceptedNet = all
	return b, nil
}

// matchServiceVendorInvoice is the three-way match of a bill against a service
// PO: the accepted service entry sheet stands where the GRN would. The bill
// names the sheet in service_entry_id (a blank one means every accepted sheet
// of the PO). Charges, bill lines and rate history do not apply to services.
func matchServiceVendorInvoice(tx *sql.Tx, tenantID, schema, invoiceID, poID string, invData map[string]interface{}, tolerancePercent float64) (VendorInvoiceMatch, error) {
	var out VendorInvoiceMatch
	sheetID := strField(invData, "service_entry_id")
	if sheetID == "" {
		return out, contractErr(codeServiceBillOver, "bill %s is for a service order: name its service entry sheet", invoiceID)
	}
	billing, err := readServiceBilling(tx, schema, poID, sheetID)
	if err != nil {
		return out, err
	}
	round2 := func(v float64) float64 { return math.Round(v*100) / 100 }
	invoiceAmount := numFromInterface(invData["invoice_amount"])
	taxRatio, poGross := billing.TaxRatio, billing.POGross
	expected := billing.AcceptedNet * taxRatio
	billed := func(field, ref string) (float64, error) {
		var total float64
		err := tx.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(NULLIF(data->>'invoice_amount','')::numeric),0)::float8
			FROM %s.documents WHERE doctype = 'VendorInvoice' AND id <> $1 AND deleted_at IS NULL
			  AND status IN (%s) AND data->>'%s' = $2`, schema, billableVendorInvoiceStatuses, field), invoiceID, ref).Scan(&total)
		return total, err
	}
	priorOnSheet, err := billed("service_entry_id", sheetID)
	if err != nil {
		return out, err
	}
	priorOnPO, err := billed("po_id", poID)
	if err != nil {
		return out, err
	}
	var reasons []string
	tol := expected * tolerancePercent / 100
	if expected <= 0 {
		reasons = append(reasons, fmt.Sprintf("Service entry sheet %s is not accepted, so there is no work to bill.", sheetID))
	} else {
		if math.Abs(invoiceAmount-expected) > tol {
			reasons = append(reasons, fmt.Sprintf("Invoice amount %.2f differs from the accepted work's value %.2f (accepted qty x PO rate, incl. tax) by more than %.2f%%.",
				invoiceAmount, round2(expected), tolerancePercent))
		}
		if priorOnSheet > 0 && priorOnSheet+invoiceAmount > expected+tol {
			reasons = append(reasons, fmt.Sprintf("Sheet %s is already billed %.2f; with this invoice that exceeds its value %.2f.", sheetID, round2(priorOnSheet), round2(expected)))
		}
	}
	if priorOnPO+invoiceAmount > poGross+poGross*tolerancePercent/100 {
		reasons = append(reasons, fmt.Sprintf("PO %s total is %.2f incl. tax; already billed %.2f, so this invoice would overbill it.", poID, round2(poGross), round2(priorOnPO)))
	}
	matched := len(reasons) == 0
	newStatus := "MismatchHold"
	if matched {
		newStatus = "Matched"
	}
	details := map[string]interface{}{
		"po_amount": round2(poGross), "po_tax_ratio": math.Round(taxRatio*10000) / 10000,
		"service_entry_value": round2(expected), "invoice_amount": invoiceAmount,
		"already_billed_po": round2(priorOnPO), "already_billed_sheet": round2(priorOnSheet),
		"tolerance_percent": tolerancePercent, "reasons": reasons,
	}
	gstPart := 0.0
	if taxRatio > 1 && invoiceAmount > 0 {
		gstPart = round2(invoiceAmount - invoiceAmount/taxRatio)
	}
	if blocked := blockedGSTShare(tenantID, billing.POData, gstPart); blocked > 0 {
		gstPart = round2(gstPart - blocked)
		invData["itc_blocked_amount"] = blocked
	} else {
		delete(invData, "itc_blocked_amount")
	}
	invData["taxable_amount"] = round2(invoiceAmount - gstPart)
	invData["gst_amount"] = gstPart
	details["taxable_amount"], details["gst_amount"] = invData["taxable_amount"], gstPart
	if bd, ok := billing.POData["gst_breakdown"].(map[string]interface{}); ok && gstPart > 0 {
		if total := numFromInterface(bd["total_tax"]); total > 0 {
			invData["gst_split"] = map[string]interface{}{
				"cgst": round2(gstPart * numFromInterface(bd["cgst"]) / total),
				"sgst": round2(gstPart * numFromInterface(bd["sgst"]) / total),
				"igst": round2(gstPart * numFromInterface(bd["igst"]) / total),
			}
		}
	}
	invData["status"] = newStatus
	invData["match_details"] = details
	updated, _ := json.Marshal(invData)
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'VendorInvoice' AND id = $3`, schema), updated, newStatus, invoiceID); err != nil {
		return out, err
	}
	if err := tx.Commit(); err != nil {
		return out, err
	}
	LogAuditEvent(tenantID, "system", "MATCH_3WAY_VENDOR_INVOICE", "SUCCESS",
		fmt.Sprintf("service match for invoice %s (PO %s, sheet %s): %s (expected=%.2f invoice=%.2f)", invoiceID, poID, sheetID, newStatus, expected, invoiceAmount))
	return VendorInvoiceMatch{Matched: matched, Status: newStatus, POID: poID, Details: details, Reasons: reasons}, nil
}
