package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"
)

// Stage 49.11.7: a privileged sign-in leaves a session row; a cashier's does
// not; ending the row refuses that token at its next request while the same
// user's other token carries on; sessions the user signs out everywhere show
// as ended.
func TestStage49117PrivilegedSessionsListAndEnd(t *testing.T) {
	db.InitDB(testConnStr())
	adminID, adminPW, cleanAdmin := seedSecurityOpsUser(t, "Super Admin")
	defer cleanAdmin()
	cashierID, _, cleanCashier := seedSecurityOpsUser(t, "Cashier")
	defer cleanCashier()
	t.Cleanup(func() {
		db.DB.Exec(`DELETE FROM tenant_default.privileged_sessions WHERE user_id IN ($1, $2)`, adminID, cashierID)
	})
	engines.ResetLiveUserStateCache()
	ctx := engines.WithRequestSource(context.Background(), "203.0.113.9", "Mozilla/5.0 (Windows NT 10.0) Chrome/120.0 Safari/537.36")

	tokenA := engines.SignToken(adminID, adminID, "Super Admin", "default", "HO", 1)
	tokenB := engines.SignToken(adminID, adminID, "Super Admin", "default", "HO", 1)
	tokenC := engines.SignToken(cashierID, cashierID, "Cashier", "default", "HO", 1)
	for _, c := range []struct{ id, role, tok string }{{adminID, "Super Admin", tokenA}, {adminID, "Super Admin", tokenB}, {cashierID, "Cashier", tokenC}} {
		engines.RecordPrivilegedSession(ctx, "default", c.id, c.id, c.role, c.tok, 1)
	}
	var n int
	db.DB.QueryRow(`SELECT count(*) FROM tenant_default.privileged_sessions WHERE user_id = $1`, cashierID).Scan(&n)
	if n != 0 {
		t.Fatalf("a cashier's sign-in made %d privileged session row(s)", n)
	}

	list := func() []engines.PrivilegedSession {
		rec := doRequest(t, apiMiddleware(handleListPrivilegedSessions), http.MethodGet, "/api/v1/admin/security/sessions", tokenB, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
		}
		var out struct {
			Sessions []engines.PrivilegedSession `json:"sessions"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out.Sessions
	}
	var idA string
	mine := 0
	for _, s := range list() {
		if s.UserID == adminID {
			mine++
			if s.State != "Active" || s.Address != "203.0.113.9" || s.Device == "" {
				t.Fatalf("row %+v", s)
			}
			if tokenIDMatches(tokenA, s.ID) {
				idA = s.ID
			}
		}
	}
	if mine != 2 || idA == "" {
		t.Fatalf("expected 2 sessions for the admin, got %d (A found: %v)", mine, idA != "")
	}

	probe := apiMiddleware(handleGetDocTypes)
	end := apiMiddleware(handleEndPrivilegedSession)
	if rec := doRequest(t, end, http.MethodPost, "/api/v1/admin/security/sessions/end", tokenB,
		map[string]string{"id": idA, "reason": "left open", "password": "wrong"}); rec.Code == http.StatusOK {
		t.Fatal("ending a session needs the administrator's own password")
	}
	if rec := doRequest(t, probe, http.MethodGet, "/api/v1/meta/doctypes", tokenA, nil); rec.Code != http.StatusOK {
		t.Fatalf("before ending: %d", rec.Code)
	}
	if rec := doRequest(t, end, http.MethodPost, "/api/v1/admin/security/sessions/end", tokenB,
		map[string]string{"id": idA, "reason": "left open", "password": adminPW}); rec.Code != http.StatusOK {
		t.Fatalf("end: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doRequest(t, probe, http.MethodGet, "/api/v1/meta/doctypes", tokenA, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("the ended token still works: %d", rec.Code)
	}
	if rec := doRequest(t, probe, http.MethodGet, "/api/v1/meta/doctypes", tokenB, nil); rec.Code != http.StatusOK {
		t.Fatalf("the user's other session must carry on: %d", rec.Code)
	}
	ended := 0
	for _, s := range list() {
		if s.ID == idA && s.State == "Ended" {
			ended++
		}
	}
	if ended != 1 {
		t.Fatal("the list does not show the ended session as Ended")
	}
	// Signing the user out everywhere shows the rest as signed out.
	if _, err := engines.SignOutUserEverywhere("default", adminID, "qa", "drill for the list"); err != nil {
		t.Fatal(err)
	}
	for _, s := range list2(t, adminID) {
		if s.State == "Active" {
			t.Fatalf("a session survived sign-out everywhere: %+v", s)
		}
	}
}

func tokenIDMatches(token, id string) bool {
	claims, err := engines.ParseToken(token)
	return err == nil && claims["jti"] == id
}

func list2(t *testing.T, userID string) []engines.PrivilegedSession {
	t.Helper()
	all, err := engines.ListPrivilegedSessions("default")
	if err != nil {
		t.Fatal(err)
	}
	var out []engines.PrivilegedSession
	for _, s := range all {
		if s.UserID == userID {
			out = append(out, s)
		}
	}
	return out
}
