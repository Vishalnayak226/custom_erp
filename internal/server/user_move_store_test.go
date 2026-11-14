package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"testing"
)

// 164.3 / 49.2.1: the Users screen's home-store control posts to
// /admin/users/location. With security.identity_change_approval Off the store
// changes at once; On, it files an AccessChangeRequest mover request and the
// user keeps the old store. A wrong role and a wrong tenant are refused.
func TestUserMoveStoreBothSettingStates(t *testing.T) {
	db.InitDB(testConnStr())
	target, cleanT := seedStage47User(t, engines.RoleCashier, "HO")
	defer cleanT()
	mgr, cleanM := seedStage47User(t, engines.RoleStoreManager, "HO")
	defer cleanM()
	defer db.DB.Exec(`DELETE FROM tenant_default.system_settings WHERE key = $1`, engines.SettingIdentityChangeApproval)
	defer db.DB.Exec(`DELETE FROM tenant_default.documents WHERE doctype = 'AccessChangeRequest' AND data->>'target_user_id' = $1`, target)
	adminTok := engines.SignToken("admin", "admin", engines.RoleSuperAdmin, "default", "", currentCredentialVersion("admin"))
	h := apiMiddleware(handleSetUserLocation)
	home := func() string {
		var l string
		db.DB.QueryRow(`SELECT COALESCE(location_code,'') FROM tenant_default.users WHERE id = $1`, target).Scan(&l)
		return l
	}
	move := func(tok, store string) (int, map[string]string) {
		rec := doRequest(t, h, http.MethodPost, "/api/v1/admin/users/location", tok, map[string]string{"id": target, "location_code": store})
		var out map[string]string
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	requests := func() int {
		var n int
		db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.documents WHERE doctype = 'AccessChangeRequest' AND data->>'target_user_id' = $1`, target).Scan(&n)
		return n
	}

	// Setting Off: applied at once, no request.
	if engines.IdentityChangeApprovalOn("default") {
		t.Fatal("the setting must be Off by default")
	}
	if code, out := move(adminTok, "STORE-A"); code != http.StatusOK || out["status"] != "success" || home() != "STORE-A" || requests() != 0 {
		t.Fatalf("Off: code %d status %q home %q requests %d", code, out["status"], home(), requests())
	}

	// Wrong role: refused, nothing moves, nothing is filed.
	if code, _ := move(stage47Token(mgr, engines.RoleStoreManager, "HO"), "STORE-B"); code < 400 || home() != "STORE-A" || requests() != 0 {
		t.Fatalf("a Store Manager moved a user: code %d home %q", code, home())
	}
	// Wrong tenant: a token for a tenant that does not hold this user is refused.
	otherTok := engines.SignToken("admin", "admin", engines.RoleSuperAdmin, "no-such-tenant", "", 1)
	if code, _ := move(otherTok, "STORE-B"); code < 400 || home() != "STORE-A" || requests() != 0 {
		t.Fatalf("another tenant moved a user: code %d home %q", code, home())
	}

	// Setting On: a mover request waits, the store does not change.
	if err := engines.SetSetting("default", engines.SettingIdentityChangeApproval, "true", "test"); err != nil {
		t.Fatal(err)
	}
	code, out := move(adminTok, "STORE-B")
	if code != http.StatusOK || out["status"] != "pending_approval" || out["request_id"] == "" || home() != "STORE-A" || requests() != 1 {
		t.Fatalf("On: code %d out %v home %q requests %d", code, out, home(), requests())
	}
	// A move to the store the user already has files nothing.
	if code, out := move(adminTok, "STORE-A"); code != http.StatusOK || out["status"] != "success" || requests() != 1 {
		t.Fatalf("same store: code %d out %v requests %d", code, out, requests())
	}
}
