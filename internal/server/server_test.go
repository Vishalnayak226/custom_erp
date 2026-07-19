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
	connStr := "postgres://postgres@localhost:5435/custom_erp?sslmode=disable"
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
	connStr := "postgres://postgres@localhost:5435/custom_erp?sslmode=disable"
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
	if !bytes.Contains(rec.Body.Bytes(), []byte("Module 'hr' is disabled")) {
		t.Errorf("expected the module-disabled error message, got body=%s", rec.Body.String())
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
	connStr := "postgres://postgres@localhost:5435/custom_erp?sslmode=disable"
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
