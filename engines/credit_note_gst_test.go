package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// Stage 73.D.10: a customer credit note against a sales invoice gives back the
// invoice's output GST in proportion, capped at what is left, so the GST
// accounts net to zero once the whole invoice is credited.
func TestStage73D10CreditNoteReversesOutputGST(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	ids := []string{"T73D10-INV", "T73D10-CN1", "T73D10-CN2", "T73D10-INV0", "T73D10-CN0"}
	cleanup := func() {
		for _, id := range ids {
			db.DB.Exec("DELETE FROM "+schema+".gl_postings WHERE document_id = $1", id)
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
	}
	cleanup()
	defer cleanup()
	insert := func(id, doctype string, data map[string]interface{}) {
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Draft', 'admin')", id, doctype, raw); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	gl := func(id, account string) (debit, credit int64) {
		db.DB.QueryRow("SELECT COALESCE(SUM(debit),0), COALESCE(SUM(credit),0) FROM "+schema+".gl_postings WHERE document_id = $1 AND account_code = $2", id, account).Scan(&debit, &credit)
		return
	}

	insert("T73D10-INV", "SalesInvoice", map[string]interface{}{"invoice_number": "T73D10-INV", "total_amount": 1180, "taxable_amount": 1000, "cgst": 90, "sgst": 90, "location": "HO"})
	if _, err := PostSalesInvoice(tenantID, "T73D10-INV", "admin"); err != nil {
		t.Fatalf("post invoice: %v", err)
	}
	if _, c := gl("T73D10-INV", "2200"); c != 9000 {
		t.Fatalf("fixture: invoice CGST credit = %d paise, want 9000", c)
	}

	insert("T73D10-CN1", "CreditNote", map[string]interface{}{"note_number": "T73D10-CN1", "amount": 590, "reason": "half back", "sales_invoice_id": "T73D10-INV"})
	if _, err := PostCreditNote(tenantID, "T73D10-CN1", "admin"); err != nil {
		t.Fatalf("post note 1: %v", err)
	}
	for account, want := range map[string]int64{"2200": 4500, "2201": 4500, "4150": 50000} {
		if d, _ := gl("T73D10-CN1", account); d != want {
			t.Errorf("note 1 debit %s = %d paise, want %d", account, d, want)
		}
	}
	var note map[string]interface{}
	var raw string
	db.DB.QueryRow("SELECT data FROM " + schema + ".documents WHERE id = 'T73D10-CN1'").Scan(&raw)
	json.Unmarshal([]byte(raw), &note)
	if note["cgst"] != 45.0 || note["taxable_amount"] != 500.0 {
		t.Errorf("note 1 split stamped as cgst=%v taxable=%v, want 45 and 500", note["cgst"], note["taxable_amount"])
	}

	insert("T73D10-CN2", "CreditNote", map[string]interface{}{"note_number": "T73D10-CN2", "amount": 800, "reason": "the rest and more", "sales_invoice_id": "T73D10-INV"})
	if _, err := PostCreditNote(tenantID, "T73D10-CN2", "admin"); err != nil {
		t.Fatalf("post note 2: %v", err)
	}
	for account, want := range map[string]int64{"2200": 4500, "2201": 4500, "4150": 71000} {
		if d, _ := gl("T73D10-CN2", account); d != want {
			t.Errorf("note 2 debit %s = %d paise, want %d (capped at what was left)", account, d, want)
		}
	}
	var net int64
	db.DB.QueryRow("SELECT COALESCE(SUM(credit) - SUM(debit), 0) FROM " + schema + ".gl_postings WHERE document_id IN ('T73D10-INV','T73D10-CN1','T73D10-CN2') AND account_code IN ('2200','2201','2202')").Scan(&net)
	if net != 0 {
		t.Errorf("output GST still payable after crediting the whole invoice: %d paise", net)
	}

	insert("T73D10-INV0", "SalesInvoice", map[string]interface{}{"invoice_number": "T73D10-INV0", "total_amount": 500, "location": "HO"})
	if _, err := PostSalesInvoice(tenantID, "T73D10-INV0", "admin"); err != nil {
		t.Fatalf("post untaxed invoice: %v", err)
	}
	insert("T73D10-CN0", "CreditNote", map[string]interface{}{"note_number": "T73D10-CN0", "amount": 100, "reason": "no gst", "sales_invoice_id": "T73D10-INV0"})
	if _, err := PostCreditNote(tenantID, "T73D10-CN0", "admin"); err != nil {
		t.Fatalf("post note 0: %v", err)
	}
	if d, _ := gl("T73D10-CN0", "4150"); d != 10000 {
		t.Errorf("a note on an invoice with no output GST: 4150 debit = %d, want 10000", d)
	}
}
