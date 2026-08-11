package engines

import "strings"

// Roles (Stage 40.3).
//
// The top role in this product was called "HR/Admin" - a name inherited from
// the very first migration, when the same person did HR and administration.
// It has not meant that for a long time: it is the role that can do
// everything, everywhere, in every module, and calling it HR/Admin made the
// profile chip read like a job title rather than a privilege level. It is
// now "Super Admin".
//
// The rename is a real one - db/migrations_stage40_3_super_admin_role.sql
// rewrites the stored value in users, role_permissions, approval_rules,
// approval_log and field_permissions - but the old name is deliberately kept
// working:
//
//   - a session token minted before the migration still carries "HR/Admin";
//   - an external script or saved API payload may still send it;
//   - a tenant schema provisioned from an older snapshot may still hold it.
//
// So every comparison goes through IsSuperAdmin rather than testing a string,
// and both names resolve to the same privilege. This is also why the 37
// scattered role-string comparisons across nine files became one predicate:
// a second rename later is now one edit, not another sweep.

const (
	// RoleSuperAdmin is the canonical name, and what is stored from here on.
	RoleSuperAdmin = "Super Admin"
	// RoleLegacySuperAdmin is the pre-Stage-35.3 name. Still accepted
	// everywhere, never written.
	RoleLegacySuperAdmin = "HR/Admin"

	RoleStoreManager = "Store Manager"
	RoleCashier      = "Cashier"
)

// IsSuperAdmin reports whether a role has unrestricted access, under either
// the current or the legacy name.
//
// Case- and space-insensitive because roles arrive from tokens, headers,
// CSV imports and hand-written API payloads, and "super admin" failing where
// "Super Admin" succeeds would be a security-shaped surprise in the wrong
// direction (a silent denial that looks like a bug and gets "fixed" by
// widening something else).
func IsSuperAdmin(role string) bool {
	switch normalizeRoleKey(role) {
	case "superadmin", "hr/admin", "hradmin":
		return true
	}
	return false
}

// CanonicalRole maps a role to the name this product stores today. Anything
// unrecognised is returned untouched - tenants define their own roles beyond
// the built-in three, and this must not rewrite them.
func CanonicalRole(role string) string {
	if IsSuperAdmin(role) {
		return RoleSuperAdmin
	}
	return role
}

func normalizeRoleKey(role string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(role), " ", ""))
}
