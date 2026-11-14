package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// Stage 148.28: returns drive the next document.
//
// A ReturnRequest already has its number (the RMA no.), the sale's cost on every
// line (148.1) and QC dispositions. This adds, all off until an admin acts:
//   - sales.return_reason_required: a return is refused without a reason;
//   - ReturnDispositionRule rows: per disposition, a restocking fee (a charge code
//     and a percent) and what the line drafts - a credit note, a replacement order,
//     a return to the vendor, or a scrap to quarantine. No row = no fee, no draft;
//   - sales.credit_note_needs_receipt: a credit note on stock items is posted only
//     once the return it names has been received and checked in.
//
// Drafts only: a person posts them, and a return line is never drafted twice.
// Revenue, GST and accounts of the return itself follow 148.4 (unchanged).

const (
	SettingReturnReasonRequired   = "sales.return_reason_required"
	SettingCreditNoteNeedsReceipt = "sales.credit_note_needs_receipt"

	returnActionCredit  = "Credit"
	returnActionReplace = "Replace"
	returnActionRTV     = "Return to vendor"
	returnActionScrap   = "Scrap"
)

// returnDispRule is one ReturnDispositionRule row, with its charge code resolved.
type returnDispRule struct {
	Action     string
	FeePercent float64
	FeeGL      string // "" = no fee: no charge code, or one that is not on file or is inactive
	// FeeGSTRate (148.28a) is the charge code's GST rate: the fee includes GST at it.
	FeeGSTRate float64
}

type returnRuleSet map[string]returnDispRule

// loadReturnDispositionRules reads the active rules (the first row per disposition wins).
func loadReturnDispositionRules(q queryRower, schema string) returnRuleSet {
	out := returnRuleSet{}
	rows, err := q.Query(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'ReturnDispositionRule' AND deleted_at IS NULL
		   AND COALESCE(data->>'status', 'Active') <> 'Inactive' ORDER BY created_at, id`, schema))
	if err != nil {
		return out
	}
	var docs []map[string]interface{}
	for rows.Next() {
		var raw []byte
		var d map[string]interface{}
		if rows.Scan(&raw) == nil && json.Unmarshal(raw, &d) == nil {
			docs = append(docs, d)
		}
	}
	rows.Close()
	if len(docs) == 0 {
		return out
	}
	codes, _ := loadTradeChargeCodes(q, schema)
	for _, d := range docs {
		disp := strField(d, "disposition")
		if _, dup := out[disp]; disp == "" || dup {
			continue
		}
		r := returnDispRule{Action: strField(d, "action"), FeePercent: numFromInterface(d["fee_percent"])}
		if c := codes[strField(d, "charge_code")]; c != nil && c.Active && r.FeePercent > 0 {
			r.FeeGL = firstNonEmpty(c.GL, "4100")
			r.FeeGSTRate = c.GSTRate
		}
		out[disp] = r
	}
	return out
}

// fee is the restocking fee on moneyPaise of a line given this disposition: none
// without a rule that names a live charge code.
func (rs returnRuleSet) fee(disposition string, moneyPaise int64) (int64, string) {
	r, ok := rs[disposition]
	if !ok || r.FeeGL == "" || moneyPaise <= 0 {
		return 0, ""
	}
	fee := int64(math.Round(float64(moneyPaise) * r.FeePercent / 100))
	if fee > moneyPaise {
		fee = moneyPaise
	}
	return fee, r.FeeGL
}

// feeGST is the GST inside a restocking fee (148.28a, user 2026-11-13: the fee
// is GST-inclusive at its charge code's rate); 0 when the code has no rate.
func (rs returnRuleSet) feeGST(disposition string, fee int64) int64 {
	rate := rs[disposition].FeeGSTRate
	if fee <= 0 || rate <= 0 {
		return 0
	}
	return int64(math.Round(float64(fee) * rate / (100 + rate)))
}

func (rs returnRuleSet) action(disposition string) string { return rs[disposition].Action }

// takeFeeOffTenders lowers the money tenders by the fee, spread by size, so the
// refund's credit legs still add up to what is paid back. 148.28a uses it the
// same way to take the fee's GST off the fee accounts.
func takeFeeOffTenders(tenders map[string]int64, fee int64) {
	keys := make([]string, 0, len(tenders))
	for k, v := range tenders {
		if k != TenderLoyaltyPoints && v > 0 {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	weights := make([]int64, len(keys))
	for i, k := range keys {
		weights[i] = tenders[k]
	}
	for i, share := range spreadPaise(fee, weights) {
		tenders[keys[i]] -= share
		if tenders[keys[i]] <= 0 {
			delete(tenders, keys[i])
		}
	}
}

// ValidateReturnReason is the one check on a new return's reason: with
// sales.return_reason_required on, a return needs a reason (a code or text);
// a code is always checked against the Sales Return reasons. It returns the
// code and text to store.
func ValidateReturnReason(tenantID, code, text string) (string, string, error) {
	code, text = strings.TrimSpace(code), strings.TrimSpace(text)
	if code == "" && text == "" && GetSettingBool(tenantID, SettingReturnReasonRequired) {
		return "", "", &ValidationError{Code: "SALESR-1315", SubFor: "reason", Message: "a return needs a reason"}
	}
	if code == "" && text != "" && !ReasonCodesRequired(tenantID) { // 74.1.1: free text alone only where codes are not required
		return "", text, nil
	}
	if code == "" {
		if text != "" { // codes are required: the typed text alone is refused
			return ReasonForException(tenantID, ReasonSalesReturn, "", text)
		}
		return "", "", nil
	}
	return ReasonForException(tenantID, ReasonSalesReturn, code, text)
}

// StampReturnReason writes the reason on a return just raised.
func StampReturnReason(tenantID, returnID, code, text string) error {
	if code == "" && text == "" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = data || jsonb_build_object('reason_code', $1::text, 'reason', $2::text), updated_at = CURRENT_TIMESTAMP
		  WHERE doctype = 'ReturnRequest' AND id = $3`, schema), code, text, returnID)
	return err
}

// creditNoteHasStock: a note that names pieces or item lines moves stock.
// creditNoteLineMaps reads a credit note's lines, saved as a list or a JSON string.
func creditNoteLineMaps(data map[string]interface{}) []map[string]interface{} {
	var lines []map[string]interface{}
	switch v := data["items"].(type) {
	case string:
		_ = json.Unmarshal([]byte(v), &lines)
	case []interface{}:
		raw, _ := json.Marshal(v)
		_ = json.Unmarshal(raw, &lines)
	}
	return lines
}

func creditNoteHasStock(data map[string]interface{}) bool {
	if strField(data, "serial_no") != "" {
		return true
	}
	for _, l := range creditNoteLineMaps(data) {
		if strField(l, "sku") != "" {
			return true
		}
	}
	return false
}

// checkCreditNoteReceiptTx refuses a credit note on stock items whose return has
// not been received and checked in, when sales.credit_note_needs_receipt is on.
func checkCreditNoteReceiptTx(tx *sql.Tx, tenantID, schema string, note map[string]interface{}) error {
	if !GetSettingBool(tenantID, SettingCreditNoteNeedsReceipt) || !creditNoteHasStock(note) {
		return nil
	}
	refuse := &ValidationError{Code: "SALESR-1316", SubFor: "return_request_id",
		Message: "a credit note on stock items needs a return that has been received and checked in"}
	rid := strField(note, "return_request_id")
	if rid == "" {
		return refuse
	}
	var status string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'status', status) FROM %s.documents WHERE doctype = 'ReturnRequest' AND id = $1`, schema), rid).Scan(&status); err != nil {
		return refuse
	}
	if status != "QC Complete" && status != "Closed" {
		return refuse
	}
	return nil
}

// returnOriginRef finds the customer and the document a credit note should name.
func returnOriginRef(q queryRower, schema, originalID string) (customer, invoiceID, cartID string) {
	var doctype, cust string
	_ = q.QueryRow(fmt.Sprintf(`SELECT doctype, COALESCE(data->>'customer_id', data->>'customer', '') FROM %s.documents WHERE id = $1`, schema), originalID).Scan(&doctype, &cust)
	switch doctype {
	case "SalesInvoice":
		return cust, originalID, ""
	case "POSCart":
		return cust, "", originalID
	}
	return cust, "", ""
}

// draftCreditNoteTx drafts the credit note for a return's Credit lines inside the
// QC transaction (the same one that left those lines out of the refund).
func draftCreditNoteTx(tx *sql.Tx, tenantID, schema, returnID string, rr map[string]interface{}, lines []returnItemRecord, paise int64) (string, error) {
	noteID := NewDocID("CRN")
	customer, invoiceID, cartID := returnOriginRef(tx, schema, strField(rr, "original_order_id"))
	items := make([]map[string]interface{}, 0, len(lines))
	for _, l := range lines {
		items = append(items, map[string]interface{}{"sku": l.SKU, "qty": l.Qty})
	}
	note := map[string]interface{}{
		"code": noteID, "customer_id": customer, "amount": PaiseToRupees(paise),
		"reason": "Return " + firstNonEmpty(strField(rr, "rma_no"), strField(rr, "code"), returnID), "status": "Draft",
		"items": items, "return_request_id": returnID, "return_location": strField(rr, "return_location"),
	}
	if invoiceID != "" {
		note["sales_invoice_id"] = invoiceID
	}
	if cartID != "" {
		note["reference_cart"] = cartID
	}
	note["note_number"] = IssueDocumentNumber(tenantID, "CreditNote", noteID, "", strField(rr, "return_location"))
	raw, _ := json.Marshal(note)
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'CreditNote', $2, 'Draft', 'system')`, schema), noteID, raw); err != nil {
		return "", err
	}
	return noteID, nil
}

// returnDraft is one entry of ReturnRequest.drafts: what a line drafted, or why not.
type returnDraft struct {
	Line    int    `json:"line"`
	SKU     string `json:"sku"`
	Action  string `json:"action"`
	Doctype string `json:"doctype,omitempty"`
	ID      string `json:"id,omitempty"`
	Skipped string `json:"skipped,omitempty"`
}

func readReturnDrafts(raw interface{}) []returnDraft {
	var out []returnDraft
	b, _ := json.Marshal(raw)
	_ = json.Unmarshal(b, &out)
	return out
}

// DraftReturnNextDocuments drafts, for a checked-in return, the replacement
// order, the purchase return and the scrap note its lines' dispositions call for.
// A line is drafted once: the result is written on the return. A line that cannot
// be drafted (no delivery address, no goods receipt) says why and is left to a
// person. Returns how many documents it drafted.
func DraftReturnNextDocuments(tenantID, returnID, user string) (int, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return 0, err
	}
	var raw []byte
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'ReturnRequest' AND id = $1 FOR UPDATE`, schema), returnID).Scan(&raw); err != nil {
		return 0, err
	}
	var data map[string]interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return 0, err
	}
	if s := strField(data, "status"); s != "QC Complete" && s != "Closed" {
		return 0, nil
	}
	var items []returnItemRecord
	b, _ := json.Marshal(data["items"])
	if err := json.Unmarshal(b, &items); err != nil {
		return 0, err
	}
	drafts := readReturnDrafts(data["drafts"])
	done := map[int]bool{}
	for _, d := range drafts {
		done[d.Line] = true
	}
	made := 0
	for i, it := range items {
		if it.NextAction == "" || done[i] {
			continue
		}
		d := returnDraft{Line: i, SKU: it.SKU, Action: it.NextAction}
		switch it.NextAction {
		case returnActionReplace:
			d.Doctype, d.ID, d.Skipped, err = draftReplacementOrder(tenantID, schema, returnID, data, it)
		case returnActionRTV:
			d.Doctype, d.ID, d.Skipped, err = draftPurchaseReturnTx(tx, tenantID, schema, returnID, data, it)
		case returnActionScrap:
			d.Doctype = "InventoryAdjustment"
			d.ID, err = draftScrapAdjustmentTx(tx, tenantID, schema, returnID, data, it)
		default:
			continue // Credit was drafted at QC
		}
		if err != nil {
			return made, err
		}
		if d.ID != "" {
			made++
		}
		drafts = append(drafts, d)
	}
	if len(drafts) == len(readReturnDrafts(data["drafts"])) {
		return 0, nil
	}
	data["drafts"] = drafts
	out, _ := json.Marshal(data)
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'ReturnRequest' AND id = $2`, schema), out, returnID); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	if made > 0 {
		LogAuditEvent(tenantID, user, "DRAFT_RETURN_DOCUMENTS", "SUCCESS", fmt.Sprintf("Drafted %d document(s) from return %s", made, returnID))
	}
	return made, nil
}

// draftReplacementOrder drafts a Pending sales order for the same item to the
// address the original online order went to. A till bill has no delivery address,
// so it is left to a person.
func draftReplacementOrder(tenantID, schema, returnID string, rr map[string]interface{}, it returnItemRecord) (doctype, id, skipped string, err error) {
	orig := strField(rr, "original_order_id")
	var raw []byte
	qerr := db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'SalesOrder' AND deleted_at IS NULL AND
		(id = $1 OR id = (SELECT data->>'sales_order_id' FROM %s.documents WHERE doctype = 'SalesInvoice' AND id = $1)) LIMIT 1`, schema, schema), orig).Scan(&raw)
	if qerr != nil {
		return "", "", "no delivery address on the sale", nil
	}
	var so map[string]interface{}
	if json.Unmarshal(raw, &so) != nil || strField(so, "shipping_address") == "" {
		return "", "", "no delivery address on the sale", nil
	}
	in := SalesOrderInput{
		Channel: "Replacement", CustomerName: strField(so, "customer_name"), CustomerID: strField(so, "customer_id"),
		ShippingAddress: strField(so, "shipping_address"), ShippingState: strField(so, "shipping_state"), ShippingPincode: strField(so, "shipping_pincode"),
		ShippingCity: strField(so, "shipping_city"), ShippingLine2: strField(so, "shipping_address_line2"),
		CustomerPhone: strField(so, "customer_phone"), PaymentStatus: "Prepaid", PreferredLocation: strField(rr, "return_location"),
		Lines: []SalesOrderLineInput{{SKU: it.SKU, Qty: it.Qty, UnitPrice: 0}},
	}
	orderID, cerr := CreateSalesOrder(tenantID, in)
	if cerr != nil {
		return "", "", "order could not be drafted: " + cerr.Error(), nil
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || jsonb_build_object('return_request_id', $1::text), updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'SalesOrder' AND id = $2`, schema), returnID, orderID); err != nil {
		return "", "", "", err
	}
	return "SalesOrder", orderID, "", nil
}

// draftPurchaseReturnTx drafts a purchase return on the latest goods receipt that
// can still take the item back; none = left to a person. A memo receipt is left
// out: memo stock goes back to its vendor with no debit note (148.16).
func draftPurchaseReturnTx(tx *sql.Tx, tenantID, schema, returnID string, rr map[string]interface{}, it returnItemRecord) (doctype, id, skipped string, err error) {
	rows, qerr := tx.Query(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'GRN' AND deleted_at IS NULL AND status NOT IN ('Draft', 'Cancelled', 'Rejected')
		AND position($1 in data::text) > 0 AND lower(COALESCE(data->>'memo_stock', '')) <> 'yes' ORDER BY created_at DESC LIMIT 20`, schema), it.SKU)
	if qerr != nil {
		return "", "", "", qerr
	}
	var grns []string
	for rows.Next() {
		var g string
		if rows.Scan(&g) == nil {
			grns = append(grns, g)
		}
	}
	rows.Close()
	for _, g := range grns {
		ctx, cerr := purchaseReturnable(tx, tenantID, schema, g, "", false)
		if cerr != nil || ctx == nil {
			continue
		}
		for _, l := range ctx.Lines {
			if l.SKU == it.SKU && l.StockBucket == PurchaseReturnBucketAccepted && l.Returnable >= float64(it.Qty) {
				prID := NewDocID("PRT")
				doc := map[string]interface{}{
					"code": prID, "grn_id": g, "vendor_id": ctx.VendorID, "po_id": ctx.POID, "location": ctx.Location,
					"reason": "Customer return " + firstNonEmpty(strField(rr, "rma_no"), strField(rr, "code"), returnID), "status": "Draft",
					"return_items":      []map[string]interface{}{{"sku": it.SKU, "batch_no": l.BatchNo, "stock_bucket": PurchaseReturnBucketAccepted, "qty": it.Qty}},
					"return_request_id": returnID,
				}
				issueNumberInto(doc, tenantID, "PurchaseReturn", prID, "", ctx.Location)
				raw, _ := json.Marshal(doc)
				if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'PurchaseReturn', $2, 'Draft', 'system')`, schema), prID, raw); err != nil {
					return "", "", "", err
				}
				return "PurchaseReturn", prID, "", nil
			}
		}
	}
	return "", "", "no goods receipt can take it back", nil
}

// feeByGLFromDoc reads the fee-by-account map stored on a RefundRequest.
func feeByGLFromDoc(v interface{}) map[string]int64 {
	out := map[string]int64{}
	if m, ok := v.(map[string]interface{}); ok {
		for k, x := range m {
			out[k] = int64(numFromInterface(x))
		}
	}
	return out
}

// draftScrapAdjustmentTx drafts the write-off note for a unit held in quarantine.
// A person posts it after looking at the piece.
func draftScrapAdjustmentTx(tx *sql.Tx, tenantID, schema, returnID string, rr map[string]interface{}, it returnItemRecord) (string, error) {
	id := NewDocID("ADJ")
	loc := strField(rr, "return_location")
	// The unit is in quarantine, so the line draws from there (pool) and carries
	// the cost the write-off books.
	var unit float64
	if item, ierr := ResolveItemBySKU(tenantID, it.SKU); ierr == nil && item != nil {
		unit = adjustmentUnitCost(tenantID, it.SKU, item.Data)
	}
	value := round2(unit * float64(it.Qty))
	lines := []map[string]interface{}{{"sku": it.SKU, "qty": -it.Qty, "note": "Scrap", "pool": AdjustmentPoolQCHold, "unit_cost": unit, "value": value}}
	linesJSON, _ := json.Marshal(lines)
	doc := map[string]interface{}{
		"code": id, "location": loc, "note": "Scrap from return " + firstNonEmpty(strField(rr, "rma_no"), strField(rr, "code"), returnID),
		"items": string(linesJSON), "loss_class": "Damage", "status": "Draft", "return_request_id": returnID, "decrease_value": value,
	}
	issueNumberInto(doc, tenantID, "InventoryAdjustment", id, "", loc)
	raw, _ := json.Marshal(doc)
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'InventoryAdjustment', $2, 'Draft', 'system')`, schema), id, raw); err != nil {
		return "", err
	}
	return id, nil
}
