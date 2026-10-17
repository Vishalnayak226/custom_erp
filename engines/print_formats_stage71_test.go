package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// TestStage71PrintFormats covers the standard print formats pack: the GRN's
// ordered qty, QC status and reasons; the vendor copy carrying no value at
// all; a transfer receipt printing against what was shipped (a short line
// used to print its received qty as "Sent"); the new Purchase Return, Sales
// Return and customer Credit Note prints; and the audit / financial-year
// fields every print carries.
func TestStage71PrintFormats(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const (
		sku, vendor, po, grn = "TEST71-ITEM", "TEST71-VEND", "TEST71-PO", "TEST71-GRN"
		to, prt, ret, cn     = "TEST71-TO", "TEST71-PRT", "TEST71-RET", "TEST71-CN"
		cust                 = "TEST71-CUST"
	)
	ids := []string{sku, vendor, po, grn, to, prt, ret, cn, cust}
	cleanup := func() {
		for _, id := range ids {
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
		db.DB.Exec("DELETE FROM "+schema+".approval_log WHERE document_id = $1", po)
	}
	cleanup()
	defer cleanup()
	insert := func(id, doctype, status string, data map[string]interface{}) {
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	js := func(v interface{}) string { b, _ := json.Marshal(v); return string(b) }

	insert(sku, "Item", "Active", map[string]interface{}{"code": sku, "name": "Test71 Ring", "hsn_code": "7113", "gst_rate": 18.0,
		"mrp": 150.0, "standard_cost": 90.0, "barcode": "8900000071001", "variant_option_values": "metal_type:Silver;size:7"})
	insert(vendor, "Vendor", "Active", map[string]interface{}{"code": vendor, "name": "Test71 Vendor"})
	insert(cust, "Customer", "Active", map[string]interface{}{"code": cust, "name": "Test71 Customer", "phone": "9000000071"})
	insert(po, "PurchaseOrder", "Approved", map[string]interface{}{"code": po, "vendor_id": vendor, "order_date": "2026-03-20",
		"items": js([]map[string]interface{}{{"sku": sku, "qty": 12, "rate": 100}})})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".approval_log (doctype, document_id, action, actor_user_id, actor_role, amount, comment) VALUES ('PurchaseOrder', $1, 'Approved', 'checker71', 'HR/Admin', 1200, '')", po); err != nil {
		t.Fatalf("approval log: %v", err)
	}
	insert(grn, "GRN", "Posted", map[string]interface{}{"code": grn, "po_id": po, "vendor": vendor, "posting_date": "2026-04-02",
		"vendor_invoice_no": "INV-71", "vendor_invoice_amount": 1000.0, "invoice_value": 1000.0,
		"received_items": js([]map[string]interface{}{{"po_id": po, "sku": sku, "qty": 10, "accepted_qty": 8, "rejected_qty": 2,
			"rejection_reason": "Scratched", "po_rate": 100, "invoice_rate": 100, "gst_rate": 18}})})
	insert(to, "TransferOrder", "Received", map[string]interface{}{"code": to, "from_warehouse": "TEST71-A", "to_warehouse": "TEST71-B",
		"items": js([]map[string]interface{}{{"sku": sku, "qty": 5}}), "dispatched_items": js([]map[string]interface{}{{"sku": sku, "qty": 5}}),
		"received_items": js([]map[string]interface{}{{"sku": sku, "qty": 3, "reason": "Two missing in carton"}})})
	insert(prt, "PurchaseReturn", "Posted", map[string]interface{}{"code": prt, "grn_id": grn, "vendor_id": vendor, "po_id": po,
		"reason": "Rejected at QC", "debit_note_id": "TEST71-DN",
		"return_items": js([]map[string]interface{}{{"sku": sku, "stock_bucket": "Rejected", "qty": 2, "unit_cost": 100, "line_value": 200}})})
	insert(ret, "ReturnRequest", "Received", map[string]interface{}{"code": ret, "request_type": "Customer Return", "return_location": "TEST71-B",
		"items": js([]map[string]interface{}{{"sku": sku, "qty": 1, "original_unit_price": 118, "disposition": "Sellable"}})})
	insert(cn, "CreditNote", "Draft", map[string]interface{}{"note_number": cn, "customer_id": cust, "amount": 500.0, "reason": "Goodwill"})

	t.Run("GRN: ordered qty, QC status, reason, audit, financial year", func(t *testing.T) {
		d, err := BuildPrintDocument(tenantID, PrintTxnGRN, grn, "", false)
		if err != nil {
			t.Fatalf("BuildPrintDocument: %v", err)
		}
		l := d.Lines[0]
		if l["po_qty"] != 12.0 || l["qc_status"] != "Partly accepted" || l["variance_reason"] != "Scratched" || l["variant"] != "Silver / 7" {
			t.Fatalf("GRN line %v", l)
		}
		if l["description"] != "Test71 Ring · Silver / 7" || l["item_code"] != sku+" / 8900000071001" {
			t.Fatalf("description / item code %q %q", l["description"], l["item_code"])
		}
		if d.Totals["po_qty"] != 12 || d.Totals["rejected_qty"] != 2 || d.Fields["financial_year"] != "2026-27" || d.Fields["currency"] != "INR" {
			t.Fatalf("totals / header %v %v", d.Totals, d.Fields)
		}
		if d.Audit["created_by"] != "system" || d.Audit["created_at"] == "" {
			t.Fatalf("audit %v", d.Audit)
		}
		p, err := BuildPrintDocument(tenantID, PrintTxnPurchaseOrder, po, "", false)
		if err != nil || p.Audit["approved_by"] != "checker71" || p.Fields["financial_year"] != "2025-26" {
			t.Fatalf("PO audit / FY (March is the previous year) %v %v %v", err, p.Audit, p.Fields["financial_year"])
		}
	})

	t.Run("vendor copy carries no value", func(t *testing.T) {
		d, err := BuildPrintDocument(tenantID, PrintTxnGRNVendorCopy, grn, "", false)
		if err != nil {
			t.Fatalf("BuildPrintDocument: %v", err)
		}
		l := d.Lines[0]
		for _, k := range []string{"rate", "amount", "taxable", "tax", "po_rate", "invoice_rate", "mrp"} {
			if _, ok := l[k]; ok {
				t.Errorf("vendor copy line still has %s", k)
			}
		}
		if item := l["item"].(map[string]interface{}); item["standard_cost"] != nil || item["mrp"] != nil {
			t.Errorf("vendor copy item still has cost: %v", item)
		}
		if _, ok := d.Fields["vendor_invoice_amount"]; ok || d.Totals["grand_total"] != 0 || len(d.TaxSummary) != 0 || d.AmountInWords != "" {
			t.Errorf("vendor copy header / totals carry value: %v %v %d %q", d.Fields, d.Totals, len(d.TaxSummary), d.AmountInWords)
		}
		if l["accepted_qty"] != 8.0 || l["po_qty"] != 12.0 || d.Fields["vendor_invoice_no"] != "INV-71" || d.Number != grn {
			t.Errorf("vendor copy lost its quantities / references: %v %v", l, d.Fields)
		}
	})

	t.Run("transfer receipt prints against what was shipped", func(t *testing.T) {
		out, err := BuildPrintDocument(tenantID, PrintTxnTransferOut, to, "", false)
		if err != nil || out.Lines[0]["shipped_qty"] != 5.0 || out.Lines[0]["requested_qty"] != 5.0 || out.Fields["stock_status_after"] != "In transit" {
			t.Fatalf("transfer out %v %v", err, out.Lines)
		}
		in, err := BuildPrintDocument(tenantID, PrintTxnTransferIn, to, "", false)
		if err != nil {
			t.Fatalf("BuildPrintDocument: %v", err)
		}
		l := in.Lines[0]
		if l["shipped_qty"] != 5.0 || l["received_qty"] != 3.0 || l["short_qty"] != 2.0 || l["reason"] != "Two missing in carton" || l["sent_qty"] != 5.0 {
			t.Fatalf("transfer in line %v", l)
		}
		if in.Fields["receipt_status"] != "Received with shortage" || in.Totals["short_qty"] != 2 {
			t.Fatalf("transfer in header %v %v", in.Fields, in.Totals)
		}
	})

	t.Run("purchase return, sales return and customer credit note print", func(t *testing.T) {
		r, err := BuildPrintDocument(tenantID, PrintTxnPurchaseReturn, prt, "", false)
		if err != nil {
			t.Fatalf("purchase return: %v", err)
		}
		if r.Party.Name != "Test71 Vendor" || r.Lines[0]["return_qty"] != 2.0 || r.Lines[0]["received_qty"] != 10.0 || r.Totals["taxable"] != 200 || r.Totals["tax"] != 36 || r.Fields["vendor_invoice_no"] != "INV-71" {
			t.Fatalf("purchase return %v %v %v %v", r.Party, r.Lines, r.Totals, r.Fields)
		}
		s, err := BuildPrintDocument(tenantID, PrintTxnSalesReturn, ret, "", false)
		if err != nil {
			t.Fatalf("sales return: %v", err)
		}
		if s.Totals["taxable"] != 100 || s.Totals["tax"] != 18 || s.Totals["grand_total"] != 118 || s.Lines[0]["condition"] != "Sellable" || s.Lines[0]["original_disc"] != 32.0 {
			t.Fatalf("sales return %v %v", s.Totals, s.Lines)
		}
		c, err := BuildPrintDocument(tenantID, PrintTxnCreditNote, cn, "", false)
		if err != nil || c.Party.Name != "Test71 Customer" || c.Totals["grand_total"] != 500 || c.Lines[0]["description"] != "Goodwill" {
			t.Fatalf("credit note %v %v %v %v", err, c.Party, c.Totals, c.Lines)
		}
	})
}
