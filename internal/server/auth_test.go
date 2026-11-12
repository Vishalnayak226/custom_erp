package server

import (
	"custom_erp/db"
	"encoding/json"
	"net/http"
	"testing"
)

// Stage 49.2.8 (MFA downgrade): once an account has MFA, the right password
// alone yields only a short-lived challenge token - never a session - and that
// token opens nothing but the MFA verify step.
func TestStage49PasswordAloneNeverOpensAnMFAAccount(t *testing.T) {
	db.InitDB(testConnStr())
	const user = "stage49-mfa-downgrade"
	pw, cleanup := seedMFATestUser(t, user)
	defer cleanup()
	ip := mfaTestIP()
	enrollMFATestUser(t, user, pw, ip)

	loginRec := doRequestFromIP(t, apiMiddleware(handleLogin), "POST", "/api/v1/login", "", map[string]string{"username": user, "password": pw}, ip)
	var resp map[string]interface{}
	json.Unmarshal(loginRec.Body.Bytes(), &resp)
	if loginRec.Code != http.StatusOK || resp["token"] != nil {
		t.Fatalf("a password login on an MFA account must not return a session token: %d %s", loginRec.Code, loginRec.Body.String())
	}
	challenge, _ := resp["challenge_token"].(string)
	if challenge == "" {
		t.Fatalf("expected a challenge token: %s", loginRec.Body.String())
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/doc/{doctype}", apiMiddleware(handleGenericDoc))
	mux.HandleFunc("GET /api/v1/me", apiMiddleware(handleGetProfile))
	for _, route := range []string{"/api/v1/doc/Brand", "/api/v1/me"} {
		if rec := doRequestFromIP(t, mux.ServeHTTP, "GET", route, challenge, nil, ip); rec.Code != http.StatusUnauthorized {
			t.Errorf("the MFA challenge token reached %s: %d - it must open nothing but the verify step", route, rec.Code)
		}
	}
}
