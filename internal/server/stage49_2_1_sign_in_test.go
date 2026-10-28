package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// Stage 49.2.1: last_sign_in_at is stamped only when a full session is issued
// - a password login for a non-MFA role, MFA activation, MFA verification -
// never for the enrollment or challenge purpose token in between.
func TestStage4921SignInStampsOnlyAFullSession(t *testing.T) {
	db.InitDB(testConnStr())
	stamped := func(id string) bool {
		t.Helper()
		var ok bool
		db.DB.QueryRow(`SELECT last_sign_in_at IS NOT NULL FROM tenant_default.users WHERE id = $1`, id).Scan(&ok)
		return ok
	}
	clear := func(id string) {
		db.DB.Exec(`UPDATE tenant_default.users SET last_sign_in_at = NULL WHERE id = $1`, id)
	}
	login := func(user, pw, ip string) map[string]interface{} {
		t.Helper()
		rec := doRequestFromIP(t, apiMiddleware(handleLogin), "POST", "/api/v1/login", "", map[string]string{"username": user, "password": pw}, ip)
		if rec.Code != http.StatusOK {
			t.Fatalf("login %s: %d %s", user, rec.Code, rec.Body.String())
		}
		var resp map[string]interface{}
		json.Unmarshal(rec.Body.Bytes(), &resp)
		return resp
	}

	// MFA role: the enrollment token is not a sign-in; activation is.
	const admin = "t4921-signin-admin"
	pw, cleanup := seedMFATestUser(t, admin)
	defer cleanup()
	ip := mfaTestIP()
	if resp := login(admin, pw, ip); resp["enrollment_token"] == nil || stamped(admin) {
		t.Fatalf("enrollment step stamped a sign-in (or gave no enrollment token): %v", resp)
	}
	_, recovery, _ := enrollMFATestUser(t, admin, pw, ip)
	if !stamped(admin) {
		t.Fatal("MFA activation issued a session but did not stamp last_sign_in_at")
	}

	// The challenge token is not a sign-in; verification is.
	clear(admin)
	resp := login(admin, pw, ip)
	challenge, _ := resp["challenge_token"].(string)
	if challenge == "" || stamped(admin) {
		t.Fatalf("challenge step stamped a sign-in (or gave no challenge): %v", resp)
	}
	if len(recovery) == 0 {
		t.Fatal("no recovery codes to verify with")
	}
	verify := doRequestFromIP(t, apiMiddleware(handleMFAVerify), "POST", "/api/v1/auth/mfa/verify", challenge, map[string]string{"code": recovery[0]}, ip)
	if verify.Code != http.StatusOK || !stamped(admin) {
		t.Fatalf("MFA verify: %d %s, stamped %v", verify.Code, verify.Body.String(), stamped(admin))
	}

	// Non-MFA role: the password login itself issues the session.
	const cashier = "t4921-signin-cashier"
	cpw := randomTestPassword()
	hash, _ := bcrypt.GenerateFromPassword([]byte(cpw), bcrypt.MinCost)
	db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, cashier)
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.users (id, username, password_hash, email, role, status)
		VALUES ($1, $1, $2, $3, $4, 'Active')`, cashier, string(hash), cashier+"@erp.com", engines.RoleCashier); err != nil {
		t.Fatalf("seed cashier: %v", err)
	}
	defer db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, cashier)
	if resp := login(cashier, cpw, mfaTestIP()); resp["token"] == nil || !stamped(cashier) {
		t.Fatalf("password session did not stamp last_sign_in_at: %v", resp)
	}

	// The admin Users list shows it.
	rec := doRequestFromIP(t, apiMiddleware(handleListUsers), "GET", "/api/v1/admin/users", engines.SignToken("admin", "admin", engines.RoleSuperAdmin, "default", "HO", currentCredentialVersion("admin")), nil, mfaTestIP())
	var users []map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &users)
	found := false
	for _, u := range users {
		if u["id"] == cashier {
			found = u["last_sign_in_at"] != ""
		}
	}
	if rec.Code != http.StatusOK || !found {
		t.Errorf("users list: %d, cashier last_sign_in_at shown %v", rec.Code, found)
	}
}

// Stage 49.2.1a (user, 2026-10-28: "take std practice"): a store switch is the
// user working, so it stamps the dormant-account clock like a sign-in does.
func TestStage4921aStoreSwitchCountsAsActivity(t *testing.T) {
	db.InitDB(testConnStr())
	if _, err := db.DB.Exec(`UPDATE tenant_default.users SET last_sign_in_at = NULL WHERE id = 'admin'`); err != nil {
		t.Fatalf("clear admin: %v", err)
	}
	token := engines.SignToken("admin", "admin", engines.RoleSuperAdmin, "default", "HO", currentCredentialVersion("admin"))
	rec := doRequest(t, apiMiddleware(handleSwitchWorkingStore), http.MethodPost, "/api/v1/me/working-store", token, map[string]string{"store": "HO"})
	if rec.Code != http.StatusOK {
		t.Fatalf("switch store: %d %s", rec.Code, rec.Body.String())
	}
	var stamped bool
	db.DB.QueryRow(`SELECT last_sign_in_at IS NOT NULL FROM tenant_default.users WHERE id = 'admin'`).Scan(&stamped)
	if !stamped {
		t.Fatal("a store switch issued a session but did not stamp last_sign_in_at")
	}
}
