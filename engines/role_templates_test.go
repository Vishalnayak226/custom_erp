package engines

import (
	"strings"
	"testing"

	"custom_erp/db"
)

// Stage 47.1.5/47.1.6/47.1.7.

func TestKnownModulesMatchTheTenantSchema(t *testing.T) {
	db.InitDB(testConnStr())
	rows, err := db.DB.Query(`SELECT DISTINCT module FROM tenant_default.doctype_meta`)
	if err != nil {
		t.Fatalf("read modules: %v", err)
	}
	defer rows.Close()
	declared := map[string]bool{}
	for _, module := range knownModules {
		declared[module] = true
	}
	live := map[string]bool{}
	for rows.Next() {
		var module string
		if err := rows.Scan(&module); err != nil {
			t.Fatalf("scan module: %v", err)
		}
		live[module] = true
		if !declared[module] {
			t.Errorf("doctype_meta has module %q that knownModules does not - Administrator and Auditor would silently not cover it", module)
		}
	}
	for module := range declared {
		if !live[module] {
			t.Errorf("knownModules declares %q, which no doctype uses - stale entry", module)
		}
	}
}

func TestEveryTemplateIsWellFormed(t *testing.T) {
	declared := map[string]bool{}
	for _, module := range knownModules {
		declared[module] = true
	}
	seen := map[string]bool{}
	for _, template := range RoleTemplates() {
		if strings.TrimSpace(template.Summary) == "" {
			t.Errorf("%s has no summary - every grant is supposed to be justifiable by it", template.Name)
		}
		if seen[normalizeRoleKey(template.Name)] {
			t.Errorf("%s is declared twice", template.Name)
		}
		seen[normalizeRoleKey(template.Name)] = true
		for module := range template.Modules {
			if !declared[module] {
				t.Errorf("%s grants module %q, which does not exist", template.Name, module)
			}
		}
		for _, legacy := range template.LegacyNames {
			if resolved, ok := RoleTemplateFor(legacy); !ok || resolved.Name != template.Name {
				t.Errorf("%s claims legacy name %q but it resolves to %q (ok=%v)", template.Name, legacy, resolved.Name, ok)
			}
		}
	}
}

func TestLegacyRoleNamesResolveToTheirTemplate(t *testing.T) {
	for _, c := range []struct{ role, want string }{
		{RoleStoreManager, "Store Supervisor"},
		{RoleSuperAdmin, "Administrator"},
		{RoleLegacySuperAdmin, "Administrator"},
		{"  cashier ", "Cashier"},
	} {
		template, ok := RoleTemplateFor(c.role)
		if !ok || template.Name != c.want {
			t.Errorf("RoleTemplateFor(%q) = %q (ok=%v), want %q", c.role, template.Name, ok, c.want)
		}
	}
	if _, ok := RoleTemplateFor("Night Shift Lead"); ok {
		t.Error("a tenant's own custom role must NOT resolve to a template - that is what stops the migration rewriting it")
	}
}

// The A-09 finding, restated as a property of the template rather than of
// one tenant's data: a Cashier's job never includes a colleague's salary
// advance, and its payslip access exists only so an employee can see their
// own (self scope, scope_policy.go).
func TestCashierTemplateDropsTheGenericSensitiveGrants(t *testing.T) {
	db.InitDB(testConnStr())
	template, ok := RoleTemplateFor(RoleCashier)
	if !ok {
		t.Fatal("Cashier has no template")
	}
	grants, err := TemplateGrants("default", template)
	if err != nil {
		t.Fatalf("TemplateGrants: %v", err)
	}
	if level, present := grants["EmployeeLoan"]; present {
		t.Errorf("Cashier still gets EmployeeLoan (%s) - the shipped seed's generic sensitive grant", level)
	}
	if grants["Payslip"] != AccessRead {
		t.Errorf("Cashier's Payslip grant = %s, want read (own payslip, self-scoped)", grants["Payslip"])
	}
	if grants["Grievance"] != AccessOperate {
		t.Errorf("Cashier must still be able to submit a grievance, got %s", grants["Grievance"])
	}
	// The Auditor's AccessNone overrides are the other direction of the
	// same rule: a read-everything role that still cannot read payroll.
	auditor, _ := RoleTemplateFor("Auditor")
	auditorGrants, err := TemplateGrants("default", auditor)
	if err != nil {
		t.Fatalf("TemplateGrants(Auditor): %v", err)
	}
	for _, doctype := range []string{"Payslip", "SalaryStructure", "EmployeeLoan", "Grievance"} {
		if level, present := auditorGrants[doctype]; present {
			t.Errorf("Auditor got %s on %s - an AccessNone override must remove the module grant", level, doctype)
		}
	}
	if auditorGrants["JournalVoucher"] != AccessRead {
		t.Errorf("Auditor must still read the ledger, got %s", auditorGrants["JournalVoucher"])
	}
}

func TestSoDDetection(t *testing.T) {
	// Every catalogued conflict must be reachable, or it is decoration.
	for _, conflict := range SoDConflicts() {
		access := EffectiveAccess{Role: "Test", Grants: map[string]AccessLevel{}, ApproverFor: map[string]bool{}}
		for doctype, level := range conflict.A.Doctypes {
			access.Grants[doctype] = level
			break
		}
		for _, doctype := range conflict.A.ApproverFor {
			access.ApproverFor[doctype] = true
			break
		}
		access.Capabilities = append(access.Capabilities, conflict.A.Capabilities...)
		for doctype, level := range conflict.B.Doctypes {
			access.Grants[doctype] = level
			break
		}
		for _, doctype := range conflict.B.ApproverFor {
			access.ApproverFor[doctype] = true
			break
		}
		access.Capabilities = append(access.Capabilities, conflict.B.Capabilities...)

		found := false
		for _, finding := range DetectSoDConflicts(access) {
			if finding.ConflictID == conflict.ID {
				found = true
				if finding.EvidenceA == "" || finding.EvidenceB == "" {
					t.Errorf("%s: reported with no evidence (%+v)", conflict.ID, finding)
				}
			}
		}
		if !found {
			t.Errorf("%s: holding both duties did not raise the conflict", conflict.ID)
		}
	}

	// Holding only one half raises nothing.
	oneSided := EffectiveAccess{
		Role:   "Test",
		Grants: map[string]AccessLevel{"Vendor": AccessOperate},
	}
	for _, finding := range DetectSoDConflicts(oneSided) {
		if finding.ConflictID == "vendor-create-and-payment" {
			t.Error("vendor create alone must not raise the payment conflict")
		}
	}

	// The Accounts Payable template's deliberate Vendor:read is what keeps
	// it clean - the reason that override exists at all.
	db.InitDB(testConnStr())
	ap, _ := RoleTemplateFor("Accounts Payable")
	grants, err := TemplateGrants("default", ap)
	if err != nil {
		t.Fatalf("TemplateGrants(AP): %v", err)
	}
	if grants["Vendor"] != AccessRead {
		t.Errorf("Accounts Payable's Vendor grant = %s, want read - it is the SoD control", grants["Vendor"])
	}
	for _, finding := range DetectSoDConflicts(EffectiveAccess{Role: "Accounts Payable", Grants: grants, Capabilities: ap.Capabilities}) {
		if finding.ConflictID == "vendor-create-and-payment" {
			t.Errorf("Accounts Payable raised the vendor/payment conflict: %+v", finding)
		}
	}
}

func TestPlanReportsCustomRolesAsUntouched(t *testing.T) {
	db.InitDB(testConnStr())
	plan, err := PlanRoleTemplateMigration("default")
	if err != nil {
		t.Fatalf("PlanRoleTemplateMigration: %v", err)
	}
	if len(plan.Diffs) == 0 {
		t.Fatal("expected at least one diff")
	}
	// "Supplier" is a real custom role in the shipped seed with no
	// template. It must be listed as untouched rather than silently reset.
	var sawSupplier bool
	for _, role := range plan.UntouchedCustomRoles {
		if role == "Supplier" {
			sawSupplier = true
		}
		if _, governed := RoleTemplateFor(role); governed {
			t.Errorf("%q is governed by a template but was listed as untouched", role)
		}
	}
	if !sawSupplier {
		t.Errorf("the Supplier role was not reported as an untouched custom role: %v", plan.UntouchedCustomRoles)
	}
	// The diff has to actually say something for a live role, or the
	// preview an owner approves is empty theatre.
	for _, diff := range plan.Diffs {
		if diff.Role == RoleCashier && !diff.Changed() {
			t.Error("the Cashier diff reports no change, but the template deliberately drops EmployeeLoan")
		}
	}
}

// Apply and revert are exercised on the Picker template specifically because
// no role named "Picker" has a single row in the shipped seed: the whole
// test creates and removes its own grants and cannot disturb a live role in
// this shared development schema.
func TestApplyAndRevertRoleTemplateMigration(t *testing.T) {
	db.InitDB(testConnStr())
	const role = "Picker"

	before, err := StoredRoleGrants("default", role)
	if err != nil {
		t.Fatalf("StoredRoleGrants: %v", err)
	}
	if len(before) != 0 {
		t.Skipf("%q already has %d grants in this schema - another session is using it; skipping rather than mutating it", role, len(before))
	}

	if _, err := ApplyRoleTemplateMigration("default", []string{role}, ""); err == nil {
		t.Error("an unapproved migration must be refused")
	}
	if _, err := ApplyRoleTemplateMigration("default", []string{"Night Shift Lead"}, "tester"); err == nil {
		t.Error("a custom role with no template must be refused")
	}

	migrationID, err := ApplyRoleTemplateMigration("default", []string{role}, "tester")
	if err != nil {
		t.Fatalf("ApplyRoleTemplateMigration: %v", err)
	}
	defer db.DB.Exec(`DELETE FROM tenant_default.role_permissions WHERE role = $1`, role)
	defer db.DB.Exec(`DELETE FROM tenant_default.role_template_migrations WHERE id = $1`, migrationID)

	after, err := StoredRoleGrants("default", role)
	if err != nil {
		t.Fatalf("StoredRoleGrants after apply: %v", err)
	}
	if len(after) == 0 {
		t.Fatal("apply wrote no grants")
	}
	if after["WarehouseTask"] != AccessOperate {
		t.Errorf("Picker's WarehouseTask grant = %s, want operate", after["WarehouseTask"])
	}
	if _, present := after["Payslip"]; present {
		t.Error("Picker was granted Payslip - the template grants no HR module at all")
	}

	found := false
	records, err := ListRoleTemplateMigrations("default")
	if err != nil {
		t.Fatalf("ListRoleTemplateMigrations: %v", err)
	}
	for _, record := range records {
		if record.ID == migrationID {
			found = true
			if record.ApprovedBy != "tester" {
				t.Errorf("approved_by = %q, want tester", record.ApprovedBy)
			}
			if record.RevertedAt != nil {
				t.Error("a fresh migration must not be marked reverted")
			}
		}
	}
	if !found {
		t.Error("the migration was not recorded in the ledger")
	}

	if err := RevertRoleTemplateMigration("default", migrationID, ""); err == nil {
		t.Error("an unattributed revert must be refused")
	}
	if err := RevertRoleTemplateMigration("default", migrationID, "tester"); err != nil {
		t.Fatalf("RevertRoleTemplateMigration: %v", err)
	}
	restored, err := StoredRoleGrants("default", role)
	if err != nil {
		t.Fatalf("StoredRoleGrants after revert: %v", err)
	}
	if len(restored) != 0 {
		t.Errorf("revert left %d grants behind - it must restore the exact prior state, including 'none'", len(restored))
	}
	if err := RevertRoleTemplateMigration("default", migrationID, "tester"); err == nil {
		t.Error("reverting twice must be refused")
	}
}

func TestExplainAccessAnswersWhy(t *testing.T) {
	db.InitDB(testConnStr())
	explanation, err := ExplainAccess("default", RoleCashier, "Payslip")
	if err != nil {
		t.Fatalf("ExplainAccess: %v", err)
	}
	if explanation.Template != "Cashier" || explanation.Summary == "" {
		t.Errorf("preview did not resolve the template: %+v", explanation)
	}
	if len(explanation.Fields) == 0 {
		t.Error("preview listed no sensitive fields for Payslip")
	}
	for _, field := range explanation.Fields {
		if field.CanRead {
			t.Errorf("preview says a Cashier can read Payslip.%s", field.Field)
		}
	}
	var sawSelf bool
	for _, reason := range explanation.ScopeReasons {
		if strings.Contains(reason, string(ScopeSelf)) {
			sawSelf = true
		}
	}
	if !sawSelf {
		t.Errorf("preview did not explain the self scope on Payslip: %v", explanation.ScopeReasons)
	}
	if explanation.Conflicts == nil {
		t.Error("preview must return an empty conflict list, not nil")
	}
}
