package main

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
	defer func() {
		db.DB.Exec(`DELETE FROM tenant_default.inventory_availability WHERE sku = $1 AND location_code = $2`, sku, location)
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE doctype = 'POSCart' AND id = 'INTEGRATIONTEST-CART'`)
	}()

	if _, err := db.DB.Exec(`INSERT INTO tenant_default.inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 50, 50)`, sku, location); err != nil {
		t.Fatalf("failed to seed inventory: %v", err)
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
