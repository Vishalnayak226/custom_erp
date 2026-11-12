package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Stage 65.5: the Control Panel lists every client for a platform operator and
// refuses a client's own Super Admin, like every cross-client endpoint.
func TestControlPanelOperatorOnly(t *testing.T) {
	db.InitDB(testConnStr())
	t.Setenv("ERP_PLATFORM_OPERATORS", "default:ops")
	t.Setenv("TENANT_BASE_DOMAIN", "example.test")
	call := func(user string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/platform/control-panel", nil)
		r.Header.Set("Resolved-Tenant-ID", "default")
		r.Header.Set("Resolved-Username", user)
		r.Header.Set("Resolved-Role", engines.RoleSuperAdmin)
		rec := httptest.NewRecorder()
		handleControlPanel(rec, r)
		return rec
	}
	if rec := call("someone"); rec.Code != http.StatusForbidden {
		t.Fatalf("non-operator Super Admin: status %d, want 403", rec.Code)
	}
	rec := call("ops")
	if rec.Code != http.StatusOK {
		t.Fatalf("operator: status %d body %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Clients []engines.ControlPanelClient `json:"clients"`
		Build   map[string]string            `json:"build"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	var def *engines.ControlPanelClient
	for i := range body.Clients {
		if body.Clients[i].TenantID == "default" {
			def = &body.Clients[i]
		}
	}
	if def == nil {
		t.Fatalf("default client missing from %d clients", len(body.Clients))
	}
	if def.Problem != "" || def.ActiveUsers == 0 || len(def.ModulesEnabled)+len(def.ModulesDisabled) == 0 {
		t.Errorf("default row incomplete: problem=%q users=%d modules=%d/%d", def.Problem, def.ActiveUsers, len(def.ModulesEnabled), len(def.ModulesDisabled))
	}
	if def.HostSlug != "" && def.URL != "https://"+def.HostSlug+".example.test/" {
		t.Errorf("url = %q for slug %q", def.URL, def.HostSlug)
	}
	if body.Build["version"] == "" {
		t.Error("build version missing")
	}
}
