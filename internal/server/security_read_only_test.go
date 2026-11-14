package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"
)

// 49.12.3 (owner's answer): read-only mode is switched from the Security
// screen (reason + own password, audited). While it is on, every change is
// refused with 423 and X-Refused: READ_ONLY - from people and from API
// clients - while reads, sign-out, a person's own password, the read-only
// POSTs and the emergency controls stay open; every answer says
// X-Read-Only, which drives the banner.
func TestStage49123ReadOnlyMode(t *testing.T) {
	for pattern := range readOnlyOpen {
		if _, ok := routeCapabilities[pattern]; !ok {
			t.Errorf("readOnlyOpen names %q, which is not a route", pattern)
		}
	}

	adminID, adminPW, cleanup := seedSecurityOpsUser(t, "Super Admin")
	defer cleanup()
	defer db.DB.Exec(`DELETE FROM tenant_default.system_settings WHERE key = $1`, engines.SettingReadOnly)
	defer engines.SetSetting("default", engines.SettingReadOnly, "false", "test")
	defer db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE status IN ('READ_ONLY_ON', 'READ_ONLY_OFF', 'REFUSED_READ_ONLY')`)
	engines.ResetLiveUserStateCache()
	token := engines.SignToken(adminID, adminID, "Super Admin", "default", "HO", 1)

	reached := apiMiddleware(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mode := apiMiddleware(handleReadOnlyMode)
	switchMode := func(on bool, password string) *httptest.ResponseRecorder {
		return doRequest(t, mode, http.MethodPost, "/api/v1/admin/security/read-only", token,
			map[string]interface{}{"on": on, "reason": "checking the stock records", "password": password})
	}

	if rec := doRequest(t, reached, http.MethodPost, "/api/v1/checkout", token, nil); rec.Code != http.StatusOK || rec.Header().Get("X-Read-Only") != "0" {
		t.Fatalf("off: %d, X-Read-Only %q", rec.Code, rec.Header().Get("X-Read-Only"))
	}
	if rec := switchMode(true, "wrong"); rec.Code == http.StatusOK {
		t.Fatal("read-only needs the administrator's own password")
	}
	if rec := switchMode(true, adminPW); rec.Code != http.StatusOK || rec.Header().Get("X-Read-Only") != "1" {
		t.Fatalf("turn on: %d %s", rec.Code, rec.Body.String())
	}

	for _, c := range []struct {
		method, path string
		refused      bool
	}{
		{http.MethodPost, "/api/v1/checkout", true},
		{http.MethodPut, "/api/v1/admin/settings", true},
		{http.MethodDelete, "/api/v1/admin/branding/logo", true},
		{http.MethodGet, "/api/v1/admin/security/posture", false},
		{http.MethodPost, "/api/v1/dashboards/query", false},
		{http.MethodPost, "/api/v1/lines/resolve", false},
		{http.MethodPost, "/api/v1/auth/logout", false},
		{http.MethodPost, "/api/v1/me/change-password", false},
		{http.MethodPost, "/api/v1/reports/export", false}, // a read; the export freeze governs it
	} {
		rec := doRequest(t, reached, c.method, c.path, token, nil)
		refused := rec.Code == http.StatusLocked && rec.Header().Get("X-Refused") == "READ_ONLY"
		if refused != c.refused || (!c.refused && rec.Code != http.StatusOK) {
			t.Errorf("%s %s: %d %q, refused want %v", c.method, c.path, rec.Code, rec.Header().Get("X-Refused"), c.refused)
		}
		if rec.Header().Get("X-Read-Only") != "1" {
			t.Errorf("%s %s: X-Read-Only %q while on", c.method, c.path, rec.Header().Get("X-Read-Only"))
		}
	}
	var n int
	db.DB.QueryRow(`SELECT count(*) FROM tenant_default.audit_logs WHERE action = 'SECURITY_CONTROL' AND status = 'READ_ONLY_ON' AND details LIKE '%stock records%'`).Scan(&n)
	if n == 0 {
		t.Error("turning it on left no audit row")
	}
	db.DB.QueryRow(`SELECT count(*) FROM tenant_default.audit_logs WHERE status = 'REFUSED_READ_ONLY' AND details = 'POST /api/v1/checkout'`).Scan(&n)
	if n == 0 {
		t.Error("a refused change left no audit row")
	}

	// An API client's change is refused the same way, before its key is claimed.
	issued, err := engines.IssueAPICredential("default", "ro4912", []string{"notifications:write"}, nil, "system")
	if err != nil {
		t.Fatal(err)
	}
	defer db.DB.Exec(`DELETE FROM tenant_default.api_credentials WHERE id = $1`, issued.Credential.ID)
	public := publicAPIMiddleware("notifications:write", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	req := httptest.NewRequest(http.MethodPost, "/api/public/v1/notifications", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+issued.APIKey)
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "ro4912-1")
	rec := httptest.NewRecorder()
	public(rec, req)
	if rec.Code != http.StatusLocked || rec.Header().Get("X-Refused") != "READ_ONLY" {
		t.Errorf("API client while read-only: %d %q", rec.Code, rec.Header().Get("X-Refused"))
	}

	// The switch itself stays open, and turning it off opens changes again.
	if rec := switchMode(false, adminPW); rec.Code != http.StatusOK || rec.Header().Get("X-Read-Only") != "0" {
		t.Fatalf("turn off: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doRequest(t, reached, http.MethodPost, "/api/v1/checkout", token, nil); rec.Code != http.StatusOK {
		t.Fatalf("after turning it off: %d", rec.Code)
	}
}
