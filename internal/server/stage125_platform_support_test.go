package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Stage 125.6: an Admin holds every admin route of its own client and every
// record type; only the platform's developer routes stay Super Admin. No
// database: both checks answer from code for a full-access role.
func TestStage125AdminGates(t *testing.T) {
	routes := []struct {
		pattern, role string
		want          bool
	}{
		{"GET /api/v1/admin/users", engines.RoleAdmin, true},
		{"GET /api/v1/admin/settings", engines.RoleAdmin, true},
		{"GET /api/v1/admin/role-permissions", engines.RoleAdmin, true},
		{"GET /api/v1/admin/image-drive", engines.RoleAdmin, true},
		{"/api/v1/logs/audit", engines.RoleAdmin, true},
		{"GET /api/v1/finance/trial-balance", engines.RoleAdmin, true},
		{"GET /api/v1/admin/users", engines.RoleStoreManager, false},
		{"/api/v1/logs/system", engines.RoleAdmin, false},
		{"/api/v1/logs/system", engines.RoleSuperAdmin, true},
		{"GET /api/v1/admin/tenants", engines.RoleAdmin, false},
		{"GET /api/v1/admin/modules", engines.RoleAdmin, false},
		{"GET /api/v1/admin/extension/hooks", engines.RoleAdmin, false},
		{"GET /api/v1/admin/extension/hooks", engines.RoleSuperAdmin, true},
		{"POST /api/v1/platform/tenants/{tenant}/open", engines.RoleAdmin, false},
		{"POST /api/v1/auth/operator-handoff", "", true},
	}
	for _, c := range routes {
		ok, _, found := checkRouteCapability(c.pattern, c.role)
		if !found {
			t.Fatalf("%s has no route classification", c.pattern)
		}
		if ok != c.want {
			t.Errorf("checkRouteCapability(%q, %q) = %v, want %v", c.pattern, c.role, ok, c.want)
		}
	}

	records := []struct {
		doctype, action string
		want            bool
	}{
		{"Item", "create", true},
		{"PurchaseOrder", "delete", true},
		{"StatusTransitionRule", "update", true},
		{"WebhookSubscription", "create", true},
	}
	for _, c := range records {
		ok, err := checkPermission("default", engines.RoleAdmin, c.doctype, c.action)
		if err != nil || ok != c.want {
			t.Errorf("checkPermission(Admin, %s, %s) = %v, %v; want %v", c.doctype, c.action, ok, err, c.want)
		}
	}
}

// Stage 125.2/125.4: the operator opens a client from the Control Panel, the
// client's own address spends the single-use code for a Super Admin session,
// and the support user can neither sign in nor be seen by the client. A new
// client's own top account is Admin, and it cannot be given a Super Admin.
func TestStage125OpenClientAsSupportUser(t *testing.T) {
	db.InitDB(testConnStr())
	var migrated bool
	_ = db.DB.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_schema = 'tenant_default' AND table_name = 'users' AND column_name = 'is_platform_support')`).Scan(&migrated)
	if !migrated {
		t.Skip("needs db/migrations_stage125_2_platform_support_user.sql applied")
	}
	t.Setenv("ERP_PLATFORM_OPERATORS", "default:ops")
	t.Setenv("TENANT_BASE_DOMAIN", "example.test")

	suffix := fmt.Sprintf("%d", time.Now().UnixNano()%1_000_000_000)
	tenantID, schema, slug := "s125"+suffix, "tenant_s125"+suffix, "s125-"+suffix
	defer func() {
		db.DB.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
		db.DB.Exec(`DELETE FROM public.tenants WHERE tenant_id = $1`, tenantID)
		db.DB.Exec(`DELETE FROM public.tenant_lifecycle_events WHERE tenant_id = $1`, tenantID)
		engines.InvalidateTenantLifecycleCache(tenantID)
	}()
	if _, err := engines.ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}

	var adminRole string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT role FROM %s.users WHERE id = 'admin'`, schema)).Scan(&adminRole); err != nil || adminRole != engines.RoleAdmin {
		t.Fatalf("a new client's admin is %q (%v), want Admin", adminRole, err)
	}

	open := func(user string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/platform/tenants/"+tenantID+"/open", strings.NewReader(`{}`))
		r.SetPathValue("tenant", tenantID)
		r.Header.Set("Resolved-Tenant-ID", "default")
		r.Header.Set("Resolved-Username", user)
		r.Header.Set("Resolved-Role", engines.RoleSuperAdmin)
		rec := httptest.NewRecorder()
		handleControlPanelOpenTenant(rec, r)
		return rec
	}
	redeem := func(code string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/operator-handoff", strings.NewReader(`{"code":"`+code+`"}`))
		r.Header.Set("Resolved-Tenant-ID", tenantID)
		rec := httptest.NewRecorder()
		handleOperatorHandoff(rec, r)
		return rec
	}
	codeOf := func(rec *httptest.ResponseRecorder) string {
		var out struct {
			URL string `json:"url"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		prefix := "https://" + slug + ".example.test/#operator_handoff="
		if !strings.HasPrefix(out.URL, prefix) {
			t.Fatalf("open url = %q, want %s<code>", out.URL, prefix)
		}
		return strings.TrimPrefix(out.URL, prefix)
	}

	if rec := open("ops"); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("open with no hostname: %d %s, want 422", rec.Code, rec.Body.String())
	}
	if _, err := db.DB.Exec(`UPDATE public.tenants SET host_slug = $1 WHERE tenant_id = $2`, slug, tenantID); err != nil {
		t.Fatal(err)
	}
	if rec := open("someone"); rec.Code != http.StatusForbidden {
		t.Fatalf("open by a non-operator: %d, want 403", rec.Code)
	}

	rec := open("ops")
	if rec.Code != http.StatusOK {
		t.Fatalf("open: %d %s", rec.Code, rec.Body.String())
	}
	code := codeOf(rec)

	// The session carries the support user as Super Admin of this client.
	rec = redeem(code)
	if rec.Code != http.StatusOK {
		t.Fatalf("redeem: %d %s", rec.Code, rec.Body.String())
	}
	var session map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &session)
	claims, err := engines.ParseToken(session["token"])
	if err != nil || claims["tenant"] != tenantID || claims["user"] != engines.PlatformSupportUsername || !engines.IsSuperAdmin(claims["role"]) {
		t.Fatalf("session claims = %v (%v)", claims, err)
	}
	if rec := redeem(code); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a spent code redeemed again: %d, want 401", rec.Code)
	}

	// An expired code is refused too.
	code = codeOf(open("ops"))
	db.DB.Exec(fmt.Sprintf(`UPDATE %s.users SET handoff_expires_at = NOW() - INTERVAL '1 second' WHERE id = $1`, schema), engines.PlatformSupportUsername)
	if rec := redeem(code); rec.Code != http.StatusUnauthorized {
		t.Fatalf("an expired code: %d, want 401", rec.Code)
	}

	// No password signs in as the support user - not even the stored marker.
	var stored string
	db.DB.QueryRow(fmt.Sprintf(`SELECT password_hash FROM %s.users WHERE id = $1`, schema), engines.PlatformSupportUsername).Scan(&stored)
	for _, pw := range []string{stored, "", "wholeops-support"} {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/login", strings.NewReader(fmt.Sprintf(`{"username":%q,"password":%q}`, engines.PlatformSupportUsername, pw)))
		r.Header.Set("Resolved-Tenant-ID", tenantID)
		lrec := httptest.NewRecorder()
		handleLogin(lrec, r)
		if lrec.Code == http.StatusOK {
			t.Fatalf("login as the support user with %q succeeded", pw)
		}
	}

	// Hidden from the client's own user list.
	r := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	r.Header.Set("Resolved-Tenant-ID", tenantID)
	r.Header.Set("Resolved-Role", engines.RoleSuperAdmin)
	lrec := httptest.NewRecorder()
	handleListUsers(lrec, r)
	if lrec.Code != http.StatusOK || strings.Contains(lrec.Body.String(), engines.PlatformSupportUsername) || !strings.Contains(lrec.Body.String(), `"admin"`) {
		t.Fatalf("users list: %d %s", lrec.Code, lrec.Body.String())
	}

	// A client's Admin cannot reach the support user through any user action.
	for _, path := range []string{"status", "location", "reset-password", "reset-mfa", "supplier", "stores"} {
		body := `{"id":"` + engines.PlatformSupportUsername + `","status":"Inactive","location_code":"HO","supplier_code":"X","access":"all","reason":"x"}`
		r := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/"+path, strings.NewReader(body))
		r.Header.Set("Resolved-Tenant-ID", tenantID)
		r.Header.Set("Resolved-Role", engines.RoleAdmin)
		r.Header.Set("Resolved-User-ID", "admin")
		rec := httptest.NewRecorder()
		map[string]http.HandlerFunc{
			"status": handleSetUserStatus, "location": handleSetUserLocation, "reset-password": handleRequestPasswordReset,
			"reset-mfa": handleAdminResetUserMFA, "supplier": handleSetUserSupplier, "stores": handleUserStores,
		}[path](rec, r)
		if rec.Code != http.StatusNotFound {
			t.Errorf("Admin %s on the support user: %d %s, want 404", path, rec.Code, rec.Body.String())
		}
	}

	// A client cannot be given a Super Admin user; in the operator's own
	// client only a Super Admin may make one.
	create := func(tenant, actorRole string) int {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", strings.NewReader(`{"username":"s125boss","password":"x","role":"Super Admin"}`))
		r.Header.Set("Resolved-Tenant-ID", tenant)
		r.Header.Set("Resolved-Role", actorRole)
		rec := httptest.NewRecorder()
		handleCreateUser(rec, r)
		if strings.Contains(rec.Body.String(), "kept for the platform operator") {
			return http.StatusUnprocessableEntity
		}
		return 0
	}
	if create(tenantID, engines.RoleSuperAdmin) != http.StatusUnprocessableEntity || create(tenantID, engines.RoleAdmin) != http.StatusUnprocessableEntity {
		t.Error("a Super Admin user was not refused in a client")
	}
	if create("default", engines.RoleAdmin) != http.StatusUnprocessableEntity {
		t.Error("an Admin made a Super Admin in the operator's client")
	}
	if create("default", engines.RoleSuperAdmin) == http.StatusUnprocessableEntity {
		t.Error("the operator's own client refused a Super Admin user")
	}
}
