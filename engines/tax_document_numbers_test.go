package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
)

// Stage 76.9: tax documents take their number from their store's series - a
// note made on the record form is issued its number (not typed), and one an
// engine creates keeps its internal id and takes the number beside it; a
// switched-off series leaves the internal number rather than stopping a
// cascade.
func TestStage769TaxDocumentNumbers(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const store = "T769S"
	cleanup := func() {
		db.DB.Exec("DELETE FROM "+schema+".sequence_counters WHERE store_code = $1", store)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'Location' AND id = $1", store)
	}
	cleanup()
	defer cleanup()
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Location', $2, 'Active', 'admin')",
		store, `{"code":"T769S","name":"Stage 76.9 test store"}`); err != nil {
		t.Fatalf("scratch store: %v", err)
	}

	note := map[string]interface{}{"customer_id": "C1", "amount": 10}
	if err := PrepareDocumentNumber(tenantID, store, "CreditNote", true, note); err != nil {
		t.Fatalf("credit note: %v", err)
	}
	id, _ := note["id"].(string)
	if !strings.HasPrefix(id, "CN/"+store+"/") || note["note_number"] != id {
		t.Fatalf("a credit note on the record form: want a CN/%s/ number in id and note_number, got id %q note_number %v", store, id, note["note_number"])
	}

	inv := IssueDocumentNumber(tenantID, "SalesInvoice", "INV-INTERNAL-1", "HQ", store)
	if !strings.HasPrefix(inv, "SI/"+store+"/") {
		t.Fatalf("an engine-made invoice at %s: want an SI/%s/ number, got %q", store, store, inv)
	}

	if got := IssueDocumentNumber(tenantID, "Item", "KEEP-ME", "HQ"); got != "KEEP-ME" {
		t.Fatalf("a doctype with no series keeps its own number, got %q", got)
	}

	if _, err := db.DB.Exec("UPDATE " + schema + ".prefix_configs SET active_status = FALSE WHERE doc_type = 'CN'"); err != nil {
		t.Fatalf("switch CN off: %v", err)
	}
	defer db.DB.Exec("UPDATE " + schema + ".prefix_configs SET active_status = TRUE WHERE doc_type = 'CN'")
	if got := IssueDocumentNumber(tenantID, "CreditNote", "CRN-INTERNAL-1", "HQ", store); got != "CRN-INTERNAL-1" {
		t.Fatalf("a switched-off series must leave the internal number, got %q", got)
	}
	off := map[string]interface{}{"customer_id": "C1", "amount": 10}
	if err := PrepareDocumentNumber(tenantID, store, "CreditNote", true, off); err == nil {
		t.Fatal("the record form must refuse a credit note while its series is switched off")
	}
}
