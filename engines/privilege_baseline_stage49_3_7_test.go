package engines

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"custom_erp/db"
)

// Stage 49.3.7 - tenant migration and customization proof.
//
// A baseline change is anything that changes what a shipped role holds: a
// role template edit (role_templates.go), a new record type landing in a
// module a template grants wholesale, or a migration seeding role_permissions.
// These tests are the standing checks every such change runs through.

// floorRoles are the roles that serve customers or move stock all day. None of
// them may hold a dangerous grant, by template or by the shipped seed.
var floorRoles = []string{RoleCashier, "Picker", "Employee Self-Service"}

func TestStage4937FloorRolesHoldNoDangerousGrant(t *testing.T) {
	db.InitDB(testConnStr())
	for _, name := range floorRoles {
		template, ok := RoleTemplateFor(name)
		if !ok {
			t.Fatalf("%s has no template", name)
		}
		found, err := TemplateDangerousGrants("default", template)
		if err != nil {
			t.Fatalf("TemplateDangerousGrants(%s): %v", name, err)
		}
		if len(found) > 0 {
			t.Errorf("the %s template holds dangerous grants: %+v", name, found)
		}
		// The shipped seed in this tenant, as the role actually stands.
		stored, err := StoredRoleGrants("default", name)
		if err != nil {
			t.Fatalf("StoredRoleGrants(%s): %v", name, err)
		}
		if found := DangerousGrantsFor(name, stored, CapabilitiesForRole(name)); len(found) > 0 {
			t.Errorf("the stored %s role holds dangerous grants: %+v", name, found)
		}
	}
}

// reviewedInheritedGrants is every dangerous grant a role template reaches
// only through a whole-module grant, each one reviewed. A template edit, or a
// new record type in a module a role is granted wholesale, that adds one
// fails TestStage4937EveryInheritedDangerousGrantIsReviewed until it is
// either written into the template explicitly or added here with its reason.
var reviewedInheritedGrants = map[string]string{
	// The job: the ledger and the payment run are what an accountant owns.
	"Accountant / BankAccount":     "owns the ledger; bank masters are finance set-up",
	"Accountant / GLPost":          "owns the ledger",
	"Accountant / JournalVoucher":  "owns the ledger",
	"Accountant / PaymentProposal": "owns the payment run (approval is a separate maker-checker step)",
	// The job: HR owns the employee record and the payroll run.
	"HR Manager / EmployeeLoan":    "owns salary advances",
	"HR Manager / Payslip":         "owns the payroll run",
	"HR Manager / SalaryStructure": "owns pay structures",
	// HR raises another person's password reset; a privileged target needs a
	// second administrator (49.2.4), so this is the maker half only.
	"HR Manager / PasswordResetRequest": "maker side of the dual-control reset",
	"Integrator / WebhookSubscription":  "wiring connectors is the role",
	// Store Manager is Store Supervisor's legacy name. The template gives HR
	// read; scope_policy's selfScopeExemptions gives Store Manager every
	// employee's EmployeeLoan rows because the shipped seed lets a
	// supervisor record staff salary advances. Together: a supervisor reads
	// every colleague's advance. Kept as shipped and reported as an owner
	// decision (49.3.7 finding) rather than changed here.
	"Store Manager / EmployeeLoan": "supervisor records staff salary advances - owner decision pending",
}

// fullAccessTemplates hold everything by design (allModules(AccessManage)).
var fullAccessTemplates = map[string]bool{"Administrator": true, RoleAdmin: true}

func TestStage4937EveryInheritedDangerousGrantIsReviewed(t *testing.T) {
	db.InitDB(testConnStr())
	seen := map[string]bool{}
	for _, template := range RoleTemplates() {
		if fullAccessTemplates[template.Name] {
			continue
		}
		names := append([]string{template.Name}, template.LegacyNames...)
		for _, name := range names {
			t2 := template
			t2.Name = name
			found, err := TemplateDangerousGrants("default", t2)
			if err != nil {
				t.Fatalf("TemplateDangerousGrants(%s): %v", name, err)
			}
			for _, g := range found {
				if !g.Inherited {
					continue
				}
				key := name + " / " + g.Doctype
				seen[key] = true
				if _, ok := reviewedInheritedGrants[key]; !ok {
					t.Errorf("%s inherits a dangerous grant nobody reviewed: %s at %s (%s) - list it in the template's Doctypes, or add it to reviewedInheritedGrants with the reason", name, g.Doctype, g.Level, g.Category)
				}
			}
		}
	}
	for key := range reviewedInheritedGrants {
		if !seen[key] {
			t.Errorf("reviewedInheritedGrants lists %q, which no template inherits any more - remove it", key)
		}
	}
}

// A template plan that would give a role a dangerous grant it does not hold
// says so before the owner applies it - a tenant is never weakened silently
// to make a migration "pass".
func TestStage4937PlanShowsDangerousGrantsBeforeSignOff(t *testing.T) {
	db.InitDB(testConnStr())
	const role = "Accounts Payable"
	before, err := StoredRoleGrants("default", role)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 0 {
		t.Skipf("%q already has grants in this schema - another session is using it", role)
	}
	t.Cleanup(func() {
		db.DB.Exec(`DELETE FROM tenant_default.role_permissions WHERE role = $1`, role)
		db.DB.Exec(`DELETE FROM tenant_default.role_template_migrations WHERE roles = $1 AND approved_by = 'qa-4937'`, role)
	})
	// The tenant has narrowed its payables clerk to looking only.
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
		VALUES ($1, 'VendorInvoice', TRUE, FALSE, FALSE, FALSE), ($1, 'JournalVoucher', TRUE, TRUE, TRUE, FALSE)`, role); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanRoleTemplateMigration("default")
	if err != nil {
		t.Fatal(err)
	}
	var diff *RoleTemplateDiff
	for i := range plan.Diffs {
		if plan.Diffs[i].Role == role {
			diff = &plan.Diffs[i]
		}
	}
	if diff == nil {
		t.Fatal("no diff for Accounts Payable")
	}
	gained := map[string]bool{}
	for _, g := range diff.DangerousGained {
		gained[g.Doctype] = true
	}
	if !gained["VendorInvoice"] || !gained["PaymentProposal"] || gained["JournalVoucher"] {
		t.Fatalf("dangerous_gained = %+v, want VendorInvoice and PaymentProposal (and not JournalVoucher, which the template narrows)", diff.DangerousGained)
	}

	// Applying it and reverting: the journal grant the template took away is
	// not handed back by the revert - it was obsolete; the rest is restored.
	id, err := ApplyRoleTemplateMigration("default", []string{role}, "qa-4937")
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := RevertRoleTemplateMigration("default", id, "qa-4937")
	if err != nil {
		t.Fatal(err)
	}
	if len(outcome.Withheld) != 1 || outcome.Withheld[0].Doctype != "JournalVoucher" {
		t.Fatalf("revert withheld %+v, want JournalVoucher", outcome.Withheld)
	}
	after, err := StoredRoleGrants("default", role)
	if err != nil {
		t.Fatal(err)
	}
	if after["VendorInvoice"] != AccessRead || after["JournalVoucher"] != AccessRead || len(after) != 2 {
		t.Fatalf("after revert the role holds %v, want VendorInvoice read and JournalVoucher read only", after)
	}
}

// grandfatheredGrantOverwrites are the migrations, each already applied once
// everywhere, that seeded a NEW record type's grants with ON CONFLICT DO
// UPDATE. The record type did not exist before them, so there was no tenant
// choice to overwrite; a re-run of any of them would. Nothing may join them.
var grandfatheredGrantOverwrites = map[string]bool{
	"migrations_stage35_4_shipping_package.sql": true, "migrations_stage35_5_courier_integration.sql": true,
	"migrations_stage35_6_channel_breadth.sql": true, "migrations_stage35_7_bundles_kits.sql": true,
	"migrations_stage35_8_settlement_reconciliation.sql": true, "migrations_stage36_1_product_groups.sql": true,
	"migrations_stage36_2_pim_tasks.sql": true, "migrations_stage36_3_import_depth.sql": true,
	"migrations_stage36_4_export_syndication.sql": true, "migrations_stage36_5_transform_rules.sql": true,
	"migrations_stage37_10_planning_depth.sql": true, "migrations_stage37_1_currency_foundation.sql": true,
	"migrations_stage37_2_multi_entity_intercompany.sql": true, "migrations_stage37_3_costing_valuation.sql": true,
	"migrations_stage37_4_budgeting_credit_dunning.sql": true, "migrations_stage37_6_deferred_prepaid_recurring_pricelist.sql": true,
	"migrations_stage37_7_projects_job_costing.sql": true, "migrations_stage37_8_service_management.sql": true,
	"migrations_stage37_9_quality_maintenance.sql": true,
}

// grandfatheredGrantRewrites: the one migration that rewrites existing
// grants - the HR/Admin -> Super Admin rename, scoped to those two
// full-access names, which no tenant's custom role can carry.
var grandfatheredGrantRewrites = map[string]bool{"migrations_stage40_3_super_admin_role.sql": true}

var (
	sqlLineComment   = regexp.MustCompile(`--[^\n]*`)
	grantInsertStmt  = regexp.MustCompile(`(?is)\bINSERT\s+INTO\s+[\w%."]*role_permissions\b[^;]*`)
	grantRewriteStmt = regexp.MustCompile(`(?is)\b(UPDATE|DELETE\s+FROM|TRUNCATE(\s+TABLE)?)\s+[\w%."]*role_permissions\b`)
	doUpdateClause   = regexp.MustCompile(`(?is)ON\s+CONFLICT[^;]*?DO\s+UPDATE`)
)

// Every baseline seed reaches a tenant through a migration. This keeps every
// migration from overwriting or removing a grant a tenant already has - on
// a baseline role it has customised, or on its own custom role.
func TestStage4937MigrationsNeverOverwriteATenantsGrants(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "db", "*.sql"))
	if err != nil || len(files) < 100 {
		t.Fatalf("migrations not found (%d, %v)", len(files), err)
	}
	var overwrites, rewrites []string
	for _, path := range files {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sql := sqlLineComment.ReplaceAllString(string(body), "")
		name := filepath.Base(path)
		for _, stmt := range grantInsertStmt.FindAllString(sql, -1) {
			if doUpdateClause.MatchString(stmt) {
				overwrites = append(overwrites, name)
				if !grandfatheredGrantOverwrites[name] {
					t.Errorf("%s seeds role_permissions with ON CONFLICT DO UPDATE: use DO NOTHING - a migration must never overwrite a grant a tenant has changed (49.3.7)", name)
				}
				break
			}
		}
		if grantRewriteStmt.MatchString(sql) {
			rewrites = append(rewrites, name)
			if !grandfatheredGrantRewrites[name] {
				t.Errorf("%s updates or deletes role_permissions rows: a baseline change goes through the role-template plan (owner sign-off), not a migration (49.3.7)", name)
			}
		}
	}
	// The grandfather lists only ever shrink.
	for _, list := range []struct {
		name  string
		files map[string]bool
		found []string
	}{{"grandfatheredGrantOverwrites", grandfatheredGrantOverwrites, overwrites}, {"grandfatheredGrantRewrites", grandfatheredGrantRewrites, rewrites}} {
		got := map[string]bool{}
		for _, f := range list.found {
			got[f] = true
		}
		var stale []string
		for f := range list.files {
			if !got[f] {
				stale = append(stale, f)
			}
		}
		sort.Strings(stale)
		if len(stale) > 0 {
			t.Errorf("%s lists %s, which no longer matches - remove it", list.name, strings.Join(stale, ", "))
		}
	}
}

// The classifier itself, without a database.
func TestStage4937DangerousGrantClassifier(t *testing.T) {
	cases := []struct {
		role    string
		doctype string
		level   AccessLevel
		want    bool
	}{
		{RoleCashier, "Payslip", AccessRead, false},           // own payslip only (self scope)
		{RoleCashier, "Payslip", AccessOperate, true},         // writing payslips is payroll
		{"HR Manager", "Payslip", AccessRead, true},           // sees everyone's pay
		{RoleStoreManager, "EmployeeLoan", AccessRead, true},  // self-scope exemption: every loan
		{"Night Lead", "JournalVoucher", AccessRead, false},   // reading the ledger
		{"Night Lead", "JournalVoucher", AccessOperate, true}, // posting to it
		{"Night Lead", "WebhookSubscription", AccessOperate, true},
		{"Night Lead", "Item", AccessManage, false},
		{RoleSuperAdmin, "JournalVoucher", AccessManage, false}, // full access is not reported grant by grant
	}
	for _, c := range cases {
		if _, got := IsDangerousGrant(c.role, c.doctype, c.level); got != c.want {
			t.Errorf("IsDangerousGrant(%s, %s, %s) = %v, want %v", c.role, c.doctype, c.level, got, c.want)
		}
	}
	if _, ok := DangerousCapabilityCategory("admin.setup-workbook"); !ok {
		t.Error("an admin capability is not dangerous")
	}
	if _, ok := DangerousCapabilityCategory("pos.price_override"); ok {
		t.Error("a till supervisor's override is not on the dangerous list")
	}
	d := DiffGrantLevels(map[string]AccessLevel{"A": AccessRead, "B": AccessManage, "C": AccessOperate},
		map[string]AccessLevel{"A": AccessOperate, "B": AccessRead, "D": AccessRead, "C": AccessNone})
	if fmt.Sprint(len(d.Loosened), len(d.Tightened), len(d.Added), len(d.Removed)) != "1 1 1 1" {
		t.Errorf("DiffGrantLevels = %+v", d)
	}
}
