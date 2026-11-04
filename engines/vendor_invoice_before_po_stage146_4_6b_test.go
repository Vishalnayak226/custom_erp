package engines

import (
	"custom_erp/db"
	"testing"
)

// Stage 146.4.6(b): a vendor invoice dated before its PO warns on save and is
// listed in a report.
func TestStage14646bVendorInvoiceDatedBeforePO(t *testing.T) {
	tenantID, schema, cleanup, insert := gateEntryTestTenant(t)
	defer cleanup()
	insert("PO-VB1", "PurchaseOrder", "Approved", map[string]interface{}{"code": "PO-VB1", "vendor": "V-1"})
	if _, err := db.DB.Exec("UPDATE " + schema + ".documents SET created_at = '2026-01-10 10:00:00' WHERE id = 'PO-VB1'"); err != nil {
		t.Fatal(err)
	}
	if msg := VendorInvoiceBeforePO(tenantID, map[string]interface{}{"po_id": "PO-VB1", "invoice_date": "2026-01-05"}); msg == "" {
		t.Errorf("an invoice dated 5 Jan for a PO of 10 Jan must warn")
	}
	if msg := VendorInvoiceBeforePO(tenantID, map[string]interface{}{"po_id": "PO-VB1", "invoice_date": "2026-01-10"}); msg != "" {
		t.Errorf("same day is fine: %s", msg)
	}
	if msg := VendorInvoiceBeforePO(tenantID, map[string]interface{}{"po_id": "PO-VB1"}); msg != "" {
		t.Errorf("no invoice date: nothing to compare: %s", msg)
	}
	if msg := VendorInvoiceBeforePO(tenantID, map[string]interface{}{"po_id": "PO-NONE", "invoice_date": "2026-01-05"}); msg != "" {
		t.Errorf("unknown PO: %s", msg)
	}
	insert("VI-VB1", "VendorInvoice", "Draft", map[string]interface{}{"code": "VI-VB1", "invoice_number": "INV-1", "vendor_id": "V-1", "po_id": "PO-VB1", "invoice_date": "2026-01-05"})
	insert("VI-VB2", "VendorInvoice", "Draft", map[string]interface{}{"code": "VI-VB2", "invoice_number": "INV-2", "vendor_id": "V-1", "po_id": "PO-VB1", "invoice_date": "2026-01-12"})
	def, ok := reportRegistry["vendor-invoices-before-po"]
	if !ok {
		t.Fatal("report vendor-invoices-before-po is not registered")
	}
	rows, err := def.Run(tenantID, nil)
	if err != nil || len(rows) != 1 || rows[0]["invoice"] != "INV-1" || rows[0]["po"] != "PO-VB1" {
		t.Fatalf("report: %v %v", err, rows)
	}
}
