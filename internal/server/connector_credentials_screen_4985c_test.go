package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 49.8.5 (screen): the list route never carries a secret, rotate replaces it
// and stamps the change, switch-off is honoured, and only an admin may.
func TestStage4985cCredentialScreenRoutes(t *testing.T) {
	db.InitDB(testConnStr())
	for _, k := range []string{"GET /api/v1/integrations/credentials", "POST /api/v1/integrations/credentials/rotate", "POST /api/v1/integrations/credentials/active"} {
		if _, ok := routeCapabilities[k]; !ok {
			t.Fatalf("%s has no route_capabilities entry", k)
		}
	}
	const code, secret = "T4985C-SCREEN", "S3cr3t-4985c-screen-token"
	cleanup := func() {
		_, _ = db.DB.Exec(`DELETE FROM tenant_default.channel_credentials WHERE channel_code = $1`, code)
		_, _ = db.DB.Exec(`DELETE FROM tenant_default.audit_logs WHERE action IN ('CONNECTOR_ROTATED','CONNECTOR_SWITCHED') AND details LIKE '%` + code + `%'`)
	}
	cleanup()
	t.Cleanup(cleanup)
	if err := engines.SaveChannelCredentialBy("default", code, map[string]string{"access_token": secret}, "before", nil); err != nil {
		t.Fatal(err)
	}
	call := func(h http.HandlerFunc, method, body, role string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/x", strings.NewReader(body))
		req.Header.Set("Resolved-Tenant-ID", "default")
		req.Header.Set("Resolved-Role", role)
		req.Header.Set("Resolved-Username", "screen4985c")
		rec := httptest.NewRecorder()
		h(rec, req)
		return rec
	}
	rec := call(handleListConnectorCredentials, http.MethodGet, "", engines.RoleSuperAdmin)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), code) || strings.Contains(rec.Body.String(), secret) {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(handleListConnectorCredentials, http.MethodGet, "", engines.RoleCashier); rec.Code != http.StatusForbidden {
		t.Fatalf("a cashier read the list: %d", rec.Code)
	}
	if rec := call(handleRotateConnectorCredential, http.MethodPost, `{"kind":"channel","code":"`+code+`","fields":{"access_token":"x"}}`, engines.RoleCashier); rec.Code != http.StatusForbidden {
		t.Fatalf("a cashier rotated: %d", rec.Code)
	}
	const next = "S3cr3t-4985c-rotated"
	rec = call(handleRotateConnectorCredential, http.MethodPost, `{"kind":"channel","code":"`+code+`","fields":{"access_token":"`+next+`","webhook_secret":""}}`, engines.RoleSuperAdmin)
	var out struct {
		Generated map[string]string `json:"generated"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), next) || len(out.Generated["webhook_secret"]) < 32 {
		t.Fatalf("rotate: %d %s", rec.Code, rec.Body.String())
	}
	// Once only: the list afterwards shows neither secret, but shows the new owner.
	rec = call(handleListConnectorCredentials, http.MethodGet, "", engines.RoleSuperAdmin)
	if strings.Contains(rec.Body.String(), next) || strings.Contains(rec.Body.String(), out.Generated["webhook_secret"]) || !strings.Contains(rec.Body.String(), "screen4985c") {
		t.Fatalf("list after rotate: %s", rec.Body.String())
	}
	rec = call(handleSetConnectorCredentialActive, http.MethodPost, `{"kind":"channel","code":"`+code+`","active":false}`, engines.RoleSuperAdmin)
	if rec.Code != http.StatusOK {
		t.Fatalf("switch off: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(handleListConnectorCredentials, http.MethodGet, "", engines.RoleSuperAdmin); !strings.Contains(rec.Body.String(), `"status":"Off"`) {
		t.Fatalf("not shown Off: %s", rec.Body.String())
	}
	if rec := call(handleSetConnectorCredentialActive, http.MethodPost, `{"kind":"channel","code":"`+code+`"}`, engines.RoleSuperAdmin); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a missing active flag: %d", rec.Code)
	}
}
