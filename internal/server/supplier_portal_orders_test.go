package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"custom_erp/db"

	"golang.org/x/crypto/bcrypt"
)

// Stage 168.4: a Supplier login reaches the supplier-portal routes and the
// few shared lookups, and no other API; a staff role is refused on the portal
// routes. The confinement lives in checkRouteCapability.
func TestSupplierPortalConfinement(t *testing.T) {
	db.InitDB(testConnStr())
	const (
		supUser   = "__supportal_orders_sup__"
		staffUser = "__supportal_orders_staff__"
		vendor    = "__SUPPORTAL_ORDERS_VENDOR"
	)
	defer seedVendor(t, vendor)()
	pw, cleanup := seedSupplierUser(t, supUser, vendor)
	defer cleanup()

	staffPW := randomTestPassword()
	hash, err := bcrypt.GenerateFromPassword([]byte(staffPW), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, staffUser)
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.users (id, username, password_hash, email, role, status)
		VALUES ($1, $1, $2, $3, 'Cashier', 'Active')`, staffUser, string(hash), staffUser+"@staff.test"); err != nil {
		t.Fatalf("seed staff: %v", err)
	}
	defer db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, staffUser)

	supTok := plainSessionToken(t, supUser, pw, mfaTestIP())
	staffTok := plainSessionToken(t, staffUser, staffPW, mfaTestIP())

	call := func(h http.HandlerFunc, path, tok string) int {
		rec := httptest.NewRecorder()
		apiMiddleware(h)(rec, newJSONRequest(t, "GET", path, tok, nil))
		return rec.Code
	}

	if c := call(handleListPendingApprovals, "/api/v1/approval/pending", supTok); c != http.StatusForbidden {
		t.Errorf("Supplier on approval/pending: want 403, got %d", c)
	}
	// 26.4.10 keeps working: Submit for approval on a supplier's own
	// submission is the one approval route left open (a missing record here,
	// so the engine's 4xx - anything but the capability gate's 403).
	subRec := httptest.NewRecorder()
	apiMiddleware(handleSubmitApproval)(subRec, newJSONRequest(t, "POST", "/api/v1/approval/submit", supTok,
		map[string]interface{}{"doctype": "SupplierSubmission", "document_id": "__NO_SUCH_SUBMISSION"}))
	if subRec.Code == http.StatusForbidden {
		t.Errorf("Supplier on approval/submit: refused (%s), want it to reach the submit handler", subRec.Body.String())
	}
	if c := call(handleSupplierPortalOrders, "/api/v1/supplier-portal/orders", supTok); c != http.StatusOK {
		t.Errorf("Supplier on own portal orders: want 200, got %d", c)
	}
	for _, p := range []string{"orders", "asns", "bills", "payments"} {
		h := map[string]http.HandlerFunc{"orders": handleSupplierPortalOrders, "asns": handleSupplierPortalASNs,
			"bills": handleSupplierPortalBills, "payments": handleSupplierPortalPayments}[p]
		if c := call(h, "/api/v1/supplier-portal/"+p, staffTok); c != http.StatusForbidden {
			t.Errorf("staff role on supplier-portal/%s: want 403, got %d", p, c)
		}
	}
}
