package engines

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// Stage 47.1.4. Two kinds of test here:
//
//   - a completeness test that re-parses the doctype_fields seeds in db/*.sql
//     and fails if doctypeScopes has drifted from them (the same
//     self-checking shape route_capabilities_test.go uses against routes.go);
//   - the cross-location / cross-owner / employee-self negative tests the
//     item asks for, against the constraint builder every caller uses.

// seedFieldRe matches one doctype_fields VALUES row:
//
//	('Doctype', 'fieldname', 'Label, possibly with commas', 'Type', TRUE, ...
var seedFieldRe = regexp.MustCompile(`\(\s*'([A-Za-z0-9_]+)'\s*,\s*'(location|location_code|employee_id|owner_id)'\s*,\s*'[^']*'\s*,\s*'[A-Za-z]+'\s*,\s*(TRUE|FALSE)`)

type seededScopeField struct {
	doctype, field string
	mandatory      bool
}

func seededScopeFields(t *testing.T) []seededScopeField {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("..", "db", "*.sql"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("could not read db/*.sql (err=%v, %d files)", err, len(paths))
	}
	var out []seededScopeField
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, m := range seedFieldRe.FindAllStringSubmatch(string(body), -1) {
			out = append(out, seededScopeField{doctype: m[1], field: m[2], mandatory: m[3] == "TRUE"})
		}
	}
	if len(out) < 40 {
		t.Fatalf("only %d scope fields parsed out of db/*.sql - the seed format changed and this test is no longer checking anything", len(out))
	}
	return out
}

func TestDoctypeScopeRegistryMatchesTheSeeds(t *testing.T) {
	for _, seeded := range seededScopeFields(t) {
		scope, known := ScopeForDoctype(seeded.doctype)
		if !known {
			t.Errorf("%s declares %q but is missing from doctypeScopes - a scoped doctype must ship classified", seeded.doctype, seeded.field)
			continue
		}
		switch seeded.field {
		case "location", "location_code":
			if len(scope.LocationFields) != 1 || scope.LocationFields[0] != seeded.field {
				t.Errorf("%s: registry location fields %v, seed says %q", seeded.doctype, scope.LocationFields, seeded.field)
			}
			if scope.LocationMandatory != seeded.mandatory {
				t.Errorf("%s: registry LocationMandatory=%v, seed says mandatory=%v", seeded.doctype, scope.LocationMandatory, seeded.mandatory)
			}
		case "employee_id":
			if scope.SelfField != "employee_id" {
				t.Errorf("%s declares employee_id but the registry has SelfField=%q", seeded.doctype, scope.SelfField)
			}
		case "owner_id":
			// Only a MANDATORY owner is the row's scope; an optional one
			// ("Owner (3PL Client, optional)") is an attribute.
			want := ""
			if seeded.mandatory {
				want = "owner_id"
			}
			if scope.OwnerField != want {
				t.Errorf("%s: registry OwnerField=%q, want %q (seed mandatory=%v)", seeded.doctype, scope.OwnerField, want, seeded.mandatory)
			}
		}
	}
}

func TestNoStaleDoctypeScopeEntries(t *testing.T) {
	seeded := map[string]bool{}
	for _, s := range seededScopeFields(t) {
		seeded[s.doctype] = true
	}
	for _, doctype := range ScopedDoctypes() {
		if !seeded[doctype] {
			t.Errorf("doctypeScopes has %q, but no db/*.sql seed declares a scope field on it - stale entry", doctype)
		}
	}
}

// The four personal-view doctypes carry a mandatory "owner" holding a
// USERNAME and are deliberately excluded (they have their own private/shared
// scoping). Locking that here stops a later pass from "completing" the
// registry with them and silently breaking shared dashboards/saved views.
func TestPersonalViewDoctypesAreNotSelfScoped(t *testing.T) {
	for doctype := range personalViewDoctypes {
		if scope, known := ScopeForDoctype(doctype); known && scope.SelfField != "" {
			t.Errorf("%s must not be self-scoped here - ListDashboardLayouts/OMS saved views implement their own private/shared rule", doctype)
		}
	}
}

func session(role, user, loc, employee string) SessionScope {
	return SessionScope{Role: role, UserID: user, LocationCode: loc, EmployeeCode: employee}
}

func TestMandatoryLocationRowWithNoLocationIsDenied(t *testing.T) {
	constraints, known := ScopeConstraints("POSSession", session(RoleCashier, "u1", "HO", ""))
	if !known || len(constraints) != 1 {
		t.Fatalf("expected one location constraint, got known=%v %v", known, constraints)
	}
	if constraints[0].AllowMissing {
		t.Error("POSSession declares location mandatory - a row without one must not be visible")
	}
	if failed := DocumentMatchesScope(constraints, map[string]interface{}{"code": "PS-1"}); failed != ScopeLocation {
		t.Errorf("a POSSession with no location must fail the location dimension, got %q", failed)
	}
	if failed := DocumentMatchesScope(constraints, map[string]interface{}{"location": "BLR"}); failed != ScopeLocation {
		t.Errorf("a POSSession at another location must be denied, got %q", failed)
	}
	if failed := DocumentMatchesScope(constraints, map[string]interface{}{"location": "HO"}); failed != "" {
		t.Errorf("the session's own location must pass, got %q", failed)
	}
}

// The behavior the old inline clause got right and must keep: a doctype
// whose location field is OPTIONAL is not location-scoped by a missing
// value, but IS still filtered when the value is present.
func TestOptionalLocationKeepsUnscopedRowsVisible(t *testing.T) {
	constraints, known := ScopeConstraints("Printer", session(RoleCashier, "u1", "HO", ""))
	if !known || len(constraints) != 1 || !constraints[0].AllowMissing {
		t.Fatalf("Printer's optional location must allow a missing value: known=%v %+v", known, constraints)
	}
	if failed := DocumentMatchesScope(constraints, map[string]interface{}{"code": "PRN-1"}); failed != "" {
		t.Errorf("a Printer with no location must stay visible, got %q", failed)
	}
	if failed := DocumentMatchesScope(constraints, map[string]interface{}{"location": "BLR"}); failed != ScopeLocation {
		t.Errorf("a Printer at another location must still be denied, got %q", failed)
	}
}

func TestEmployeeSelfScope(t *testing.T) {
	// A Cashier holds read-only on Payslip in the shipped seed, so it is
	// confined to its own employee record - the A-09 finding.
	cashier := session(RoleCashier, "u-cashier", "HO", "EMP-7")
	constraints, known := ScopeConstraints("Payslip", cashier)
	if !known {
		t.Fatal("Payslip must be in the scope registry")
	}
	if failed := DocumentMatchesScope(constraints, map[string]interface{}{"employee_id": "EMP-9", "net_pay": 42000}); failed != ScopeSelf {
		t.Errorf("a colleague's payslip must be denied, got %q", failed)
	}
	if failed := DocumentMatchesScope(constraints, map[string]interface{}{"employee_id": "EMP-7"}); failed != "" {
		t.Errorf("the cashier's own payslip must be visible, got %q", failed)
	}

	// Fail closed: no linked Employee record means no rows, not all rows.
	unlinked, _ := ScopeConstraints("Payslip", session(RoleCashier, "u-till", "HO", ""))
	if failed := DocumentMatchesScope(unlinked, map[string]interface{}{"employee_id": "EMP-7"}); failed != ScopeSelf {
		t.Error("a session with no Employee link must match no self-scoped row")
	}

	// Store Manager holds read-only on Payslip too - equally confined.
	manager, _ := ScopeConstraints("Payslip", session(RoleStoreManager, "u-mgr", "HO", "EMP-1"))
	if failed := DocumentMatchesScope(manager, map[string]interface{}{"employee_id": "EMP-9"}); failed != ScopeSelf {
		t.Errorf("a Store Manager must not read another employee's payslip, got %q", failed)
	}

	// ...but it holds create/update on Leave, so it keeps the whole list.
	leave, _ := ScopeConstraints("Leave", session(RoleStoreManager, "u-mgr", "HO", "EMP-1"))
	if failed := DocumentMatchesScope(leave, map[string]interface{}{"employee_id": "EMP-9"}); failed != "" {
		t.Errorf("a Store Manager administers its team's leave and must still see it, got %q", failed)
	}

	// Super Admin is global on every dimension.
	if c, _ := ScopeConstraints("Payslip", session(RoleSuperAdmin, "u-admin", "HO", "")); len(c) != 0 {
		t.Errorf("Super Admin must have no scope constraints, got %v", c)
	}
	if c, _ := ScopeConstraints("Payslip", session(RoleLegacySuperAdmin, "u-admin", "HO", "")); len(c) != 0 {
		t.Errorf("the legacy Super Admin name must resolve global too, got %v", c)
	}
}

func TestOwnerScopeIsDeniedWithoutTheGlobalCapability(t *testing.T) {
	// No session carries a 3PL owner yet (47.5), so a role without the
	// global-owner capability sees no owner-scoped row at all.
	cashier, known := ScopeConstraints("StorageBillingRate", session(RoleCashier, "u1", "HO", ""))
	if !known {
		t.Fatal("StorageBillingRate must be in the scope registry")
	}
	var sawOwner bool
	for _, c := range cashier {
		if c.Dimension == ScopeOwner {
			sawOwner = true
			if c.Value != "" || c.AllowMissing {
				t.Errorf("owner constraint should fail closed, got %+v", c)
			}
		}
	}
	if !sawOwner {
		t.Fatal("expected an owner constraint for a role without scope.owner.global")
	}
	if failed := DocumentMatchesScope(cashier, map[string]interface{}{"location_code": "HO", "owner_id": "OWN-1"}); failed != ScopeOwner {
		t.Errorf("a Cashier must not see another party's 3PL billing row, got %q", failed)
	}

	// Store Manager holds scope.owner.global today (the pre-47.5 default),
	// so only the location dimension remains for it.
	manager, _ := ScopeConstraints("StorageBillingRate", session(RoleStoreManager, "u2", "HO", ""))
	for _, c := range manager {
		if c.Dimension == ScopeOwner {
			t.Error("Store Manager holds scope.owner.global and must not get an owner constraint")
		}
	}
	if failed := DocumentMatchesScope(manager, map[string]interface{}{"location_code": "HO", "owner_id": "OWN-1"}); failed != "" {
		t.Errorf("Store Manager must still see its own location's 3PL billing rows, got %q", failed)
	}
}

// A doctype the registry does not know (one built at runtime through the
// Doctype Builder) must report unknown so the caller keeps the pre-47.1.4
// location clause instead of silently dropping every filter.
func TestUnknownDoctypeFallsBackRatherThanUnscoping(t *testing.T) {
	if _, known := ScopeConstraints("SomeTenantInventedDoctype", session(RoleCashier, "u1", "HO", "")); known {
		t.Error("an unregistered doctype must report unknown")
	}
}
