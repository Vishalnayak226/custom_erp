package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"

	"golang.org/x/crypto/bcrypt"
)

// seedAdminPasswordResetTestUser mirrors seedSessionRevocationTestUser
// (stage49_2_2_session_revocation_test.go) but with a caller-chosen role, so
// these tests can seed both an ordinary target and a privileged one.
func seedAdminPasswordResetTestUser(t *testing.T, role string) (userID, password string, cleanup func()) {
	t.Helper()
	suffix := make([]byte, 6)
	_, _ = rand.Read(suffix)
	userID = "__pwdreset_" + hex.EncodeToString(suffix) + "__"
	password = randomTestPassword()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash seed password: %v", err)
	}
	if _, err := db.DB.Exec(
		`INSERT INTO tenant_default.users (id, username, password_hash, email, role, status, location_code)
		 VALUES ($1, $1, $2, $3, $4, 'Active', 'HO')`,
		userID, string(hash), userID+"@pwdreset.invalid", role); err != nil {
		t.Fatalf("seed admin-password-reset test user: %v", err)
	}
	return userID, password, func() {
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE doctype = 'PasswordResetRequest' AND data::text LIKE '%'||$1||'%'`, userID)
		db.DB.Exec(`DELETE FROM tenant_default.approval_log WHERE doctype = 'PasswordResetRequest' AND document_id IN
			(SELECT id FROM tenant_default.documents WHERE doctype = 'PasswordResetRequest' AND data::text LIKE '%'||$1||'%')`, userID)
		db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, userID)
		engines.ResetLiveUserStateCache()
	}
}

func storedPasswordHash(t *testing.T, userID string) string {
	t.Helper()
	var hash string
	if err := db.DB.QueryRow(`SELECT password_hash FROM tenant_default.users WHERE id = $1`, userID).Scan(&hash); err != nil {
		t.Fatalf("read stored password hash for %s: %v", userID, err)
	}
	return hash
}

// TestAdminPasswordResetImmediateForOrdinaryTarget covers the gap the
// 2026-09-08 handover note names explicitly: before this there was no
// admin-driven way to reset another user's password at all. A Cashier
// target is the common case and must be reset in one call, with the
// one-time password returned right there and every existing session on the
// account revoked.
func TestAdminPasswordResetImmediateForOrdinaryTarget(t *testing.T) {
	db.InitDB(testConnStr())
	engines.ResetLiveUserStateCache()

	actorID, _, actorCleanup := seedAdminPasswordResetTestUser(t, "Super Admin")
	defer actorCleanup()
	targetID, oldPassword, targetCleanup := seedAdminPasswordResetTestUser(t, "Cashier")
	defer targetCleanup()

	// A session opened under the target's OLD credential_version must not
	// survive the reset - the same claim 49.2.4's self-service change/reset
	// already prove for their own paths.
	oldToken := engines.SignToken(targetID, targetID, "Cashier", "default", "HO", 1)
	probe := apiMiddleware(handleGetDocTypes)
	if rec := doRequest(t, probe, http.MethodGet, "/api/v1/meta/doctypes", oldToken, nil); rec.Code != http.StatusOK {
		t.Fatalf("baseline: target's token should work before any reset, status=%d body=%s", rec.Code, rec.Body.String())
	}

	actorToken := engines.SignToken(actorID, actorID, "Super Admin", "default", "HO", 1)
	resetHandler := apiMiddleware(handleRequestPasswordReset)
	rec := doRequest(t, resetHandler, http.MethodPost, "/api/v1/admin/users/reset-password", actorToken,
		map[string]string{"id": targetID})
	if rec.Code != http.StatusOK {
		t.Fatalf("admin password reset for an ordinary target should succeed immediately, status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Status       string `json:"status"`
		TempPassword string `json:"temp_password"`
		Detail       string `json:"detail"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "success" {
		t.Errorf("expected status=success for an ordinary target, got %q", resp.Status)
	}
	if resp.TempPassword == "" {
		t.Fatal("expected a one-time password in the response")
	}
	if len(resp.TempPassword) < 12 {
		t.Errorf("temp password %q looks too short to be the generated one-time credential", resp.TempPassword)
	}

	newHash := storedPasswordHash(t, targetID)
	if bcrypt.CompareHashAndPassword([]byte(newHash), []byte(oldPassword)) == nil {
		t.Error("the old password must no longer verify against the stored hash")
	}
	if bcrypt.CompareHashAndPassword([]byte(newHash), []byte(resp.TempPassword)) != nil {
		t.Error("the returned one-time password must verify against the stored hash")
	}

	if rec := doRequest(t, probe, http.MethodGet, "/api/v1/meta/doctypes", oldToken, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("the target's pre-reset session must be revoked, status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestAdminPasswordResetRequiresDualControlForPrivilegedTarget is the other
// half of the still-open 49.2.4 clause: a Super Admin target must not be
// resettable by a single admin acting alone. The requesting admin cannot
// approve their own request (maker-checker), and the target's password must
// stay untouched until a SECOND, different Super Admin approves it - at
// which point only that approver, never the requester, receives the
// generated password.
func TestAdminPasswordResetRequiresDualControlForPrivilegedTarget(t *testing.T) {
	db.InitDB(testConnStr())
	engines.ResetLiveUserStateCache()

	actorID, _, actorCleanup := seedAdminPasswordResetTestUser(t, "Super Admin")
	defer actorCleanup()
	approverID, _, approverCleanup := seedAdminPasswordResetTestUser(t, "Super Admin")
	defer approverCleanup()
	targetID, oldPassword, targetCleanup := seedAdminPasswordResetTestUser(t, "Super Admin")
	defer targetCleanup()

	actorToken := engines.SignToken(actorID, actorID, "Super Admin", "default", "HO", 1)
	resetHandler := apiMiddleware(handleRequestPasswordReset)

	// A reason is mandatory once the target is privileged.
	blankReasonRec := doRequest(t, resetHandler, http.MethodPost, "/api/v1/admin/users/reset-password", actorToken,
		map[string]string{"id": targetID})
	if blankReasonRec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a blank reason against a privileged target must be refused, status=%d body=%s", blankReasonRec.Code, blankReasonRec.Body.String())
	}

	rec := doRequest(t, resetHandler, http.MethodPost, "/api/v1/admin/users/reset-password", actorToken,
		map[string]string{"id": targetID, "reason": "colleague locked out, verified by phone"})
	if rec.Code != http.StatusOK {
		t.Fatalf("a reasoned request against a privileged target should be accepted, status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Status       string `json:"status"`
		DocumentID   string `json:"document_id"`
		TempPassword string `json:"temp_password"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "pending_approval" {
		t.Fatalf("a privileged target must route to pending_approval, got %q", resp.Status)
	}
	if resp.DocumentID == "" {
		t.Fatal("expected a document_id for the pending request")
	}
	if resp.TempPassword != "" {
		t.Error("the requester must never receive the one-time password - only the approver does")
	}

	// Nothing must have changed yet.
	untouchedHash := storedPasswordHash(t, targetID)
	if bcrypt.CompareHashAndPassword([]byte(untouchedHash), []byte(oldPassword)) != nil {
		t.Error("the target's password must be untouched while the request is still pending")
	}

	decide := apiMiddleware(handleDecideApproval)

	// The requester cannot approve their own request.
	selfDecideRec := doRequest(t, decide, http.MethodPost, "/api/v1/approval/decide", actorToken,
		map[string]string{"doctype": "PasswordResetRequest", "document_id": resp.DocumentID, "decision": "Approved"})
	if selfDecideRec.Code == http.StatusOK {
		t.Fatalf("maker-checker violation: the requester must not be able to approve their own request, status=%d body=%s",
			selfDecideRec.Code, selfDecideRec.Body.String())
	}
	stillUntouchedHash := storedPasswordHash(t, targetID)
	if bcrypt.CompareHashAndPassword([]byte(stillUntouchedHash), []byte(oldPassword)) != nil {
		t.Error("a rejected self-approval attempt must not have mutated the target's password")
	}

	// A second, different Super Admin approves - and only they see the result.
	approverToken := engines.SignToken(approverID, approverID, "Super Admin", "default", "HO", 1)
	approveRec := doRequest(t, decide, http.MethodPost, "/api/v1/approval/decide", approverToken,
		map[string]string{"doctype": "PasswordResetRequest", "document_id": resp.DocumentID, "decision": "Approved"})
	if approveRec.Code != http.StatusOK {
		t.Fatalf("a different Super Admin's approval should succeed, status=%d body=%s", approveRec.Code, approveRec.Body.String())
	}
	var approveResp struct {
		TempPassword string `json:"temp_password"`
	}
	if err := json.Unmarshal(approveRec.Body.Bytes(), &approveResp); err != nil {
		t.Fatalf("decode approve response: %v", err)
	}
	if approveResp.TempPassword == "" {
		t.Fatal("the approver's decide response must carry the generated one-time password")
	}

	finalHash := storedPasswordHash(t, targetID)
	if bcrypt.CompareHashAndPassword([]byte(finalHash), []byte(oldPassword)) == nil {
		t.Error("the old password must no longer verify after approval")
	}
	if bcrypt.CompareHashAndPassword([]byte(finalHash), []byte(approveResp.TempPassword)) != nil {
		t.Error("the approver's one-time password must verify against the stored hash after approval")
	}
}

// An admin must not be able to use this endpoint to reset their own
// password - Change Password on the Profile screen is the self-service path
// and already has its own reauthentication.
func TestAdminPasswordResetRejectsSelfTarget(t *testing.T) {
	db.InitDB(testConnStr())
	engines.ResetLiveUserStateCache()

	actorID, _, cleanup := seedAdminPasswordResetTestUser(t, "Super Admin")
	defer cleanup()

	actorToken := engines.SignToken(actorID, actorID, "Super Admin", "default", "HO", 1)
	resetHandler := apiMiddleware(handleRequestPasswordReset)
	rec := doRequest(t, resetHandler, http.MethodPost, "/api/v1/admin/users/reset-password", actorToken,
		map[string]string{"id": actorID})
	if rec.Code == http.StatusOK {
		t.Fatalf("resetting one's own password through the admin endpoint must be refused, status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestUpdateProfileRequiresReauthToChangeEmail closes the other 49.2.4
// clause: a hijacked session must not be able to silently redirect the
// account's recovery destination. Changing idle-timeout alone (no email
// change) must not require a password at all.
func TestUpdateProfileRequiresReauthToChangeEmail(t *testing.T) {
	db.InitDB(testConnStr())
	engines.ResetLiveUserStateCache()

	userID, password, cleanup := seedAdminPasswordResetTestUser(t, "Cashier")
	defer cleanup()
	token := engines.SignToken(userID, userID, "Cashier", "default", "HO", 1)
	updateProfile := apiMiddleware(handleUpdateProfile)

	// No email change: current_password may be absent entirely.
	noopRec := doRequest(t, updateProfile, http.MethodPut, "/api/v1/me", token,
		map[string]interface{}{"idle_timeout_minutes": 30})
	if noopRec.Code != http.StatusOK {
		t.Fatalf("an idle-timeout-only update must not require reauthentication, status=%d body=%s", noopRec.Code, noopRec.Body.String())
	}

	// Changing the email with no/wrong current_password must be refused.
	wrongPwRec := doRequest(t, updateProfile, http.MethodPut, "/api/v1/me", token,
		map[string]interface{}{"email": "attacker-controlled@evil.invalid", "current_password": "not-the-password"})
	if wrongPwRec.Code != http.StatusUnauthorized {
		t.Fatalf("an email change with the wrong current_password must be refused, status=%d body=%s", wrongPwRec.Code, wrongPwRec.Body.String())
	}
	var stillOldEmail string
	if err := db.DB.QueryRow(`SELECT COALESCE(email, '') FROM tenant_default.users WHERE id = $1`, userID).Scan(&stillOldEmail); err != nil {
		t.Fatalf("read email after refused change: %v", err)
	}
	if stillOldEmail == "attacker-controlled@evil.invalid" {
		t.Fatal("the email must not have changed when reauthentication failed")
	}

	// The correct current_password succeeds.
	okRec := doRequest(t, updateProfile, http.MethodPut, "/api/v1/me", token,
		map[string]interface{}{"email": "legit-new-address@example.invalid", "current_password": password})
	if okRec.Code != http.StatusOK {
		t.Fatalf("an email change with the correct current_password should succeed, status=%d body=%s", okRec.Code, okRec.Body.String())
	}
	var newEmail string
	if err := db.DB.QueryRow(`SELECT COALESCE(email, '') FROM tenant_default.users WHERE id = $1`, userID).Scan(&newEmail); err != nil {
		t.Fatalf("read email after successful change: %v", err)
	}
	if newEmail != "legit-new-address@example.invalid" {
		t.Errorf("expected the email to be updated, got %q", newEmail)
	}
}
