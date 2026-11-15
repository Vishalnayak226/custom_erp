package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// TestGRNStepsRejectedUnitStaysOutOfStock (FA-20261005-05): a receipt of 10
// with 1 rejected unit posts 9 into stock, and the posted view reports the
// same 9 accepted and 1 rejected that the screen's posting message states.
func TestGRNStepsRejectedUnitStaysOutOfStock(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	if !grnLineTablesReady(db.DB, schema) {
		t.Skip("needs migrations_stage94_grn_steps.sql")
	}
	const (
		sku, vend, po, grnID, location = "TESTA5-ITEM", "TESTA5-VEND", "TESTA5-PO", "TESTA5-GRN", "TESTA5-LOC"
	)
	preserveSeries(t, schema, "DN", "VCN", "GRNBatchSeq", "VendorInvoice", "GRN")
	cleanup := func() {
		db.DB.Exec("DELETE FROM "+schema+".gl_postings WHERE document_id = $1", grnID)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'StockLedgerEntry' AND (data->>'voucher_id' = $1 OR data->>'voucher_id' LIKE $1 || '-C%')", grnID)
		db.DB.Exec("DELETE FROM "+schema+".grn_line WHERE grn_id = $1", grnID)
		db.DB.Exec("DELETE FROM "+schema+".grn_line_change WHERE grn_id = $1", grnID)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE id IN ($1,$2,$3,$4)", sku, vend, po, grnID)
		db.DB.Exec("DELETE FROM "+schema+".item_cost WHERE item_code = $1", sku)
		db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype IN ('Batch','StockLedgerEntry') AND (data->>'item' = $1 OR data->>'item_id' = $1)", sku)
	}
	cleanup()
	defer cleanup()
	insert := func(id, doctype, status string, data map[string]interface{}) {
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	lines := func(rows ...map[string]interface{}) string {
		raw, _ := json.Marshal(rows)
		return string(raw)
	}
	insert(sku, "Item", "Active", map[string]interface{}{"code": sku, "name": "A5 Kurta", "hsn_code": "6204", "gst_rate": 5.0, "mrp": 199.0})
	insert(vend, "Vendor", "Active", map[string]interface{}{"code": vend, "name": "A5 Vendor"})
	insert(po, "PurchaseOrder", "Approved", map[string]interface{}{"code": po, "vendor": vend, "gst_mode": GSTModeExclusive, "total_amount": 1000, "grand_total": 1050,
		"items": lines(map[string]interface{}{"sku": sku, "qty": 10, "rate": 100.0})})
	payload := map[string]interface{}{
		"flow": "steps", "po_ids": po, "location": location,
		"vendor_invoice_no": "INV-A5-1", "vendor_invoice_date": "2026-11-15", "vendor_invoice_amount": 1050,
		"received_items": lines(map[string]interface{}{"sku": sku, "qty": 10, "invoice_rate": 100}),
	}
	if err := PrepareGRNPricing(tenantID, true, payload); err != nil {
		t.Fatalf("pricing: %v", err)
	}
	if err := PrepareGRNSteps(tenantID, "system", true, payload); err != nil {
		t.Fatalf("steps: %v", err)
	}
	if err := PrepareGRNBatchNumbers(tenantID, true, payload); err != nil {
		t.Fatalf("batch: %v", err)
	}
	payload["code"] = grnID
	insert(grnID, "GRN", strField(payload, "status"), payload)
	SyncGRNLineRows(tenantID, grnID)

	one, why := 1.0, "Torn in transit"
	if _, err := VerifyGRNStep(tenantID, "system", "Super Admin", grnID, GRNVerifyInput{Lines: []GRNVerifyLine{{LineNo: 1, RejectedQty: &one, RejectionReason: &why}}, Reason: "one torn"}); err != nil {
		t.Fatalf("verify: %v", err)
	}
	v, err := PriceCheckGRNStep(tenantID, "system", grnID, GRNPriceCheckInput{Regroup: true})
	if err != nil || len(v.Children) == 0 {
		t.Fatalf("view: %v children %d", err, len(v.Children))
	}
	if v, err = PostGRNChildStep(tenantID, "system", "", grnID, v.Children[0].Key, GRNPostInput{}); err != nil {
		t.Fatalf("post: %v", err)
	}
	var acc, rej float64
	for _, l := range v.Lines {
		acc += l.AcceptedQty
		rej += l.RejectedQty + l.DamagedQty
	}
	if acc != 9 || rej != 1 {
		t.Fatalf("posted view says %v accepted, %v rejected; want 9 and 1", acc, rej)
	}
	// The ledger keeps the rejected unit apart: 9 sellable, 1 set aside.
	var onHand, available, setAside float64
	db.DB.QueryRow("SELECT COALESCE(SUM(on_hand),0)::float8, COALESCE(SUM(available),0)::float8, COALESCE(SUM(qc_hold+damaged),0)::float8 FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = $2", sku, location).Scan(&onHand, &available, &setAside)
	if available != 9 || setAside != 1 || onHand != 10 {
		t.Fatalf("on hand %v, available %v, set aside %v; want 10, 9, 1 (the rejected unit must not become sellable)", onHand, available, setAside)
	}
}
