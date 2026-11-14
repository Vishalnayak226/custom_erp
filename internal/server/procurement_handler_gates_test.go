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

// 148.14c: converting a requisition needs create on what it makes.
func TestConvertRequisitionNeedsCreateOnTarget(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	const role = "TEST-ROLE-14814C"
	cleanup := func() { _, _ = db.DB.Exec(`DELETE FROM `+schema+`.role_permissions WHERE role=$1`, role) }
	cleanup()
	t.Cleanup(cleanup)
	call := func(target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/procurement/convert-requisition",
			strings.NewReader(`{"requisition_id":"TEST-NO-SUCH-REQ","target":"`+target+`","store_code":"X","financial_year":"2026-27"}`))
		req.Header.Set("Resolved-Tenant-ID", "default")
		req.Header.Set("Resolved-Role", role)
		req.Header.Set("Resolved-User-ID", "TEST-USER-14814C")
		rec := httptest.NewRecorder()
		handleConvertRequisition(rec, req)
		return rec
	}
	for _, target := range []string{"PurchaseOrder", "RFQ"} {
		if rec := call(target); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "GLOBAL-0011") {
			t.Fatalf("%s without create: want 403 GLOBAL-0011, got %d %s", target, rec.Code, rec.Body.String())
		}
		if _, err := db.DB.Exec(`INSERT INTO `+schema+`.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES ($1,$2,TRUE,TRUE,FALSE,FALSE)`, role, target); err != nil {
			t.Fatal(err)
		}
		// Past the gate: the engine then refuses the made-up requisition, which is not a permission refusal.
		if rec := call(target); rec.Code == http.StatusForbidden || strings.Contains(rec.Body.String(), "GLOBAL-0011") {
			t.Fatalf("%s with create: want past the gate, got %d %s", target, rec.Code, rec.Body.String())
		}
	}
	// Create on one target does not open the other.
	cleanup()
	if _, err := db.DB.Exec(`INSERT INTO `+schema+`.role_permissions (role, doctype_name, allow_read, allow_create) VALUES ($1,'RFQ',TRUE,TRUE)`, role); err != nil {
		t.Fatal(err)
	}
	if rec := call("PurchaseOrder"); rec.Code != http.StatusForbidden {
		t.Fatalf("create on RFQ only: PurchaseOrder must be refused, got %d", rec.Code)
	}
}

// 148.12c: matching a bill needs finance.post or update on VendorInvoice, and the
// caller's tolerance is ignored.
func TestMatchVendorInvoiceGateAndTolerance(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	const role, user = "TEST-ROLE-14812C", "TEST-USER-14812C"
	const po, grn, inv = "TEST-14812C-PO", "TEST-14812C-GRN", "TEST-14812C-VI"
	cleanup := func() {
		_, _ = db.DB.Exec(`DELETE FROM `+schema+`.role_permissions WHERE role=$1`, role)
		_, _ = db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE id IN ($1,$2,$3)`, po, grn, inv)
		_, _ = db.DB.Exec(`DELETE FROM `+schema+`.audit_logs WHERE user_id=$1`, user)
	}
	cleanup()
	t.Cleanup(cleanup)
	put := func(id, doctype, status string, data map[string]interface{}) {
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec(`INSERT INTO `+schema+`.documents (id,doctype,data,status,created_by) VALUES ($1,$2,$3,$4,'system')`, id, doctype, raw, status); err != nil {
			t.Fatal(err)
		}
	}
	// A PO with no item rates: the bill must equal the PO total within the tolerance.
	// 1100 against 1000 is 10% out: held at the configured 2%, matched at a caller's 50%.
	put(po, "PurchaseOrder", "Approved", map[string]interface{}{"code": po, "total_amount": 1000.0, "grand_total": 1000.0})
	put(grn, "GRN", "Posted", map[string]interface{}{"code": grn, "po_id": po})
	put(inv, "VendorInvoice", "Draft", map[string]interface{}{"code": inv, "po_id": po, "grn_id": grn, "invoice_amount": 1100.0})
	call := func(role, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/procurement/vendor-invoice/match", strings.NewReader(body))
		req.Header.Set("Resolved-Tenant-ID", "default")
		req.Header.Set("Resolved-Role", role)
		req.Header.Set("Resolved-User-ID", user)
		rec := httptest.NewRecorder()
		handleMatchVendorInvoice(rec, req)
		return rec
	}
	body := `{"invoice_id":"` + inv + `","tolerance_percent":50}`
	if rec := call(role, body); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "GLOBAL-0011") {
		t.Fatalf("a role with neither finance.post nor update on VendorInvoice: want 403 GLOBAL-0011, got %d %s", rec.Code, rec.Body.String())
	}
	if _, err := db.DB.Exec(`INSERT INTO `+schema+`.role_permissions (role, doctype_name, allow_read, allow_update) VALUES ($1,'VendorInvoice',TRUE,TRUE)`, role); err != nil {
		t.Fatal(err)
	}
	rec := call(role, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("update on VendorInvoice: want 200, got %d %s", rec.Code, rec.Body.String())
	}
	var res struct {
		Matched bool   `json:"matched"`
		Status  string `json:"status"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if res.Matched || res.Status != "MismatchHold" {
		t.Fatalf("the caller's tolerance_percent 50 must not apply: want MismatchHold, got %s", rec.Body.String())
	}
	// The configured tolerance still decides: a bill inside it matches.
	if _, err := db.DB.Exec(`UPDATE `+schema+`.documents SET data = data || '{"invoice_amount":1010}'::jsonb, status='Draft' WHERE id=$1`, inv); err != nil {
		t.Fatal(err)
	}
	if rec := call(role, body); !strings.Contains(rec.Body.String(), `"matched":true`) {
		t.Fatalf("a bill within the configured tolerance matches: got %d %s", rec.Code, rec.Body.String())
	}
	// finance.post alone (Accountant) passes the gate without any VendorInvoice row.
	holder := ""
	for _, r := range engines.RolesWithCapability("finance.post") {
		if !engines.HasFullAccess(r) {
			holder = r
			break
		}
	}
	if holder == "" {
		t.Skip("no non-admin role holds finance.post")
	}
	if rec := call(holder, body); rec.Code == http.StatusForbidden {
		t.Fatalf("%s holds finance.post: want past the gate, got %d %s", holder, rec.Code, rec.Body.String())
	}
}
