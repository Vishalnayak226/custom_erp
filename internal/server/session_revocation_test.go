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

func seedSessionRevocationTestUser(t *testing.T) (userID, password string, cleanup func()) {
	t.Helper()
	suffix := make([]byte, 6)
	_, _ = rand.Read(suffix)
	userID = "__cvrevoke_" + hex.EncodeToString(suffix) + "__"
	password = randomTestPassword()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash seed password: %v", err)
	}
	if _, err := db.DB.Exec(
		`INSERT INTO tenant_default.users (id, username, password_hash, email, role, status, location_code)
		 VALUES ($1, $1, $2, $3, 'Cashier', 'Active', 'HO')`,
		userID, string(hash), userID+"@cvrevoke.invalid"); err != nil {
		t.Fatalf("seed session-revocation test user: %v", err)
	}
	return userID, password, func() {
		db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, userID)
		engines.ResetLiveUserStateCache()
	}
}

// Stage 49.2.2/49.2.4: a self-service password change must revoke every
// OTHER session on the account immediately (not just once the ~30s
// live-state cache window happens to expire), while the token the change
// itself returns keeps working. Exercised at the real HTTP handler, not the
// engine function directly, because the claim under test - "the OLD bearer
// token now gets rejected" - lives entirely in apiMiddleware's live-state
// re-check (middleware.go), not in engines.ValidatePasswordStrength or the
// UPDATE statement.
func TestChangePasswordRevokesOtherSessionsButNotItsOwnToken(t *testing.T) {
	db.InitDB(testConnStr())
	userID, oldPassword, cleanup := seedSessionRevocationTestUser(t)
	defer cleanup()
	engines.ResetLiveUserStateCache()

	probe := apiMiddleware(handleGetDocTypes)
	changePW := apiMiddleware(handleChangePassword)

	oldToken := engines.SignToken(userID, userID, "Cashier", "default", "HO", 1)
	if rec := doRequest(t, probe, http.MethodGet, "/api/v1/meta/doctypes", oldToken, nil); rec.Code != http.StatusOK {
		t.Fatalf("baseline: token should work before any password change, status=%d body=%s", rec.Code, rec.Body.String())
	}

	newPassword := randomTestPassword()
	changeRec := doRequest(t, changePW, http.MethodPost, "/api/v1/me/change-password", oldToken, map[string]string{
		"current_password": oldPassword,
		"new_password":     newPassword,
	})
	if changeRec.Code != http.StatusOK {
		t.Fatalf("change-password should succeed, status=%d body=%s", changeRec.Code, changeRec.Body.String())
	}
	var changeResp struct {
		Status string `json:"status"`
		Token  string `json:"token"`
	}
	if err := json.Unmarshal(changeRec.Body.Bytes(), &changeResp); err != nil {
		t.Fatalf("unmarshal change-password response: %v", err)
	}
	if changeResp.Token == "" {
		t.Fatal("change-password must return a fresh token so the caller's own session survives the change")
	}

	if rec := doRequest(t, probe, http.MethodGet, "/api/v1/meta/doctypes", oldToken, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("the pre-change token must be rejected once the password has changed, status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := doRequest(t, probe, http.MethodGet, "/api/v1/meta/doctypes", changeResp.Token, nil); rec.Code != http.StatusOK {
		t.Errorf("the token change-password itself just issued must keep working, status=%d body=%s", rec.Code, rec.Body.String())
	}

	// The old password must genuinely stop working too (not just its old
	// token) - otherwise "changed" only rotated a claim, not a credential.
	var storedHash string
	if err := db.DB.QueryRow(`SELECT password_hash FROM tenant_default.users WHERE id = $1`, userID).Scan(&storedHash); err != nil {
		t.Fatalf("read post-change hash: %v", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(oldPassword)) == nil {
		t.Error("the old password must no longer verify against the stored hash")
	}
	if bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(newPassword)) != nil {
		t.Error("the new password must verify against the stored hash")
	}
}

// A weak new password must be refused before anything is mutated - the
// current session, the stored hash and credential_version are all
// untouched, and the account is not left in a half-changed state.
func TestChangePasswordRejectsWeakPasswordWithoutMutatingAnything(t *testing.T) {
	db.InitDB(testConnStr())
	userID, oldPassword, cleanup := seedSessionRevocationTestUser(t)
	defer cleanup()
	engines.ResetLiveUserStateCache()

	var before int
	if err := db.DB.QueryRow(`SELECT credential_version FROM tenant_default.users WHERE id = $1`, userID).Scan(&before); err != nil {
		t.Fatalf("read initial credential_version: %v", err)
	}

	token := engines.SignToken(userID, userID, "Cashier", "default", "HO", 1)
	changePW := apiMiddleware(handleChangePassword)

	rec := doRequest(t, changePW, http.MethodPost, "/api/v1/me/change-password", token, map[string]string{
		"current_password": oldPassword,
		"new_password":     "qwertyuiop123",
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a common/denylisted new password must be refused, status=%d body=%s", rec.Code, rec.Body.String())
	}

	var after int
	if err := db.DB.QueryRow(`SELECT credential_version FROM tenant_default.users WHERE id = $1`, userID).Scan(&after); err != nil {
		t.Fatalf("read post-attempt credential_version: %v", err)
	}
	if after != before {
		t.Errorf("a rejected password change must not bump credential_version, got %d -> %d", before, after)
	}

	probe := apiMiddleware(handleGetDocTypes)
	if rec := doRequest(t, probe, http.MethodGet, "/api/v1/meta/doctypes", token, nil); rec.Code != http.StatusOK {
		t.Errorf("the existing session must still work after a rejected change attempt, status=%d body=%s", rec.Code, rec.Body.String())
	}
}
