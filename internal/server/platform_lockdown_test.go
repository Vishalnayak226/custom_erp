package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"
)

// Stage 60.6 lock-down: every client has its own Super Admin, so a Super Admin
// alone must not reach another client. Cross-client endpoints need a platform
// operator (ERP_PLATFORM_OPERATORS); the per-client reads stay open to a
// client's own Super Admin for its own client only.
func TestCrossClientEndpointsNeedPlatformOperator(t *testing.T) {
	db.InitDB(testConnStr())
	req := func(method, target, tenant, user string) *http.Request {
		r := httptest.NewRequest(method, target, strings.NewReader(`{}`))
		r.Header.Set("Resolved-Tenant-ID", tenant)
		r.Header.Set("Resolved-Username", user)
		r.Header.Set("Resolved-Role", engines.RoleSuperAdmin)
		return r
	}
	t.Setenv("ERP_PLATFORM_OPERATORS", "default:ops")

	// A client's Super Admin: refused on every cross-client endpoint.
	for name, h := range map[string]http.HandlerFunc{
		"list tenants":        handleListTenants,
		"tenant usage":        handleTenantUsage,
		"list sandboxes":      handleListSandboxTenants,
		"set entitlement":     handleSetModuleEntitlement,
		"set package":         handleSetTenantPackage,
		"provision tenant":    handleProvisionTenant,
		"set tenant hostname": handleSetTenantHostSlug,
	} {
		method := http.MethodPost
		if strings.HasPrefix(name, "list") || name == "tenant usage" {
			method = http.MethodGet
		}
		w := httptest.NewRecorder()
		h(w, req(method, "/x", "acme", "admin"))
		if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "PLATFORM-0317") {
			t.Errorf("%s as a client's Super Admin: %d %s, want 403 PLATFORM-0317", name, w.Code, w.Body.String())
		}
	}

	// The same client admin may read its own entitlements, not another client's.
	w := httptest.NewRecorder()
	handleGetModuleEntitlements(w, req(http.MethodGet, "/x?tenant_id=default", "acme", "admin"))
	if w.Code != http.StatusForbidden {
		t.Errorf("reading another client's entitlements: %d, want 403", w.Code)
	}
	w = httptest.NewRecorder()
	handleGetModuleEntitlements(w, req(http.MethodGet, "/x", "default", "admin"))
	if w.Code != http.StatusOK {
		t.Errorf("reading own entitlements: %d %s, want 200", w.Code, w.Body.String())
	}

	// The operator gets through.
	w = httptest.NewRecorder()
	handleListTenants(w, req(http.MethodGet, "/x", "default", "ops"))
	if w.Code != http.StatusOK {
		t.Errorf("operator listing tenants: %d %s, want 200", w.Code, w.Body.String())
	}
}
