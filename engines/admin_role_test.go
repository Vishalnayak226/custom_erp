package engines

import "testing"

// Stage 125.1/125.6: Super Admin is the platform operator's alone. A client's
// Admin administers its client exactly as Super Admin does (HasFullAccess) but
// never passes IsSuperAdmin, which guards the developer areas, and keeps the
// authenticator (decision D5).
func TestStage125AdminRole(t *testing.T) {
	for _, role := range []string{"Admin", "admin", " ADMIN "} {
		if IsSuperAdmin(role) {
			t.Errorf("IsSuperAdmin(%q) = true; Admin must stay out of the operator's areas", role)
		}
		if !HasFullAccess(role) {
			t.Errorf("HasFullAccess(%q) = false; Admin runs the whole business", role)
		}
		if !RequiresMFA(role) {
			t.Errorf("RequiresMFA(%q) = false; Admin keeps the authenticator", role)
		}
		if got := CanonicalRole(role); got != RoleAdmin {
			t.Errorf("CanonicalRole(%q) = %q, want %q", role, got, RoleAdmin)
		}
	}
	for _, role := range []string{RoleSuperAdmin, RoleLegacySuperAdmin} {
		if !IsSuperAdmin(role) || !HasFullAccess(role) || !RequiresMFA(role) {
			t.Errorf("%q lost Super Admin reach", role)
		}
	}
	for _, role := range []string{RoleStoreManager, RoleCashier, "Administrator", "Admins", ""} {
		if HasFullAccess(role) {
			t.Errorf("HasFullAccess(%q) = true", role)
		}
	}

	held := map[string]bool{}
	for _, c := range CapabilitiesForRole(RoleAdmin) {
		held[c] = true
	}
	for _, c := range []string{"audit.logs", "finance.statements", "hr.payroll", "pos.price_override", "returns.exception"} {
		if !held[c] {
			t.Errorf("Admin is missing business capability %q", c)
		}
	}
	if held["system.logs"] {
		t.Error("Admin holds system.logs; the platform's system logs are a developer area")
	}
	if tpl, ok := RoleTemplateFor("admin"); !ok || tpl.Name != RoleAdmin || len(tpl.GlobalScopes) != 4 {
		t.Errorf("RoleTemplateFor(admin) = %+v, %v", tpl, ok)
	}
}

// Stage 125: only the operator's own client may hold a Super Admin user.
func TestStage125IsPlatformTenant(t *testing.T) {
	t.Setenv(platformOperatorsEnv, "")
	if !IsPlatformTenant("default") || !IsPlatformTenant("") || IsPlatformTenant("minn") {
		t.Error("with no operators named, only default is the platform's client")
	}
	t.Setenv(platformOperatorsEnv, " Ops:alice , default:admin")
	if !IsPlatformTenant("ops") || !IsPlatformTenant("default") || IsPlatformTenant("minn") {
		t.Error("each tenant named in ERP_PLATFORM_OPERATORS is the platform's")
	}
	t.Setenv(platformOperatorsEnv, "ops:alice")
	if IsPlatformTenant("default") {
		t.Error("default is not the platform's once operators name another client")
	}
}
