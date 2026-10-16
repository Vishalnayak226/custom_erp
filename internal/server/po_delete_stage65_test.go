package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Stage 65.2: a PO can be deleted at any status - approved and sent included -
// until goods are received against it (directly, or on a multi-PO receipt that
// lists it in po_ids); a role without Delete on Purchase Order cannot.
func TestPurchaseOrderDeleteUntilGoodsReceived(t *testing.T) {
	db.InitDB(testConnStr())
	const freePO, receivedPO, multiPO, grnID, multiGRN = "PO-DEL65-FREE", "PO-DEL65-RECV", "PO-DEL65-MULTI", "GRN-DEL65-1", "GRN-DEL65-2"
	ids := []interface{}{freePO, receivedPO, multiPO, grnID, multiGRN}
	clean := func() {
		db.DB.Exec("DELETE FROM tenant_default.documents WHERE id IN ($1, $2, $3, $4, $5)", ids...)
	}
	clean()
	t.Cleanup(clean)
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES
		($1, 'PurchaseOrder', '{"po_number":"PO-DEL65-FREE","sent_to_vendor_at":"2026-10-16"}', 'Approved', 'system'),
		($2, 'PurchaseOrder', '{"po_number":"PO-DEL65-RECV"}', 'Approved', 'system'),
		($3, 'PurchaseOrder', '{"po_number":"PO-DEL65-MULTI"}', 'Approved', 'system'),
		($4, 'GRN', '{"po_id":"PO-DEL65-RECV"}', 'Posted', 'system'),
		($5, 'GRN', '{"po_id":"PO-OTHER","po_ids":"PO-OTHER,PO-DEL65-MULTI"}', 'Posted', 'system')`, ids...); err != nil {
		t.Fatalf("seed: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/doc/{doctype}/{id}", apiMiddleware(handleGenericDoc))
	// apiMiddleware re-reads the live user, so each role needs a real user of it.
	request := func(user, role, path string) *httptest.ResponseRecorder {
		token := engines.SignToken(user, user, role, "default", "HO", currentCredentialVersion(user))
		req := httptest.NewRequest(http.MethodDelete, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	if rec := request("manager1", "Store Manager", "/api/v1/doc/PurchaseOrder/"+freePO); rec.Code != http.StatusForbidden {
		t.Errorf("role without Delete: status=%d, want 403 (%s)", rec.Code, rec.Body.String())
	}
	if rec := request("system", "Super Admin", "/api/v1/doc/PurchaseOrder/"+freePO); rec.Code != http.StatusOK {
		t.Errorf("approved + sent PO with no receipt: status=%d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	for _, po := range []string{receivedPO, multiPO} {
		rec := request("system", "Super Admin", "/api/v1/doc/PurchaseOrder/"+po)
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "PURCHA-0320") {
			t.Errorf("%s with goods received: status=%d body=%s, want 422 PURCHA-0320", po, rec.Code, rec.Body.String())
		}
	}
	// Once the receipt is cancelled, the PO can go.
	if _, err := db.DB.Exec(`UPDATE tenant_default.documents SET status = 'Cancelled' WHERE id = $1`, grnID); err != nil {
		t.Fatal(err)
	}
	if rec := request("system", "Super Admin", "/api/v1/doc/PurchaseOrder/"+receivedPO); rec.Code != http.StatusOK {
		t.Errorf("after the receipt was cancelled: status=%d, want 200 (%s)", rec.Code, rec.Body.String())
	}
}
