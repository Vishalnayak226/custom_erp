package server

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"

	"golang.org/x/crypto/bcrypt"
)

// randomTestPassword returns a throwaway high-entropy password for a disposable test user.
// Never logged or asserted on directly - only used to obtain a real token via the real
// login handler, matching how a genuine client would authenticate.
func randomTestPassword() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "T" + hex.EncodeToString(b) + "!1"
}

func doRequest(t *testing.T, handler http.HandlerFunc, method, path, token string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("failed to marshal request body: %v", err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

// TestCheckoutToForecastIntegration drives the real HTTP handlers end-to-end - login,
// checkout, and demand forecast - rather than calling engine functions directly with
// hand-inserted fixtures. This is the regression test for a real bug found in this
// codebase: CalculateSalesVelocity used to filter on a POSCart status ('completed')
// that handleCheckout never actually wrote ('Paid'), so forecasts silently computed
// zero velocity against all real checkout traffic. The old unit test for this used a
// SQL fixture with status='completed' directly, which is exactly why it never caught
// the mismatch - this test cannot make that mistake, because it goes through the
// actual handleCheckout handler and can only observe what it actually persists.
func TestCheckoutToForecastIntegration(t *testing.T) {
	connStr := testConnStr()
	db.InitDB(connStr)

	testUser := "__integrationtest_user__"
	pw := randomTestPassword()
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to hash test password: %v", err)
	}
	db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, testUser)
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.users (id, username, password_hash, email, role, status) VALUES ($1, $1, $2, $3, 'HR/Admin', 'Active')`, testUser, string(hash), testUser+"@erp.com"); err != nil {
		t.Fatalf("failed to seed test user: %v", err)
	}
	defer db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, testUser)

	sku := "INTEGRATIONTEST-SKU"
	location := "INTEGRATIONTEST-LOC"
	db.DB.Exec(`DELETE FROM tenant_default.inventory_availability WHERE sku = $1 AND location_code = $2`, sku, location)
	db.DB.Exec(`DELETE FROM tenant_default.documents WHERE doctype = 'POSCart' AND id = 'INTEGRATIONTEST-CART'`)
	db.DB.Exec(`DELETE FROM tenant_default.documents WHERE doctype = 'Item' AND id = $1`, sku)
	defer func() {
		db.DB.Exec(`DELETE FROM tenant_default.inventory_availability WHERE sku = $1 AND location_code = $2`, sku, location)
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE doctype = 'POSCart' AND id = 'INTEGRATIONTEST-CART'`)
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE doctype = 'Item' AND id = $1`, sku)
	}()

	if _, err := db.DB.Exec(`INSERT INTO tenant_default.inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 50, 50)`, sku, location); err != nil {
		t.Fatalf("failed to seed inventory: %v", err)
	}
	// Stage 17.5: checkout now gates on the Item having hsn_code/gst_rate set.
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')`,
		sku, `{"name":"Integration Test Item","hsn_code":"6109","gst_rate":18}`); err != nil {
		t.Fatalf("failed to seed test item: %v", err)
	}

	// 1. Real login via the real handler chain (apiMiddleware + handleLogin).
	// This test user is HR/Admin, which is MFA-mandatory (Stage 13.3) - a
	// fresh user always has mfa_enabled=false, so login returns an
	// enrollment token instead of a session token.
	loginRec := doRequest(t, apiMiddleware(handleLogin), "POST", "/api/v1/login", "", map[string]string{
		"username": testUser,
		"password": pw,
	})
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login failed: status=%d body=%s", loginRec.Code, loginRec.Body.String())
	}
	var loginResp map[string]interface{}
	if err := json.Unmarshal(loginRec.Body.Bytes(), &loginResp); err != nil {
		t.Fatalf("failed to decode login response: %v", err)
	}
	enrollmentToken, _ := loginResp["enrollment_token"].(string)
	if enrollmentToken == "" {
		t.Fatalf("expected login to require MFA enrollment for a fresh HR/Admin user, got: %s", loginRec.Body.String())
	}

	// 1a. Real MFA enrollment - obtain a secret via the real handler, exactly
	// as a client would before rendering a QR code.
	enrollRec := doRequest(t, apiMiddleware(handleMFAEnroll), "POST", "/api/v1/auth/mfa/enroll", enrollmentToken, nil)
	if enrollRec.Code != http.StatusOK {
		t.Fatalf("MFA enroll failed: status=%d body=%s", enrollRec.Code, enrollRec.Body.String())
	}
	var enrollResp map[string]string
	if err := json.Unmarshal(enrollRec.Body.Bytes(), &enrollResp); err != nil {
		t.Fatalf("failed to decode MFA enroll response: %v", err)
	}
	secret := enrollResp["secret"]
	if secret == "" {
		t.Fatalf("MFA enroll succeeded but returned no secret")
	}

	// 1b. Real MFA activation - compute the same code an authenticator app
	// would show for this secret right now, and submit it via the real
	// handler. This both activates MFA and completes login.
	code, err := engines.GenerateTOTPCode(secret)
	if err != nil {
		t.Fatalf("failed to compute TOTP code: %v", err)
	}
	activateRec := doRequest(t, apiMiddleware(handleMFAActivate), "POST", "/api/v1/auth/mfa/activate", enrollmentToken, map[string]string{
		"code": code,
	})
	if activateRec.Code != http.StatusOK {
		t.Fatalf("MFA activate failed: status=%d body=%s", activateRec.Code, activateRec.Body.String())
	}
	var activateResp map[string]string
	if err := json.Unmarshal(activateRec.Body.Bytes(), &activateResp); err != nil {
		t.Fatalf("failed to decode MFA activate response: %v", err)
	}
	token := activateResp["token"]
	if token == "" {
		t.Fatalf("MFA activation succeeded but returned no session token")
	}

	// 1c. Stage 20.7: checkout now requires an open cashier session for the
	// acting user's location - open one the same way a real session start
	// would (engines.OpenPOSSession, keyed off this same testUser as cashier).
	if _, err := engines.OpenPOSSession("default", "", location, testUser, testUser, 0); err != nil {
		t.Fatalf("failed to open POS session: %v", err)
	}
	defer db.DB.Exec(`DELETE FROM tenant_default.documents WHERE doctype = 'POSSession' AND data->>'cashier' = $1`, testUser)

	// 2. Real checkout via the real handler chain - this is what actually writes POSCart's status
	checkoutRec := doRequest(t, apiMiddleware(handleCheckout), "POST", "/api/v1/checkout", token, map[string]interface{}{
		"cart_number":  "INTEGRATIONTEST-CART",
		"location":     location,
		"payment_mode": "Cash",
		"items": []map[string]interface{}{
			{"sku": sku, "qty": 20, "sale_price": 100, "cost_price": 60},
		},
	})
	if checkoutRec.Code != http.StatusOK {
		t.Fatalf("checkout failed: status=%d body=%s", checkoutRec.Code, checkoutRec.Body.String())
	}

	// 3. Confirm the checkout actually wrote what we expect it to (documents this invariant
	// explicitly, so a future change to the status value trips this test immediately)
	var storedStatus string
	if err := db.DB.QueryRow(`SELECT status FROM tenant_default.documents WHERE id = 'INTEGRATIONTEST-CART' AND doctype = 'POSCart'`).Scan(&storedStatus); err != nil {
		t.Fatalf("failed to read back created POSCart: %v", err)
	}
	if storedStatus != "Paid" {
		t.Errorf("expected checkout to create a POSCart with status 'Paid', got %q", storedStatus)
	}

	// 4. Real forecast call via the real handler chain - the exact path that used to always
	// return zero regardless of real sales.
	forecastRec := doRequest(t, apiMiddleware(handleDemandForecast), "POST", "/api/v1/optimization/forecast", token, map[string]interface{}{
		"location_code": location,
		"sku":           sku,
		"forecast_days": 30,
	})
	if forecastRec.Code != http.StatusOK {
		t.Fatalf("forecast call failed: status=%d body=%s", forecastRec.Code, forecastRec.Body.String())
	}
	var forecastResp map[string]interface{}
	if err := json.Unmarshal(forecastRec.Body.Bytes(), &forecastResp); err != nil {
		t.Fatalf("failed to decode forecast response: %v", err)
	}
	demand, _ := forecastResp["forecasted_demand"].(float64)

	// 20 units sold / 30-day lookback * 30 forecast days = 20.0
	if demand != 20.0 {
		t.Errorf("expected forecasted_demand to reflect the real checkout (20.0), got %v - "+
			"if this is 0, the checkout-to-forecast status mismatch has regressed", demand)
	}
}

// TestModuleGateBlocksAndRestoresDoctypeAccess (Stage 14.1) drives the real
// generic-doc handler chain end-to-end against tenant_default's "hr" module:
// access is open by default, a disabled module_entitlements row 403s the
// same doctype it did not before, and re-enabling restores it - proving the
// runtime module_key resolution added to handleGenericDoc (handlers_core_doc_engine.go, right
// next to the existing checkPermission call) actually takes effect, not just
// that engines.IsModuleEnabled/SetModuleEntitlement compile.
func TestModuleGateBlocksAndRestoresDoctypeAccess(t *testing.T) {
	connStr := testConnStr()
	db.InitDB(connStr)

	testUser := "__moduletest_user__"
	pw := randomTestPassword()
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to hash test password: %v", err)
	}
	db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, testUser)
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.users (id, username, password_hash, email, role, status) VALUES ($1, $1, $2, $3, 'HR/Admin', 'Active')`, testUser, string(hash), testUser+"@erp.com"); err != nil {
		t.Fatalf("failed to seed test user: %v", err)
	}
	defer db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, testUser)

	// Always restore "hr" to enabled afterward, regardless of test outcome -
	// this runs against the shared tenant_default schema, not a disposable tenant.
	defer func() {
		if err := engines.SetModuleEntitlement("default", "hr", true, "test-cleanup"); err != nil {
			t.Logf("cleanup: failed to re-enable hr module: %v", err)
		}
	}()

	// Real login + MFA enrollment/activation, same pattern as TestCheckoutToForecastIntegration.
	loginRec := doRequest(t, apiMiddleware(handleLogin), "POST", "/api/v1/login", "", map[string]string{
		"username": testUser,
		"password": pw,
	})
	var loginResp map[string]interface{}
	if err := json.Unmarshal(loginRec.Body.Bytes(), &loginResp); err != nil {
		t.Fatalf("failed to decode login response: %v", err)
	}
	enrollmentToken, _ := loginResp["enrollment_token"].(string)
	if enrollmentToken == "" {
		t.Fatalf("expected login to require MFA enrollment for a fresh HR/Admin user, got: %s", loginRec.Body.String())
	}
	enrollRec := doRequest(t, apiMiddleware(handleMFAEnroll), "POST", "/api/v1/auth/mfa/enroll", enrollmentToken, nil)
	var enrollResp map[string]string
	if err := json.Unmarshal(enrollRec.Body.Bytes(), &enrollResp); err != nil {
		t.Fatalf("failed to decode MFA enroll response: %v", err)
	}
	code, err := engines.GenerateTOTPCode(enrollResp["secret"])
	if err != nil {
		t.Fatalf("failed to compute TOTP code: %v", err)
	}
	activateRec := doRequest(t, apiMiddleware(handleMFAActivate), "POST", "/api/v1/auth/mfa/activate", enrollmentToken, map[string]string{"code": code})
	var activateResp map[string]string
	if err := json.Unmarshal(activateRec.Body.Bytes(), &activateResp); err != nil {
		t.Fatalf("failed to decode MFA activate response: %v", err)
	}
	token := activateResp["token"]
	if token == "" {
		t.Fatalf("MFA activation succeeded but returned no session token: %s", activateRec.Body.String())
	}

	getEmployee := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/doc/Employee", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.SetPathValue("doctype", "Employee")
		rec := httptest.NewRecorder()
		apiMiddleware(handleGenericDoc)(rec, req)
		return rec
	}

	// 1. Baseline: "hr" module enabled by default, Employee list should be reachable.
	if rec := getEmployee(); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with hr module enabled, got status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 2. Disable "hr" via the real engine function, confirm the same request now 403s.
	if err := engines.SetModuleEntitlement("default", "hr", false, "test"); err != nil {
		t.Fatalf("failed to disable hr module: %v", err)
	}
	rec := getEmployee()
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 with hr module disabled, got status=%d body=%s", rec.Code, rec.Body.String())
	}
	// Stage 23: moduleGate now returns the standardized SAAS-0191 catalog
	// message/code instead of a dynamic per-module string.
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"code":"SAAS-0191"`)) {
		t.Errorf("expected the SAAS-0191 module-disabled error code, got body=%s", rec.Body.String())
	}

	// 3. Re-enable, confirm access is restored (not just that the module flips in isolation).
	if err := engines.SetModuleEntitlement("default", "hr", true, "test"); err != nil {
		t.Fatalf("failed to re-enable hr module: %v", err)
	}
	if rec := getEmployee(); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 after re-enabling hr module, got status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestVersionEndpointIsPublicAndTenantStampingWorks (Stage 14.6) checks two
// things the plan explicitly called out: /api/v1/version must be reachable
// with no Authorization header at all (same tier as /login), and
// engines.ProvisionTenantSchema must actually persist the version it was
// called with, not silently drop it.
func TestVersionEndpointIsPublicAndTenantStampingWorks(t *testing.T) {
	connStr := testConnStr()
	db.InitDB(connStr)

	// 1. No Authorization header at all - must not 401.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
	rec := httptest.NewRecorder()
	apiMiddleware(handleVersion)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected /api/v1/version to be reachable with no auth header, got status=%d body=%s", rec.Code, rec.Body.String())
	}
	var versionResp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &versionResp); err != nil {
		t.Fatalf("failed to decode version response: %v", err)
	}
	if versionResp["version"] != currentAppVersion() {
		t.Errorf("expected version %q, got %q", currentAppVersion(), versionResp["version"])
	}

	// 2. Provisioning stamps the tenant with the version it's called with.
	testTenant := "__versiontest_tenant__"
	testSchema := "tenant___versiontest_tenant__"
	db.DB.Exec(`DROP SCHEMA IF EXISTS ` + testSchema + ` CASCADE`)
	db.DB.Exec(`DELETE FROM public.tenants WHERE tenant_id = $1`, testTenant)
	defer func() {
		db.DB.Exec(`DROP SCHEMA IF EXISTS ` + testSchema + ` CASCADE`)
		db.DB.Exec(`DELETE FROM public.tenants WHERE tenant_id = $1`, testTenant)
	}()

	if _, err := engines.ProvisionTenantSchema(testTenant, testSchema, "9.9.9-test"); err != nil {
		t.Fatalf("failed to provision test tenant: %v", err)
	}
	var recordedVersion string
	if err := db.DB.QueryRow(`SELECT app_version FROM public.tenants WHERE tenant_id = $1`, testTenant).Scan(&recordedVersion); err != nil {
		t.Fatalf("failed to read back stamped version: %v", err)
	}
	if recordedVersion != "9.9.9-test" {
		t.Errorf("expected stamped app_version '9.9.9-test', got %q", recordedVersion)
	}
}

// TestCrossTenantIsolationAndTokenSecurity (Stage 11.1). The checklist item this closes was
// re-opened 2026-07-12 pointing at two specific findings - the "no auth header -> admin"
// fallback and the generic-list SQL injection - both fixed and verified the same day
// (docs/operations/hardening_roadmap.md Phase 1, closed 2026-07-12) but never re-verified
// against a passing bar afterward. This is that re-verification, executed fresh rather than
// just trusting the old note, covering both halves the item names: cross-tenant role
// boundaries and token verification.
//
// The cross-tenant half specifically drives an active spoofing attempt (a client-supplied
// X-Tenant-ID header naming a second, real tenant, sent alongside a validly-signed token for
// the first) rather than only checking default behavior - proving Resolved-Tenant-ID comes
// solely from the verified JWT's own "tenant" claim (middleware.go's Token & Tenant
// Resolution block) for any authenticated request, never from request-controlled input.
func TestCrossTenantIsolationAndTokenSecurity(t *testing.T) {
	connStr := testConnStr()
	db.InitDB(connStr)

	tenantA, schemaA := "__sectest_tenant_a__", "tenant___sectest_tenant_a__"
	tenantB, schemaB := "__sectest_tenant_b__", "tenant___sectest_tenant_b__"
	cleanup := func() {
		db.DB.Exec(`DROP SCHEMA IF EXISTS ` + schemaA + ` CASCADE`)
		db.DB.Exec(`DROP SCHEMA IF EXISTS ` + schemaB + ` CASCADE`)
		db.DB.Exec(`DELETE FROM public.tenants WHERE tenant_id IN ($1, $2)`, tenantA, tenantB)
	}
	cleanup()
	defer cleanup()

	if _, err := engines.ProvisionTenantSchema(tenantA, schemaA, ""); err != nil {
		t.Fatalf("failed to provision tenant A: %v", err)
	}
	if _, err := engines.ProvisionTenantSchema(tenantB, schemaB, ""); err != nil {
		t.Fatalf("failed to provision tenant B: %v", err)
	}
	if _, err := db.DB.Exec(`INSERT INTO ` + schemaA + `.documents (id, doctype, data, status, created_by) VALUES ('SECTEST-ITEM-A', 'Item', '{"name":"Tenant A Secret Item"}', 'Active', 'system')`); err != nil {
		t.Fatalf("failed to seed tenant A item: %v", err)
	}
	if _, err := db.DB.Exec(`INSERT INTO ` + schemaB + `.documents (id, doctype, data, status, created_by) VALUES ('SECTEST-ITEM-B', 'Item', '{"name":"Tenant B Secret Item"}', 'Active', 'system')`); err != nil {
		t.Fatalf("failed to seed tenant B item: %v", err)
	}

	// The user the token claims to be must actually exist and be Active:
	// Stage 29.8's live user-state re-check reads it back on every request, so
	// a token for a phantom user is now rejected as a dead session. Seeding it
	// keeps this test about tenant scoping rather than accidentally testing
	// that check.
	if _, err := db.DB.Exec(`INSERT INTO ` + schemaA + `.users (id, username, password_hash, role, status, location_code)
		VALUES ('sectest-user-a', 'sectest-user-a', 'x', 'HR/Admin', 'Active', 'HO')
		ON CONFLICT (id) DO UPDATE SET status = 'Active'`); err != nil {
		t.Fatalf("failed to seed tenant A user: %v", err)
	}
	engines.ResetLiveUserStateCache()

	// Minted directly rather than via /login - tests token-level tenant scoping
	// independent of the login/MFA flow. With the user row above in place this
	// is equivalent to what a real successful login would have issued.
	tokenA := engines.SignToken("sectest-user-a", "sectest-user-a", "HR/Admin", tenantA, "HO")

	// 1. Active spoofing attempt: tenant A's token, but the request also claims
	// X-Tenant-ID: tenant B while asking for a document that only exists in B's schema.
	// If tenant resolution ever preferred the header over the JWT claim, this would leak.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/doc/Item/SECTEST-ITEM-B", nil)
	req.Header.Set("Authorization", "Bearer "+tokenA)
	req.Header.Set("X-Tenant-ID", tenantB)
	req.SetPathValue("doctype", "Item")
	req.SetPathValue("id", "SECTEST-ITEM-B")
	rec := httptest.NewRecorder()
	apiMiddleware(handleGenericDoc)(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("cross-tenant read leaked: tenant A's token fetched tenant B's document via a spoofed X-Tenant-ID header, status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 2. Same token correctly reads its own tenant's document - proves the 404 above is
	// real isolation working correctly, not a broken handler returning 404 for everything.
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/doc/Item/SECTEST-ITEM-A", nil)
	req2.Header.Set("Authorization", "Bearer "+tokenA)
	req2.SetPathValue("doctype", "Item")
	req2.SetPathValue("id", "SECTEST-ITEM-A")
	rec2 := httptest.NewRecorder()
	apiMiddleware(handleGenericDoc)(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected tenant A's token to read its own document, status=%d body=%s", rec2.Code, rec2.Body.String())
	}

	// 3. List endpoint from tenant A never includes tenant B's document.
	req3 := httptest.NewRequest(http.MethodGet, "/api/v1/doc/Item", nil)
	req3.Header.Set("Authorization", "Bearer "+tokenA)
	req3.SetPathValue("doctype", "Item")
	rec3 := httptest.NewRecorder()
	apiMiddleware(handleGenericDoc)(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("expected tenant A's list call to succeed, status=%d body=%s", rec3.Code, rec3.Body.String())
	}
	if bytes.Contains(rec3.Body.Bytes(), []byte("SECTEST-ITEM-B")) {
		t.Errorf("cross-tenant leak: tenant A's Item list included tenant B's document: %s", rec3.Body.String())
	}

	// 4. Token verification: no Authorization header on a non-public route -> 401, not a
	// silent fallback (the exact class of bug Phase 1.1 fixed).
	req4 := httptest.NewRequest(http.MethodGet, "/api/v1/doc/Item", nil)
	req4.SetPathValue("doctype", "Item")
	rec4 := httptest.NewRecorder()
	apiMiddleware(handleGenericDoc)(rec4, req4)
	if rec4.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with no auth header, got %d", rec4.Code)
	}

	// 5. Token verification: malformed token -> 401.
	req5 := httptest.NewRequest(http.MethodGet, "/api/v1/doc/Item", nil)
	req5.Header.Set("Authorization", "Bearer not.a.valid.jwt")
	req5.SetPathValue("doctype", "Item")
	rec5 := httptest.NewRecorder()
	apiMiddleware(handleGenericDoc)(rec5, req5)
	if rec5.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with malformed token, got %d", rec5.Code)
	}

	// 6. Token verification: signature-tampered token (a real, validly-signed token with its
	// last character flipped) -> 401, not silently accepted with a corrupted-but-parseable claim set.
	lastChar := tokenA[len(tokenA)-1]
	replacement := byte('A')
	if lastChar == 'A' {
		replacement = 'B'
	}
	tampered := tokenA[:len(tokenA)-1] + string(replacement)
	req6 := httptest.NewRequest(http.MethodGet, "/api/v1/doc/Item", nil)
	req6.Header.Set("Authorization", "Bearer "+tampered)
	req6.SetPathValue("doctype", "Item")
	rec6 := httptest.NewRecorder()
	apiMiddleware(handleGenericDoc)(rec6, req6)
	if rec6.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with a signature-tampered token, got %d", rec6.Code)
	}
}
