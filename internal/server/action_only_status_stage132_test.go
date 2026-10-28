package server

import (
	"bytes"
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Stage 132.17: a status that stands for stock or money already moved is set
// only by the document's own action. Guide saved a transfer straight as
// Dispatched and received it - stock at the destination, nothing taken from
// the source - and did the same with an expense claim (Verified, then paid),
// a vendor invoice (Matched, then paid) and a production order (Material
// Issued, then completed with no materials used).
func TestActionOnlyStatusRefusedOnPlainSave(t *testing.T) {
	db.InitDB(testConnStr())
	const id = "T13217-TO"
	cleanup := func() { _, _ = db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id LIKE 'T13217-%'`) }
	cleanup()
	defer cleanup()
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by)
		VALUES ('T13217-STORE', 'Location', '{"code":"T13217-STORE","name":"T13217 store","type":"Store","status":"Active"}', 'Active', 'admin')`); err != nil {
		t.Fatalf("seed location: %v", err)
	}

	token := engines.SignToken("admin", "admin", "HR/Admin", "default", "HO", currentCredentialVersion("admin"))
	engines.ResetLiveUserStateCache()
	save := func(status string) (int, string) {
		t.Helper()
		payload, _ := json.Marshal(map[string]interface{}{
			"id": id, "transfer_number": id, "from_warehouse": "HO", "to_warehouse": "T13217-STORE",
			"items": `[{"sku":"T13217-SKU","qty":2}]`, "status": status,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/doc/TransferOrder", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.SetPathValue("doctype", "TransferOrder")
		rec := httptest.NewRecorder()
		apiMiddleware(handleGenericDoc)(rec, req)
		var body map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		code, _ := body["code"].(string)
		if code != "" && code != "GLOBAL-0019" {
			t.Logf("save as %s: %s", status, rec.Body.String())
		}
		return rec.Code, code
	}
	stored := func() string {
		var s string
		_ = db.DB.QueryRow(`SELECT status FROM tenant_default.documents WHERE id = $1`, id).Scan(&s)
		return s
	}

	// A create claiming Dispatched is refused, and nothing is stored.
	if status, code := save("Dispatched"); status != http.StatusUnprocessableEntity || code != "GLOBAL-0019" {
		t.Fatalf("create as Dispatched: %d %s, want 422 GLOBAL-0019", status, code)
	}
	if s := stored(); s != "" {
		t.Fatalf("a refused create was stored as %q", s)
	}

	// Approved -> Dispatched is a legal step in the transition map, but only
	// /transfer/dispatch may take it (it takes the stock out of the source).
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'TransferOrder', '{"transfer_number":"T13217-TO","from_warehouse":"HO","to_warehouse":"T13217-STORE","items":"[]","status":"Approved"}', 'Approved', 'admin')`, id); err != nil {
		t.Fatalf("seed transfer: %v", err)
	}
	if status, code := save("Dispatched"); status != http.StatusUnprocessableEntity || code != "GLOBAL-0019" {
		t.Fatalf("Approved -> Dispatched by a save: %d %s, want 422 GLOBAL-0019", status, code)
	}
	if s := stored(); s != "Approved" {
		t.Fatalf("status after a refused save = %q, want Approved", s)
	}

	// A document the action already moved can still be re-saved with its
	// status unchanged (an edit sends the whole record back).
	if _, err := db.DB.Exec(`UPDATE tenant_default.documents SET status = 'Dispatched', data = jsonb_set(data, '{status}', '"Dispatched"') WHERE id = $1`, id); err != nil {
		t.Fatalf("move to Dispatched: %v", err)
	}
	if _, code := save("Dispatched"); code == "GLOBAL-0019" {
		t.Fatalf("re-saving a Dispatched transfer with its status unchanged was refused")
	}

	// The other three Guide proved are on the list, and the form hides them;
	// so is a price list's Superseded (135.12), which only an approval sets.
	for doctype, status := range map[string]string{"ExpenseClaim": "Verified", "VendorInvoice": "Matched", "ProductionOrder": "Material Issued", "PriceListVersion": "Superseded"} {
		if !isActionOnlyStatus(doctype, status) {
			t.Errorf("%s %s is not action-only", doctype, status)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/doc/TransferOrder/meta", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.SetPathValue("doctype", "TransferOrder")
	rec := httptest.NewRecorder()
	apiMiddleware(handleGetDocTypeMeta)(rec, req)
	var fields []engines.FieldMeta
	if err := json.Unmarshal(rec.Body.Bytes(), &fields); err != nil {
		t.Fatalf("meta: %v %s", err, rec.Body.String())
	}
	hidden := false
	for _, f := range fields {
		if f.Fieldname == "status" {
			for _, s := range f.ApprovalOnly {
				hidden = hidden || s == "Dispatched"
			}
		}
	}
	if !hidden {
		t.Errorf("the TransferOrder form still offers Dispatched")
	}
}
