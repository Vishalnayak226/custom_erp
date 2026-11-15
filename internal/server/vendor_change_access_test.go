package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Stage 148.10: a VendorChange writes to the vendor when approved, and holds
// the bank numbers the sensitive-field policy keeps to Super Admin. So only the
// save path may file one, and everyone else sees the numbers masked.
func TestStage14810RequestIsNeitherForgedNorRead(t *testing.T) {
	db.InitDB(testConnStr())
	const (
		tenantID = "default"
		vendorID = "T14810-ACCESS"
		oldBank  = "111122223333"
		newBank  = "999900004444"
		forged   = "555566667777"
	)
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func() {
		_, _ = db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE doctype = 'Vendor' AND id = $1`, vendorID)
		_, _ = db.DB.Exec(`DELETE FROM `+schema+`.approval_log WHERE document_id IN (SELECT id FROM `+schema+`.documents WHERE doctype = 'VendorChange' AND data->>'vendor_id' = $1)`, vendorID)
		_, _ = db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE doctype = 'VendorChange' AND data->>'vendor_id' = $1`, vendorID)
	}
	cleanup()
	t.Cleanup(cleanup)
	vendor := map[string]interface{}{"code": vendorID, "name": "Access Vendor", "state": "Karnataka", "bank_account_number": oldBank, "bank_ifsc": "HDFC0000001"}
	raw, _ := json.Marshal(vendor)
	if _, err := db.DB.Exec(`INSERT INTO `+schema+`.documents (id, doctype, data, status, created_by) VALUES ($1, 'Vendor', $2, 'Active', 'admin')`, vendorID, raw); err != nil {
		t.Fatal(err)
	}
	bank := func() string {
		var s string
		_ = db.DB.QueryRow(`SELECT data->>'bank_account_number' FROM `+schema+`.documents WHERE doctype = 'Vendor' AND id = $1`, vendorID).Scan(&s)
		return s
	}
	call := func(h http.HandlerFunc, method, role, user, doctype, id string, body interface{}) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		r := httptest.NewRequest(method, "/api/v1/doc/"+doctype, strings.NewReader(string(b)))
		r.SetPathValue("doctype", doctype)
		if id != "" {
			r.SetPathValue("id", id)
		}
		r.Header.Set("Resolved-Tenant-ID", tenantID)
		r.Header.Set("Resolved-Role", role)
		r.Header.Set("Resolved-User-ID", user)
		r.Header.Set("Resolved-Username", user)
		r.Header.Set("Resolved-Location", "HO")
		w := httptest.NewRecorder()
		h(w, r)
		return w
	}

	// An Accountant has no rights on Vendor; a request filed by hand is not a way in.
	forgedDoc := map[string]interface{}{"vendor_id": vendorID, "fields": "bank_account_number",
		"new_values": `{"bank_account_number":"` + forged + `"}`, "status": "Draft"}
	if w := call(handleGenericDoc, http.MethodPost, "Accountant", "acct14810", "VendorChange", "", forgedDoc); w.Code != http.StatusForbidden {
		t.Fatalf("an Accountant filed a VendorChange by hand: %d %s", w.Code, w.Body.String())
	}
	if bank() != oldBank {
		t.Fatalf("the vendor's bank moved: %s", bank())
	}

	// The real path: Super Admin edits the vendor, the save says it is held.
	edit := map[string]interface{}{}
	for k, v := range vendor {
		edit[k] = v
	}
	edit["bank_account_number"] = newBank
	edit["id"] = vendorID
	// Through /{id}, as every screen edits: a create of a live code is refused (57.26(j)).
	w := call(handleGenericDoc, http.MethodPost, engines.RoleSuperAdmin, "admin", "Vendor", vendorID, edit)
	if w.Code != http.StatusOK {
		t.Fatalf("vendor save: %d %s", w.Code, w.Body.String())
	}
	var saved map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &saved)
	if saved["held_fields"] != "bank_account_number" {
		t.Fatalf("the save did not say the field is held: %s", w.Body.String())
	}
	if bank() != oldBank {
		t.Fatalf("a listed change went live before approval: %s", bank())
	}
	var changeID string
	if err := db.DB.QueryRow(`SELECT id FROM `+schema+`.documents WHERE doctype = 'VendorChange' AND data->>'vendor_id' = $1`, vendorID).Scan(&changeID); err != nil {
		t.Fatal(err)
	}

	// The approver reads what changes, masked; the numbers go nowhere else.
	for _, role := range []string{"Accountant", engines.RoleCashier, engines.RoleStoreManager} {
		w := call(handleListPendingApprovals, http.MethodGet, role, "reader14810", "", "", nil)
		if body := w.Body.String(); strings.Contains(body, newBank) || strings.Contains(body, oldBank) {
			t.Fatalf("%s read a raw bank number in the pending list", role)
		}
	}
	w = call(handleListPendingApprovals, http.MethodGet, "Accountant", "acct14810", "", "", nil)
	var pending []map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &pending)
	summary := ""
	for _, p := range pending {
		if p["id"] == changeID {
			summary, _ = p["summary"].(string)
		}
	}
	if !strings.Contains(summary, "Access Vendor") || !strings.Contains(summary, "bank account number") || !strings.Contains(summary, "****3333") || !strings.Contains(summary, "****4444") {
		t.Fatalf("the approver cannot see what changes: %q", summary)
	}
	w = call(handleGenericDoc, http.MethodGet, "Accountant", "acct14810", "VendorChange", changeID, nil)
	if body := w.Body.String(); w.Code != http.StatusOK || strings.Contains(body, newBank) || strings.Contains(body, oldBank) {
		t.Fatalf("an Accountant read the raw request: %d %s", w.Code, body)
	}

	// Read-only is enough to decide: the approval still lands.
	if err := engines.DecideApproval(tenantID, "VendorChange", changeID, "acct14810", "Accountant", "HO", "Approved", "ok"); err != nil {
		t.Fatal(err)
	}
	if bank() != newBank {
		t.Fatalf("approved change not applied: %s", bank())
	}
}
