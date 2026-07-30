package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"testing"
)

func TestPIMDashboardRouteRequiresAuthenticationAndHonorsModuleGate(t *testing.T) {
	db.InitDB(testConnStr())
	handler := apiMiddleware(moduleGate("pim", handlePIMDashboard))

	unauthenticated := doRequest(t, handler, http.MethodGet, "/api/v1/pim/dashboard", "", nil)
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated dashboard status=%d, want %d", unauthenticated.Code, http.StatusUnauthorized)
	}

	wasEnabled, err := engines.IsModuleEnabled("default", "pim")
	if err != nil {
		t.Fatalf("read PIM module entitlement: %v", err)
	}
	if err := engines.SetModuleEntitlement("default", "pim", true, "test"); err != nil {
		t.Fatalf("enable PIM module for route test: %v", err)
	}
	defer func() { _ = engines.SetModuleEntitlement("default", "pim", wasEnabled, "test-cleanup") }()

	// Stage 29.8: apiMiddleware now re-reads the token's user from the
	// database on every request, so a token minted for a user id that doesn't
	// exist is correctly rejected as a dead session. Seed the row the token
	// claims to be - which is also what a real login would have required.
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.users (id, username, password_hash, role, status, location_code)
		VALUES ('dashboard-test', 'dashboard-test', 'x', 'HR/Admin', 'Active', 'HO')
		ON CONFLICT (id) DO UPDATE SET status = 'Active', role = 'HR/Admin'`); err != nil {
		t.Fatalf("seed dashboard test user: %v", err)
	}
	defer func() {
		_, _ = db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = 'dashboard-test'`)
		engines.ResetLiveUserStateCache()
	}()
	engines.ResetLiveUserStateCache()

	token := engines.SignToken("dashboard-test", "dashboard-test", "HR/Admin", "default", "HO")
	authorized := doRequest(t, handler, http.MethodGet, "/api/v1/pim/dashboard", token, nil)
	if authorized.Code != http.StatusOK {
		t.Fatalf("authorized dashboard status=%d body=%s", authorized.Code, authorized.Body.String())
	}
	var payload engines.PIMDashboard
	if err := json.Unmarshal(authorized.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode dashboard response: %v", err)
	}

	if err := engines.SetModuleEntitlement("default", "pim", false, "test"); err != nil {
		t.Fatalf("disable PIM module for route test: %v", err)
	}
	disabled := doRequest(t, handler, http.MethodGet, "/api/v1/pim/dashboard", token, nil)
	if disabled.Code != http.StatusForbidden {
		t.Fatalf("disabled PIM module dashboard status=%d, want %d", disabled.Code, http.StatusForbidden)
	}
}
