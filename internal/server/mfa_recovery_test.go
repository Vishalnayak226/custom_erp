package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"

	"golang.org/x/crypto/bcrypt"
)

// Stage 32.5 regression tests for the MFA lockout hole: before this, losing a
// phone meant SSH-ing to the server and clearing mfa_enabled by hand, because
// there were no recovery codes and no way for an enrolled user to re-enroll.
//
// These drive the real handler chain (apiMiddleware + the real handlers) end
// to end rather than calling engine functions with hand-inserted fixtures -
// the same posture as TestCheckoutToForecastIntegration, and for the same
// reason: the properties that matter here (a recovery code is accepted where
// a TOTP code would be, and only once) live in the handler, not the engine.

// doRequestFromIP is doRequest with a caller-chosen source address.
//
// Needed because /api/v1/login is rate-limited to 5 requests per minute per
// client IP (Stage 13.14), and httptest.NewRequest gives every request the
// same RemoteAddr - so a few MFA tests in a row, each of which has to log in
// several times to obtain purpose tokens, otherwise trip the limiter and fail
// on 429 rather than on anything they are actually testing. Each test gets
// its own IP via mfaTestIP.
func doRequestFromIP(t *testing.T, handler http.HandlerFunc, method, path, token string, body interface{}, ip string) *httptest.ResponseRecorder {
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
	req.RemoteAddr = ip + ":54321"
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

// mfaTestIP hands each test its own address in the RFC 5737 TEST-NET-1 range,
// so one test's logins never count against another's rate-limit bucket.
var mfaTestIPCounter int

func mfaTestIP() string {
	mfaTestIPCounter++
	return fmt.Sprintf("203.0.113.%d", mfaTestIPCounter)
}

// seedMFATestUser creates a disposable HR/Admin user (MFA-mandatory per
// engines.RequiresMFA) and returns its id and password, plus a cleanup.
func seedMFATestUser(t *testing.T, id string) (string, func()) {
	t.Helper()
	pw := randomTestPassword()
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to hash test password: %v", err)
	}
	db.DB.Exec(`DELETE FROM tenant_default.mfa_recovery_codes WHERE user_id = $1`, id)
	db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, id)
	if _, err := db.DB.Exec(
		`INSERT INTO tenant_default.users (id, username, password_hash, email, role, status)
		 VALUES ($1, $1, $2, $3, 'HR/Admin', 'Active')`, id, string(hash), id+"@erp.com"); err != nil {
		t.Fatalf("failed to seed test user: %v", err)
	}
	return pw, func() {
		db.DB.Exec(`DELETE FROM tenant_default.mfa_recovery_codes WHERE user_id = $1`, id)
		db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, id)
	}
}

// enrollMFATestUser runs the real login -> enroll -> activate sequence and
// returns the active TOTP secret plus the recovery codes handed out.
func enrollMFATestUser(t *testing.T, user, pw, ip string) (secret string, recoveryCodes []string, sessionToken string) {
	t.Helper()
	loginRec := doRequestFromIP(t, apiMiddleware(handleLogin), "POST", "/api/v1/login", "", map[string]string{
		"username": user, "password": pw,
	}, ip)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login failed: status=%d body=%s", loginRec.Code, loginRec.Body.String())
	}
	var loginResp map[string]interface{}
	if err := json.Unmarshal(loginRec.Body.Bytes(), &loginResp); err != nil {
		t.Fatalf("failed to decode login response: %v", err)
	}
	enrollToken, _ := loginResp["enrollment_token"].(string)
	if enrollToken == "" {
		t.Fatalf("expected an enrollment token for a fresh HR/Admin user, got: %s", loginRec.Body.String())
	}

	enrollRec := doRequestFromIP(t, apiMiddleware(handleMFAEnroll), "POST", "/api/v1/auth/mfa/enroll", enrollToken, nil, ip)
	if enrollRec.Code != http.StatusOK {
		t.Fatalf("MFA enroll failed: status=%d body=%s", enrollRec.Code, enrollRec.Body.String())
	}
	var enrollResp map[string]string
	if err := json.Unmarshal(enrollRec.Body.Bytes(), &enrollResp); err != nil {
		t.Fatalf("failed to decode enroll response: %v", err)
	}
	secret = enrollResp["secret"]

	code, err := engines.GenerateTOTPCode(secret)
	if err != nil {
		t.Fatalf("failed to compute TOTP code: %v", err)
	}
	activateRec := doRequestFromIP(t, apiMiddleware(handleMFAActivate), "POST", "/api/v1/auth/mfa/activate", enrollToken, map[string]string{"code": code}, ip)
	if activateRec.Code != http.StatusOK {
		t.Fatalf("MFA activate failed: status=%d body=%s", activateRec.Code, activateRec.Body.String())
	}
	var activateResp struct {
		Token         string   `json:"token"`
		RecoveryCodes []string `json:"recovery_codes"`
	}
	if err := json.Unmarshal(activateRec.Body.Bytes(), &activateResp); err != nil {
		t.Fatalf("failed to decode activate response: %v", err)
	}
	if activateResp.Token == "" {
		t.Fatalf("activation returned no session token: %s", activateRec.Body.String())
	}
	return secret, activateResp.RecoveryCodes, activateResp.Token
}

// challengeTokenFor logs in an already-enrolled user and returns the
// short-lived mfa_challenge token that /auth/mfa/verify requires.
func challengeTokenFor(t *testing.T, user, pw, ip string) string {
	t.Helper()
	loginRec := doRequestFromIP(t, apiMiddleware(handleLogin), "POST", "/api/v1/login", "", map[string]string{
		"username": user, "password": pw,
	}, ip)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login failed: status=%d body=%s", loginRec.Code, loginRec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(loginRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode login response: %v", err)
	}
	tok, _ := resp["challenge_token"].(string)
	if tok == "" {
		t.Fatalf("expected a challenge token for an enrolled user, got: %s", loginRec.Body.String())
	}
	return tok
}

// TestMFARecoveryCodeLogin is the core regression test: a recovery code gets
// you in when the authenticator is gone, and it works exactly once.
func TestMFARecoveryCodeLogin(t *testing.T) {
	db.InitDB(testConnStr())

	ip := mfaTestIP()
	const testUser = "__mfarecoverytest_user__"
	pw, cleanup := seedMFATestUser(t, testUser)
	defer cleanup()

	_, codes, _ := enrollMFATestUser(t, testUser, pw, ip)
	if len(codes) != engines.RecoveryCodeCount {
		t.Fatalf("expected %d recovery codes at enrollment, got %d", engines.RecoveryCodeCount, len(codes))
	}

	// 1. A recovery code is accepted in place of a TOTP code.
	verifyRec := doRequest(t, apiMiddleware(handleMFAVerify), "POST", "/api/v1/auth/mfa/verify",
		challengeTokenFor(t, testUser, pw, ip), map[string]string{"code": codes[0]})
	if verifyRec.Code != http.StatusOK {
		t.Fatalf("recovery code was rejected: status=%d body=%s", verifyRec.Code, verifyRec.Body.String())
	}
	var verifyResp struct {
		Token            string `json:"token"`
		UsedRecoveryCode bool   `json:"used_recovery_code"`
		CodesRemaining   int    `json:"recovery_codes_remaining"`
	}
	if err := json.Unmarshal(verifyRec.Body.Bytes(), &verifyResp); err != nil {
		t.Fatalf("failed to decode verify response: %v", err)
	}
	if verifyResp.Token == "" {
		t.Fatalf("recovery-code login returned no session token")
	}
	if !verifyResp.UsedRecoveryCode {
		t.Errorf("expected used_recovery_code=true so the UI can prompt for a new device")
	}
	if verifyResp.CodesRemaining != engines.RecoveryCodeCount-1 {
		t.Errorf("expected %d codes remaining, got %d", engines.RecoveryCodeCount-1, verifyResp.CodesRemaining)
	}

	// 2. The same code must not work twice - this is the property that stops a
	// screenshot of the code list from being a permanent standing credential.
	replayRec := doRequest(t, apiMiddleware(handleMFAVerify), "POST", "/api/v1/auth/mfa/verify",
		challengeTokenFor(t, testUser, pw, ip), map[string]string{"code": codes[0]})
	if replayRec.Code == http.StatusOK {
		t.Fatalf("a spent recovery code was accepted a second time: %s", replayRec.Body.String())
	}

	// 3. A code that was never issued is rejected.
	bogusRec := doRequest(t, apiMiddleware(handleMFAVerify), "POST", "/api/v1/auth/mfa/verify",
		challengeTokenFor(t, testUser, pw, ip), map[string]string{"code": "ZZZZZ-ZZZZZ"})
	if bogusRec.Code == http.StatusOK {
		t.Fatalf("an unissued recovery code was accepted: %s", bogusRec.Body.String())
	}

	// 4. A second, untouched code still works - one redemption must not
	// invalidate the rest of the set.
	secondRec := doRequest(t, apiMiddleware(handleMFAVerify), "POST", "/api/v1/auth/mfa/verify",
		challengeTokenFor(t, testUser, pw, ip), map[string]string{"code": codes[1]})
	if secondRec.Code != http.StatusOK {
		t.Fatalf("second recovery code was rejected: status=%d body=%s", secondRec.Code, secondRec.Body.String())
	}
}

// TestMFARecoveryCodeNormalization covers the typed-by-hand path: these codes
// are read off paper, so case and the display dash must not matter.
func TestMFARecoveryCodeNormalization(t *testing.T) {
	db.InitDB(testConnStr())

	ip := mfaTestIP()
	const testUser = "__mfanormalizetest_user__"
	pw, cleanup := seedMFATestUser(t, testUser)
	defer cleanup()

	_, codes, _ := enrollMFATestUser(t, testUser, pw, ip)
	if len(codes) == 0 {
		t.Fatalf("no recovery codes issued")
	}

	// Lower-cased, dash stripped, padded with spaces - all the ways a code
	// comes back off a printed card.
	mangled := "  " + strings.ToLower(strings.ReplaceAll(codes[0], "-", "")) + "  "
	rec := doRequest(t, apiMiddleware(handleMFAVerify), "POST", "/api/v1/auth/mfa/verify",
		challengeTokenFor(t, testUser, pw, ip), map[string]string{"code": mangled})
	if rec.Code != http.StatusOK {
		t.Fatalf("a correctly-typed-but-differently-formatted recovery code was rejected: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestMFAReenrollMovesAuthenticator covers the "my phone was replaced" flow,
// including the property that matters most: the old authenticator keeps
// working until the new one is confirmed, so an abandoned attempt cannot
// itself cause the lockout this stage exists to prevent.
func TestMFAReenrollMovesAuthenticator(t *testing.T) {
	db.InitDB(testConnStr())

	ip := mfaTestIP()
	const testUser = "__mfareenrolltest_user__"
	pw, cleanup := seedMFATestUser(t, testUser)
	defer cleanup()

	oldSecret, _, _ := enrollMFATestUser(t, testUser, pw, ip)

	// A full session token, which is what the /me/mfa/* endpoints take (unlike
	// the login-time handlers, which run on purpose tokens).
	sessionToken := engines.SignToken(testUser, testUser, "HR/Admin", "default", "HO")

	// 1. Start the device change.
	startRec := doRequest(t, apiMiddleware(handleMFAReenrollStart), "POST", "/api/v1/me/mfa/reenroll", sessionToken,
		map[string]string{"password": pw})
	if startRec.Code != http.StatusOK {
		t.Fatalf("reenroll start failed: status=%d body=%s", startRec.Code, startRec.Body.String())
	}
	var startResp map[string]string
	if err := json.Unmarshal(startRec.Body.Bytes(), &startResp); err != nil {
		t.Fatalf("failed to decode reenroll response: %v", err)
	}
	newSecret := startResp["secret"]
	if newSecret == "" || newSecret == oldSecret {
		t.Fatalf("expected a fresh secret distinct from the active one")
	}

	// 2. Mid-flight, the OLD authenticator must still work. This is the whole
	// reason the new secret parks in a separate column.
	oldCode, err := engines.GenerateTOTPCode(oldSecret)
	if err != nil {
		t.Fatalf("failed to compute TOTP code: %v", err)
	}
	midRec := doRequest(t, apiMiddleware(handleMFAVerify), "POST", "/api/v1/auth/mfa/verify",
		challengeTokenFor(t, testUser, pw, ip), map[string]string{"code": oldCode})
	if midRec.Code != http.StatusOK {
		t.Fatalf("the existing authenticator stopped working mid-reenrollment: status=%d body=%s", midRec.Code, midRec.Body.String())
	}

	// 3. Confirm with a code from the new device.
	newCode, err := engines.GenerateTOTPCode(newSecret)
	if err != nil {
		t.Fatalf("failed to compute TOTP code: %v", err)
	}
	confirmRec := doRequest(t, apiMiddleware(handleMFAReenrollConfirm), "POST", "/api/v1/me/mfa/reenroll/confirm", sessionToken,
		map[string]string{"code": newCode})
	if confirmRec.Code != http.StatusOK {
		t.Fatalf("reenroll confirm failed: status=%d body=%s", confirmRec.Code, confirmRec.Body.String())
	}
	var confirmResp struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	if err := json.Unmarshal(confirmRec.Body.Bytes(), &confirmResp); err != nil {
		t.Fatalf("failed to decode confirm response: %v", err)
	}
	if len(confirmResp.RecoveryCodes) != engines.RecoveryCodeCount {
		t.Errorf("expected a fresh set of %d recovery codes after a device change, got %d",
			engines.RecoveryCodeCount, len(confirmResp.RecoveryCodes))
	}

	// 4. The new device now works and the old one does not.
	newCode2, _ := engines.GenerateTOTPCode(newSecret)
	afterRec := doRequest(t, apiMiddleware(handleMFAVerify), "POST", "/api/v1/auth/mfa/verify",
		challengeTokenFor(t, testUser, pw, ip), map[string]string{"code": newCode2})
	if afterRec.Code != http.StatusOK {
		t.Fatalf("the new authenticator does not work after confirmation: status=%d body=%s", afterRec.Code, afterRec.Body.String())
	}
	oldCode2, _ := engines.GenerateTOTPCode(oldSecret)
	if oldCode2 != newCode2 { // guard against the astronomically unlikely collision
		staleRec := doRequest(t, apiMiddleware(handleMFAVerify), "POST", "/api/v1/auth/mfa/verify",
			challengeTokenFor(t, testUser, pw, ip), map[string]string{"code": oldCode2})
		if staleRec.Code == http.StatusOK {
			t.Errorf("the replaced authenticator still works after a device change")
		}
	}
}

// TestAdminResetUserMFA covers the last-resort path for a colleague who lost
// both their phone and their codes: an admin puts the account back into the
// enrollment state, without weakening the MFA requirement itself.
func TestAdminResetUserMFA(t *testing.T) {
	db.InitDB(testConnStr())

	ip := mfaTestIP()
	const targetUser = "__mfaresettarget_user__"
	pw, cleanup := seedMFATestUser(t, targetUser)
	defer cleanup()
	enrollMFATestUser(t, targetUser, pw, ip)

	// The acting admin is a real, enrolled account driven through the real
	// login chain - a hand-signed token for a user row that does not exist is
	// rejected by apiMiddleware as an expired session.
	const adminUser = "__mfaresetadmin_user__"
	adminIP := mfaTestIP()
	adminPw, adminCleanup := seedMFATestUser(t, adminUser)
	defer adminCleanup()
	_, _, adminToken := enrollMFATestUser(t, adminUser, adminPw, adminIP)

	resetRec := doRequest(t, apiMiddleware(handleAdminResetUserMFA), "POST", "/api/v1/admin/users/reset-mfa", adminToken,
		map[string]string{"id": targetUser})
	if resetRec.Code != http.StatusOK {
		t.Fatalf("admin MFA reset failed: status=%d body=%s", resetRec.Code, resetRec.Body.String())
	}

	// The next login must ask for enrollment again, not hand out a session.
	loginRec := doRequestFromIP(t, apiMiddleware(handleLogin), "POST", "/api/v1/login", "", map[string]string{
		"username": targetUser, "password": pw,
	}, ip)
	var loginResp map[string]interface{}
	if err := json.Unmarshal(loginRec.Body.Bytes(), &loginResp); err != nil {
		t.Fatalf("failed to decode login response: %v", err)
	}
	if _, ok := loginResp["enrollment_token"]; !ok {
		t.Fatalf("after an admin reset, login should require re-enrollment; got: %s", loginRec.Body.String())
	}
	if _, ok := loginResp["token"]; ok {
		t.Fatalf("after an admin reset, login must not issue a session token directly: %s", loginRec.Body.String())
	}

	// The old recovery codes must be gone too - otherwise the reset would
	// leave a working credential behind.
	n, err := engines.CountUnusedRecoveryCodes("default", targetUser)
	if err != nil {
		t.Fatalf("failed to count recovery codes: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 recovery codes after an admin reset, got %d", n)
	}
}
