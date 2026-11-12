package server

import (
	"net/http"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"
)

// TestLogoutEndsTheSessionOnTheServer (162.15): after POST /auth/logout the
// same token is refused - a copy of it included - while the user's other
// session carries on.
func TestLogoutEndsTheSessionOnTheServer(t *testing.T) {
	db.InitDB(testConnStr())
	userID, _, clean := seedSecurityOpsUser(t, "Cashier")
	defer clean()
	t.Cleanup(func() { db.DB.Exec(`DELETE FROM tenant_default.user_sessions WHERE user_id = $1`, userID) })
	engines.ResetLiveUserStateCache()

	tokenA := engines.SignToken(userID, userID, "Cashier", "default", "HO", 1)
	tokenB := engines.SignToken(userID, userID, "Cashier", "default", "HO", 1)
	probe := apiMiddleware(handleGetDocTypes)
	logout := apiMiddleware(handleLogout)
	for _, tok := range []string{tokenA, tokenB} {
		if rec := doRequest(t, probe, http.MethodGet, "/api/v1/meta/doctypes", tok, nil); rec.Code != http.StatusOK {
			t.Fatalf("before sign-out: %d", rec.Code)
		}
	}
	if rec := doRequest(t, logout, http.MethodPost, "/api/v1/auth/logout", tokenA, nil); rec.Code != http.StatusOK {
		t.Fatalf("logout: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doRequest(t, probe, http.MethodGet, "/api/v1/meta/doctypes", tokenA, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("the signed-out token still works: %d", rec.Code)
	}
	if rec := doRequest(t, probe, http.MethodGet, "/api/v1/meta/doctypes", tokenB, nil); rec.Code != http.StatusOK {
		t.Fatalf("the user's other session must carry on: %d", rec.Code)
	}
	// Signing out again with the dead token is refused, not an error loop.
	if rec := doRequest(t, logout, http.MethodPost, "/api/v1/auth/logout", tokenA, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a second sign-out with the same token: %d", rec.Code)
	}
}
