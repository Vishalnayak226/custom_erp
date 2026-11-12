package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// Stage 73.D.8: a record-form save of a posted invoice keeps the keys the
// server keeps on it (GST split, settlement history, outstanding) and refuses
// a change to its money.
func TestStage73D8GenericSaveKeepsServerKeysAndFreezesPostedMoney(t *testing.T) {
	db.InitDB(testConnStr())
	const id = "T73D8-SINV"
	db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, id)
	defer db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, id)
	stored := map[string]interface{}{
		"invoice_number": id, "customer": "T73D8 Customer", "location": "HO", "total_amount": 1180, "taxable_amount": 1000,
		"cgst": 90, "sgst": 90, "outstanding_amount": 680, "settled_amount": 500,
		"settlements": []interface{}{map[string]interface{}{"reference": "R1", "amount": 500}},
		"due_date":    "2031-01-31",
	}
	raw, _ := json.Marshal(stored)
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, 'SalesInvoice', $2, 'Approved', 'admin')`, id, raw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/doc/{doctype}", apiMiddleware(handleGenericDoc))
	mux.HandleFunc("/api/v1/doc/{doctype}/{id}", apiMiddleware(handleGenericDoc))
	token := engines.SignToken("admin", "admin", engines.RoleSuperAdmin, "default", "HO", currentCredentialVersion("admin"))

	// The form sends what it shows: no settlements, no outstanding, no GST split.
	edit := map[string]interface{}{"invoice_number": id, "customer": "T73D8 Customer", "location": "HO", "total_amount": 1180,
		"due_date": "2031-02-28", "status": "Approved", "edit_reason": "due date agreed later"}
	if rec := doRequest(t, mux.ServeHTTP, http.MethodPost, "/api/v1/doc/SalesInvoice/"+id, token, edit); rec.Code != http.StatusOK {
		t.Fatalf("editing the due date: %d %s", rec.Code, rec.Body.String())
	}
	var after map[string]interface{}
	var afterRaw string
	db.DB.QueryRow(`SELECT data FROM tenant_default.documents WHERE id = $1`, id).Scan(&afterRaw)
	json.Unmarshal([]byte(afterRaw), &after)
	if after["due_date"] != "2031-02-28" {
		t.Errorf("the edit itself did not save: due_date = %v", after["due_date"])
	}
	for _, k := range []string{"settlements", "outstanding_amount", "cgst", "sgst", "taxable_amount"} {
		if after[k] == nil {
			t.Errorf("the save wiped the server-kept key %q", k)
		}
	}

	edit["total_amount"] = 999
	rec := doRequest(t, mux.ServeHTTP, http.MethodPost, "/api/v1/doc/SalesInvoice/"+id, token, edit)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "FIN-0844") {
		t.Errorf("changing a posted invoice's total: %d %s, want FIN-0844", rec.Code, rec.Body.String())
	}
}

// Stage 104: the PO screen's edit save (poDraftPayload, view-procurement.js)
// sends the vendor, store, lines and GST settings - never the server-issued
// code / po_number. Before the merge above, the save replaced the record and
// was refused "PO Number (Internal) is required" (six times on the default
// client's production log, 2026-10-17).
func TestStage104POEditWithoutNumberFieldsSaves(t *testing.T) {
	db.InitDB(testConnStr())
	const poID, vendorID = "PO-T104-EDIT-1", "T104-VENDOR"
	clean := func() {
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id IN ($1, $2)`, poID, vendorID)
	}
	clean()
	t.Cleanup(clean)
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES
		($1, 'Vendor', '{"code":"T104-VENDOR","name":"T104 Vendor","state":"Maharashtra"}', 'Active', 'admin'),
		($2, 'PurchaseOrder', '{"code":"PO-T104-EDIT-1","po_number":"PO-T104-EDIT-1","vendor":"T104-VENDOR","vendor_id":"T104-VENDOR","location":"HO","target_warehouse":"HO","items":"[]","total_amount":0}', 'Draft', 'admin')`, vendorID, poID); err != nil {
		t.Fatalf("seed: %v", err)
	}
	storeHasState(t, "HO") // 49.5.6e: a PO's store needs a state
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/doc/{doctype}/{id}", apiMiddleware(handleGenericDoc))
	token := engines.SignToken("admin", "admin", engines.RoleSuperAdmin, "default", "HO", currentCredentialVersion("admin"))

	edit := map[string]interface{}{"vendor": vendorID, "vendor_id": vendorID, "target_warehouse": "HO", "location": "HO",
		"items": "[]", "gst_mode": engines.GSTModeExclusive, "interstate": false, "total_amount": 0, "status": "Draft"}
	if rec := doRequest(t, mux.ServeHTTP, http.MethodPost, "/api/v1/doc/PurchaseOrder/"+poID, token, edit); rec.Code != http.StatusOK {
		t.Fatalf("editing a PO from the PO screen: %d %s", rec.Code, rec.Body.String())
	}
	var afterRaw string
	db.DB.QueryRow(`SELECT data FROM tenant_default.documents WHERE id = $1`, poID).Scan(&afterRaw)
	var after map[string]interface{}
	json.Unmarshal([]byte(afterRaw), &after)
	if after["code"] != poID || after["po_number"] != poID {
		t.Errorf("the edit lost the PO's number: code=%v po_number=%v", after["code"], after["po_number"])
	}
}
