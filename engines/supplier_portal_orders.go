package engines

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"custom_erp/db"
)

// Supplier portal: orders, ASNs, bills and payments (PROC > Vendors).
//
// A Supplier login is linked to one vendor (users.supplier_code). Everything
// here is read-only except SendSupplierASN, and every query is fenced to that
// vendor and the caller's own client. A record of another vendor is the same
// ErrSupplierNotFound as one that does not exist, so ids cannot be probed.
//
// What a supplier sees is a whitelist, built field by field below. Never shown:
// rates, MRP, discounts, line and order totals, charges, freight, tax splits,
// source requisitions / RFQs / quotes, internal notes, who approved what, bill
// match details and the GL postings. A bill's own amounts, dates and payments
// are the supplier's own money and are shown.

const (
	SupplierPOClosedCode  = "VENDOR-0310"
	SupplierASNQtyCode    = "VENDOR-0311"
	SupplierASNLinesCode  = "VENDOR-0312"
	supplierListLimit     = 200
	supplierPOStatusOpen  = "Approved"
	supplierPOStatusClose = "Closed"
)

// ErrSupplierNotFound is returned for a record that is not the supplier's.
var ErrSupplierNotFound = errors.New("not found")

// ErrSupplierNotLinked is returned when the login has no vendor yet.
var ErrSupplierNotLinked = errors.New("this supplier account is not linked to a vendor yet")

// IsSupplierRole reports whether a role is the outside-supplier login.
func IsSupplierRole(role string) bool {
	return strings.EqualFold(strings.TrimSpace(role), "Supplier")
}

// SupplierVendor is the vendor a Supplier login speaks for.
type SupplierVendor struct {
	ID, Code, Name string
}

// ResolveSupplierVendor finds the vendor a login is linked to, in its own client.
func ResolveSupplierVendor(tenantID, userID string) (*SupplierVendor, error) {
	code, err := SupplierCodeForUser(tenantID, userID)
	if err != nil {
		return nil, err
	}
	if code == "" {
		return nil, ErrSupplierNotLinked
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	id, v, ok := findVendorRow(db.DB, schema, code)
	if !ok {
		return nil, ErrSupplierNotLinked
	}
	return &SupplierVendor{ID: id, Code: firstNonEmpty(strField(v, "code"), id), Name: strField(v, "name")}, nil
}

// vendorMatch is the SQL that fences a document to the vendor: the vendor
// field of a PO / ASN or the vendor_id of a bill holds the vendor's id or code.
func vendorMatch(alias string) string {
	return fmt.Sprintf("(%[1]sdata->>'vendor' IN ($1, $2) OR %[1]sdata->>'vendor_id' IN ($1, $2))", alias)
}

type supplierDoc struct {
	ID     string
	Status string
	Data   map[string]interface{}
}

func supplierDocs(schema, doctype, extraWhere string, v *SupplierVendor, args ...interface{}) ([]supplierDoc, error) {
	q := fmt.Sprintf(`SELECT id, status, data FROM %s.documents WHERE doctype = '%s' AND deleted_at IS NULL AND %s %s
		ORDER BY created_at DESC LIMIT %d`, schema, doctype, vendorMatch(""), extraWhere, supplierListLimit)
	rows, err := db.DB.Query(q, append([]interface{}{v.ID, v.Code}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []supplierDoc
	for rows.Next() {
		var d supplierDoc
		var raw []byte
		if err := rows.Scan(&d.ID, &d.Status, &raw); err != nil {
			return nil, err
		}
		if json.Unmarshal(raw, &d.Data) != nil {
			continue
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func supplierOrderView(tenantID string, d supplierDoc) map[string]interface{} {
	ordered, _ := fetchPOItemQuantities(tenantID, d.ID)
	received, _ := fetchGRNReceivedQuantities(tenantID, d.ID, "")
	var lines []map[string]interface{}
	totalOpen := 0.0
	for _, ln := range jsonRows(d.Data["items"]) {
		sku := strField(ln, "sku")
		qty := numFromInterface(ln["qty"]) + numFromInterface(ln["free_qty"])
		closed := numFromInterface(ln["closed_qty"])
		open := ordered[sku] - received[sku]
		if open < 0 {
			open = 0
		}
		totalOpen += open
		lines = append(lines, map[string]interface{}{"sku": sku, "qty": qty, "closed_qty": closed, "received_qty": received[sku], "open_qty": open})
	}
	return map[string]interface{}{
		"id": d.ID, "po_number": firstNonEmpty(strField(d.Data, "po_number"), d.ID), "status": d.Status,
		"expected_date": strField(d.Data, "expected_delivery_date"), "location": strField(d.Data, "location"),
		"lines": lines, "open_qty": totalOpen,
	}
}

// SupplierOrders lists the vendor's approved and closed orders. A draft or an
// order still waiting for approval is internal and not shown.
func SupplierOrders(tenantID string, v *SupplierVendor) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	docs, err := supplierDocs(schema, "PurchaseOrder", "AND status IN ('Approved', 'Closed')", v)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]interface{}, 0, len(docs))
	for _, d := range docs {
		out = append(out, supplierOrderView(tenantID, d))
	}
	return out, nil
}

// SupplierOrder is one of the vendor's orders, or ErrSupplierNotFound.
func SupplierOrder(tenantID string, v *SupplierVendor, id string) (map[string]interface{}, error) {
	d, err := supplierPO(tenantID, v, id)
	if err != nil {
		return nil, err
	}
	return supplierOrderView(tenantID, *d), nil
}

func supplierPO(tenantID string, v *SupplierVendor, id string) (*supplierDoc, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	docs, err := supplierDocs(schema, "PurchaseOrder", "AND status IN ('Approved', 'Closed') AND id = $3", v, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, ErrSupplierNotFound
	}
	return &docs[0], nil
}

func supplierASNView(d supplierDoc) map[string]interface{} {
	var lines []map[string]interface{}
	for _, ln := range jsonRows(d.Data["expected_items"]) {
		lines = append(lines, map[string]interface{}{"sku": strField(ln, "sku"), "qty": numFromInterface(ln["qty"])})
	}
	return map[string]interface{}{
		"id": d.ID, "asn_number": firstNonEmpty(strField(d.Data, "asn_number"), d.ID), "po_id": strField(d.Data, "po_id"),
		"po_number": strField(d.Data, "po_number"), "status": d.Status, "expected_date": strField(d.Data, "expected_date"),
		"carrier": strField(d.Data, "carrier"), "tracking_number": strField(d.Data, "tracking_number"), "lines": lines,
	}
}

// supplierASNScope: an ASN is the vendor's by its own vendor field, or by the
// order it names being the vendor's.
func supplierASNDocs(schema string, v *SupplierVendor, id string) ([]supplierDoc, error) {
	where := fmt.Sprintf(`AND NOT %s`, "false")
	_ = where
	q := fmt.Sprintf(`SELECT a.id, a.status, a.data FROM %[1]s.documents a
		WHERE a.doctype = 'ASN' AND a.deleted_at IS NULL AND ($3 = '' OR a.id = $3)
		  AND (a.data->>'vendor' IN ($1, $2) OR a.data->>'po_id' IN (
		        SELECT p.id FROM %[1]s.documents p WHERE p.doctype = 'PurchaseOrder' AND p.deleted_at IS NULL AND %[2]s))
		ORDER BY a.created_at DESC LIMIT %[3]d`, schema, vendorMatch("p."), supplierListLimit)
	rows, err := db.DB.Query(q, v.ID, v.Code, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []supplierDoc
	for rows.Next() {
		var d supplierDoc
		var raw []byte
		if err := rows.Scan(&d.ID, &d.Status, &raw); err != nil {
			return nil, err
		}
		if json.Unmarshal(raw, &d.Data) == nil {
			out = append(out, d)
		}
	}
	return out, rows.Err()
}

// SupplierASNs lists the vendor's advance ship notices.
func SupplierASNs(tenantID string, v *SupplierVendor) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	docs, err := supplierASNDocs(schema, v, "")
	if err != nil {
		return nil, err
	}
	out := make([]map[string]interface{}, 0, len(docs))
	for _, d := range docs {
		out = append(out, supplierASNView(d))
	}
	return out, nil
}

// SupplierASN is one of the vendor's notices, or ErrSupplierNotFound.
func SupplierASN(tenantID string, v *SupplierVendor, id string) (map[string]interface{}, error) {
	if strings.TrimSpace(id) == "" {
		return nil, ErrSupplierNotFound
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	docs, err := supplierASNDocs(schema, v, id)
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, ErrSupplierNotFound
	}
	return supplierASNView(docs[0]), nil
}

func supplierBillView(d supplierDoc) map[string]interface{} {
	match, pay := "Not matched", "Unpaid"
	switch d.Status {
	case "Matched", "Approved", "Paid":
		match = "Matched"
	case "MismatchHold":
		match = "On hold"
	}
	paid := numFromInterface(d.Data["paid_amount"])
	switch {
	case d.Status == "Paid":
		pay = "Paid"
	case paid > 0:
		pay = "Part paid"
	}
	amount := numFromInterface(d.Data["invoice_amount"])
	outstanding := amount - paid
	if v, ok := d.Data["outstanding_amount"]; ok && v != nil {
		outstanding = numFromInterface(v)
	}
	if d.Status == "Paid" {
		outstanding = 0
	}
	return map[string]interface{}{
		"id": d.ID, "invoice_number": strField(d.Data, "invoice_number"), "invoice_date": strField(d.Data, "invoice_date"),
		"due_date": strField(d.Data, "due_date"), "po_id": strField(d.Data, "po_id"),
		"amount": amount, "paid_amount": paid, "outstanding_amount": outstanding,
		"match_status": match, "payment_status": pay,
	}
}

// SupplierBills lists the vendor's bills with their match and payment status.
func SupplierBills(tenantID string, v *SupplierVendor) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	docs, err := supplierDocs(schema, "VendorInvoice", "AND status <> 'Draft'", v)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]interface{}, 0, len(docs))
	for _, d := range docs {
		out = append(out, supplierBillView(d))
	}
	return out, nil
}

// SupplierBill is one of the vendor's bills, or ErrSupplierNotFound.
func SupplierBill(tenantID string, v *SupplierVendor, id string) (map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	docs, err := supplierDocs(schema, "VendorInvoice", "AND status <> 'Draft' AND id = $3", v, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, ErrSupplierNotFound
	}
	return supplierBillView(docs[0]), nil
}

// SupplierPayments lists what was paid against the vendor's bills: date, amount
// and reference, newest first. The ledger keys and who paid stay internal.
func SupplierPayments(tenantID string, v *SupplierVendor) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	docs, err := supplierDocs(schema, "VendorInvoice", "AND status <> 'Draft'", v)
	if err != nil {
		return nil, err
	}
	out := []map[string]interface{}{}
	for _, d := range docs {
		for _, p := range jsonRows(d.Data["payments"]) {
			if strField(p, "kind") != "" && strField(p, "kind") != settlementKindPayment {
				continue
			}
			out = append(out, map[string]interface{}{
				"date": strField(p, "date"), "amount": numFromInterface(p["amount"]),
				"reference": strField(p, "reference"), "bill_id": d.ID, "invoice_number": strField(d.Data, "invoice_number"),
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return strField(out[i], "date") > strField(out[j], "date") })
	return out, nil
}

// SupplierASNInput is what a supplier sends for one of its open orders.
type SupplierASNInput struct {
	POID           string  `json:"po_id"`
	ExpectedDate   string  `json:"expected_date"`
	Carrier        string  `json:"carrier"`
	TrackingNumber string  `json:"tracking_number"`
	Lines          []ASNQL `json:"lines"`
}

// ASNQL is one line of a supplier's notice.
type ASNQL struct {
	Sku string  `json:"sku"`
	Qty float64 `json:"qty"`
}

// SendSupplierASN files an Expected ASN against one of the vendor's own open
// orders, through the ordinary ASN rules. A line may not exceed what the order
// still has open (ordered, less closed, received and already on other open
// notices). The check and the insert share one advisory lock per order, so two
// notices sent at the same moment cannot both take the same open quantity.
func SendSupplierASN(tenantID, userID string, v *SupplierVendor, in SupplierASNInput) (map[string]interface{}, error) {
	po, err := supplierPO(tenantID, v, in.POID)
	if err != nil {
		return nil, err // another vendor's order is a plain not-found
	}
	if po.Status != supplierPOStatusOpen {
		return nil, &ValidationError{Code: SupplierPOClosedCode, SubFor: "ASN", Message: fmt.Sprintf("Order %s is %s - a notice can only be sent for an open order", po.ID, po.Status)}
	}
	want := map[string]float64{}
	var order []string
	for _, ln := range in.Lines {
		sku := strings.TrimSpace(ln.Sku)
		if sku == "" || ln.Qty <= 0 {
			return nil, &ValidationError{Code: SupplierASNLinesCode, SubFor: "ASN", Message: "Every line needs an item and a quantity above zero"}
		}
		if _, seen := want[sku]; !seen {
			order = append(order, sku)
		}
		want[sku] += ln.Qty
	}
	if len(want) == 0 {
		return nil, &ValidationError{Code: SupplierASNLinesCode, SubFor: "ASN", Message: "Add at least one line"}
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
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1))`, tenantID+":supplier-asn:"+po.ID); err != nil {
		return nil, err
	}
	ordered, err := fetchPOItemQuantities(tenantID, po.ID)
	if err != nil {
		return nil, err
	}
	received, err := fetchGRNReceivedQuantities(tenantID, po.ID, "")
	if err != nil {
		return nil, err
	}
	onNotices := map[string]float64{}
	rows, err := tx.Query(fmt.Sprintf(`SELECT data->>'expected_items' FROM %s.documents WHERE doctype = 'ASN' AND deleted_at IS NULL AND status = 'Expected' AND data->>'po_id' = $1`, schema), po.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var items sql.NullString
		if err := rows.Scan(&items); err != nil {
			rows.Close()
			return nil, err
		}
		for _, ln := range jsonRows(items.String) {
			onNotices[strField(ln, "sku")] += numFromInterface(ln["qty"])
		}
	}
	rows.Close()
	var expected []map[string]interface{}
	for _, sku := range order {
		total, onOrder := ordered[sku]
		if !onOrder {
			return nil, &ValidationError{Code: "ASN-0271", SubFor: "ASN", Message: fmt.Sprintf("SKU %q is not part of order %s", sku, po.ID)}
		}
		open := total - received[sku] - onNotices[sku]
		if open < 0 {
			open = 0
		}
		if want[sku] > open {
			return nil, &ValidationError{Code: SupplierASNQtyCode, SubFor: "ASN", Message: fmt.Sprintf(
				"%s: %g is more than the %g still open on order %s", sku, want[sku], open, po.ID)}
		}
		expected = append(expected, map[string]interface{}{"sku": sku, "qty": want[sku]})
	}
	itemsJSON, _ := json.Marshal(expected)
	payload := map[string]interface{}{
		"po_id": po.ID, "po_number": firstNonEmpty(strField(po.Data, "po_number"), po.ID),
		"vendor":   firstNonEmpty(strField(po.Data, "vendor"), strField(po.Data, "vendor_id")),
		"location": strField(po.Data, "location"), "status": "Expected", "expected_items": string(itemsJSON),
		"expected_date": strings.TrimSpace(in.ExpectedDate), "carrier": strings.TrimSpace(in.Carrier),
		"tracking_number": strings.TrimSpace(in.TrackingNumber),
	}
	if d := payload["expected_date"].(string); d != "" {
		if _, err := time.Parse("2006-01-02", d); err != nil {
			return nil, &ValidationError{Code: SupplierASNLinesCode, SubFor: "ASN", Message: "The expected date must be a date (YYYY-MM-DD)"}
		}
	}
	if err := PrepareDocumentNumber(tenantID, strField(po.Data, "location"), "ASN", true, payload); err != nil {
		return nil, err
	}
	if err := ValidateDocument(tenantID, "ASN", payload); err != nil {
		return nil, err
	}
	if err := ValidateTransactionalRules(tenantID, "ASN", "", "", nil, payload); err != nil {
		return nil, err
	}
	id := strField(payload, "id")
	delete(payload, "id")
	b, _ := json.Marshal(payload)
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'ASN', $2, 'Expected', $3)`, schema), id, b, userID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, userID, "SUPPLIER_ASN_SENT", "SUCCESS", fmt.Sprintf("Vendor %s sent notice %s for order %s", v.ID, id, po.ID))
	return supplierASNView(supplierDoc{ID: id, Status: "Expected", Data: payload}), nil
}
