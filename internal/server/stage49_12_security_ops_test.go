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

func seedSecurityOpsUser(t *testing.T, role string) (id, password string, cleanup func()) {
	t.Helper()
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	id = "__secops_" + hex.EncodeToString(b) + "__"
	password = randomTestPassword()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.users (id, username, password_hash, email, role, status, location_code)
		VALUES ($1, $1, $2, $3, $4, 'Active', 'HO')`, id, string(hash), id+"@secops.invalid", role); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return id, password, func() {
		db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = $1`, id)
		db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE user_id = $1`, id)
		engines.ResetLiveUserStateCache()
	}
}

// Stage 49.11.1 / 49.12.3: a delivered export is a DATA_EXPORT event; with the
// client's export freeze on, every export is refused (423) and logged, while
// audit evidence exports stay open; the freeze needs the admin's password.
func TestStage49123ExportFreezeAndExportEvent(t *testing.T) {
	db.InitDB(testConnStr())
	adminID, adminPW, cleanup := seedSecurityOpsUser(t, "Super Admin")
	defer cleanup()
	defer engines.SetSetting("default", engines.SettingFreezeExports, "false", "test")
	engines.ResetLiveUserStateCache()
	token := engines.SignToken(adminID, adminID, "Super Admin", "default", "HO", 1)
	export := apiMiddleware(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("rows")) })
	const exportPath = "/api/v1/admin/setup-workbook/export"

	if rec := doRequest(t, export, http.MethodGet, exportPath, token, nil); rec.Code != http.StatusOK {
		t.Fatalf("export before the freeze: %d %s", rec.Code, rec.Body.String())
	}
	countEvents := func(status string) int {
		var n int
		db.DB.QueryRow(`SELECT count(*) FROM tenant_default.audit_logs WHERE user_id = $1 AND action = 'DATA_EXPORT' AND status = $2`, adminID, status).Scan(&n)
		return n
	}
	if countEvents("SUCCESS") != 1 {
		t.Fatal("a delivered export must be one DATA_EXPORT SUCCESS event")
	}

	freeze := apiMiddleware(handleFreezeExports)
	if rec := doRequest(t, freeze, http.MethodPost, "/api/v1/admin/security/freeze-exports", token,
		map[string]interface{}{"on": true, "reason": "suspected leak", "password": "not-the-password"}); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("freeze with a wrong password: %d, want 422", rec.Code)
	}
	if engines.GetSettingBool("default", engines.SettingFreezeExports) {
		t.Fatal("a wrong password must not freeze exports")
	}
	if rec := doRequest(t, freeze, http.MethodPost, "/api/v1/admin/security/freeze-exports", token,
		map[string]interface{}{"on": true, "reason": "suspected leak", "password": adminPW}); rec.Code != http.StatusOK {
		t.Fatalf("freeze: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doRequest(t, export, http.MethodGet, exportPath, token, nil); rec.Code != http.StatusLocked {
		t.Fatalf("export under the freeze: %d, want 423", rec.Code)
	}
	if countEvents("REFUSED_FROZEN") != 1 {
		t.Fatal("a refused export must be logged as REFUSED_FROZEN")
	}
	if rec := doRequest(t, export, http.MethodGet, "/api/v1/admin/audit-logs/archives/x/export", token, nil); rec.Code != http.StatusOK {
		t.Fatalf("audit evidence export under the freeze: %d, want 200", rec.Code)
	}
	if rec := doRequest(t, freeze, http.MethodPost, "/api/v1/admin/security/freeze-exports", token,
		map[string]interface{}{"on": false, "reason": "all clear", "password": adminPW}); rec.Code != http.StatusOK {
		t.Fatalf("unfreeze: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doRequest(t, export, http.MethodGet, exportPath, token, nil); rec.Code != http.StatusOK {
		t.Fatalf("export after the freeze: %d", rec.Code)
	}
}

// Stage 49.12.3: "sign out everywhere" kills the target's live token at once
// and needs the admin's password; the posture endpoint answers.
func TestStage49123SignOutEverywhereEndsTheSession(t *testing.T) {
	db.InitDB(testConnStr())
	adminID, adminPW, cleanAdmin := seedSecurityOpsUser(t, "Super Admin")
	defer cleanAdmin()
	userID, _, cleanUser := seedSecurityOpsUser(t, "Cashier")
	defer cleanUser()
	engines.ResetLiveUserStateCache()
	adminToken := engines.SignToken(adminID, adminID, "Super Admin", "default", "HO", 1)
	userToken := engines.SignToken(userID, userID, "Cashier", "default", "HO", 1)
	probe := apiMiddleware(handleGetDocTypes)
	if rec := doRequest(t, probe, http.MethodGet, "/api/v1/meta/doctypes", userToken, nil); rec.Code != http.StatusOK {
		t.Fatalf("baseline: %d", rec.Code)
	}
	signOut := apiMiddleware(handleSignOutUserEverywhere)
	if rec := doRequest(t, signOut, http.MethodPost, "/api/v1/admin/security/sign-out-user", userToken,
		map[string]string{"id": adminID, "reason": "cashier tries it", "password": "x"}); rec.Code == http.StatusOK {
		t.Fatal("a cashier must not use an emergency control")
	}
	if rec := doRequest(t, signOut, http.MethodPost, "/api/v1/admin/security/sign-out-user", adminToken,
		map[string]string{"id": userID, "reason": "phone lost on the floor", "password": adminPW}); rec.Code != http.StatusOK {
		t.Fatalf("sign out: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doRequest(t, probe, http.MethodGet, "/api/v1/meta/doctypes", userToken, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("the signed-out token still works: %d", rec.Code)
	}

	rec := doRequest(t, apiMiddleware(handleSecurityPosture), http.MethodGet, "/api/v1/admin/security/posture", adminToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("posture: %d %s", rec.Code, rec.Body.String())
	}
	var p struct {
		Rules []json.RawMessage `json:"rules"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil || len(p.Rules) == 0 {
		t.Fatalf("posture has no rules: %v %s", err, rec.Body.String())
	}
}
