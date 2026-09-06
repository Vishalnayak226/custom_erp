package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
)

// Stage 47.1.3. These tests are the executable form of the item's own
// acceptance sentence: a real Cashier gets nothing back for payroll, bank,
// grievance, cost, connector-secret and personal fields, on read AND on
// write AND through bulk import/edit (which share RejectRestrictedFieldWrites
// with the single-document path), while the roles that legitimately need a
// field keep it.

func TestPayrollFieldsAreSuperAdminOnly(t *testing.T) {
	payrollDoctypes := map[string][]string{
		"Payslip":         {"gross_pay", "net_pay", "pf_deduction", "esi_deduction", "pt_deduction", "tds_deduction", "loan_deduction"},
		"SalaryStructure": {"basic", "hra", "other_allowances", "pf_percent", "esi_percent", "pt_amount"},
		"EmployeeLoan":    {"principal_amount", "monthly_deduction", "outstanding_balance"},
		"Employee":        {"bank_account_no", "bank_ifsc"},
	}
	for doctype, fields := range payrollDoctypes {
		for _, role := range []string{RoleCashier, RoleStoreManager, "Picker", "Auditor", ""} {
			policy := sensitiveFieldPolicy(role, doctype)
			for _, field := range fields {
				perm, ok := policy[field]
				if !ok {
					t.Errorf("%s.%s: role %q got no policy entry - the field is unrestricted", doctype, field, role)
					continue
				}
				if perm.AllowRead || perm.AllowWrite {
					t.Errorf("%s.%s: role %q got read=%v write=%v, want both false", doctype, field, role, perm.AllowRead, perm.AllowWrite)
				}
			}
		}
		// Both spellings of the top role keep full access - a session token
		// minted before the Stage 40.3 rename still says "HR/Admin".
		for _, role := range []string{RoleSuperAdmin, RoleLegacySuperAdmin, "super admin"} {
			if policy := sensitiveFieldPolicy(role, doctype); policy != nil {
				t.Errorf("%s: role %q must keep every field, got %d restrictions", doctype, role, len(policy))
			}
		}
	}
}

func TestBankAndSecretFieldsAreNotReadableBelowSuperAdmin(t *testing.T) {
	cases := []struct{ doctype, field string }{
		{"BankAccount", "account_number"},
		{"BankAccount", "ifsc_code"},
		{"Vendor", "bank_account_number"},
		{"Vendor", "bank_ifsc"},
		{"WebhookSubscription", "secret"},
		{"RoboticsIntegrationCredential", "api_key"},
		{"PIMCatalog", "share_token_hash"},
		{"PIMImportSchedule", "hook_token_hash"},
		{"Grievance", "description"},
		{"Grievance", "resolution_notes"},
	}
	for _, c := range cases {
		for _, role := range []string{RoleCashier, RoleStoreManager} {
			perm, ok := sensitiveFieldPolicy(role, c.doctype)[c.field]
			if !ok || perm.AllowRead {
				t.Errorf("%s.%s: role %q can still read it (entry present=%v)", c.doctype, c.field, role, ok)
			}
		}
	}
}

// The two deliberate write-only fields: readable by nobody below Super
// Admin, writable by any role that already passed the doctype gate. Losing
// either half of that is a real regression - denying the write breaks
// webhook creation and grievance submission, allowing the read is the leak.
func TestWriteOnlyFieldsStayWritable(t *testing.T) {
	for _, c := range []struct{ doctype, field string }{
		{"WebhookSubscription", "secret"},
		{"Grievance", "description"},
	} {
		for _, role := range []string{RoleCashier, RoleStoreManager, "Picker"} {
			perm, ok := sensitiveFieldPolicy(role, c.doctype)[c.field]
			if !ok {
				t.Fatalf("%s.%s: expected a policy entry for role %q", c.doctype, c.field, role)
			}
			if perm.AllowRead {
				t.Errorf("%s.%s: role %q must not read it", c.doctype, c.field, role)
			}
			if !perm.AllowWrite {
				t.Errorf("%s.%s: role %q must still be able to write it", c.doctype, c.field, role)
			}
		}
	}
}

func TestCostFieldsKeepStoreManagerAndDenyCashier(t *testing.T) {
	for _, c := range []struct{ doctype, field string }{
		{"Item", "cost_price"},
		{"Asset", "cost"},
		{"BOM", "standard_cost"},
		{"ProductionOrder", "actual_cost"},
		{"WorkCenter", "cost_per_hour"},
		{"Customer", "date_of_birth"},
	} {
		if _, restricted := sensitiveFieldPolicy(RoleStoreManager, c.doctype)[c.field]; restricted {
			t.Errorf("%s.%s: Store Manager must keep it (cost/margin and DOB are management data, not floor data)", c.doctype, c.field)
		}
		perm, ok := sensitiveFieldPolicy(RoleCashier, c.doctype)[c.field]
		if !ok || perm.AllowRead || perm.AllowWrite {
			t.Errorf("%s.%s: Cashier must get neither read nor write (entry present=%v, %+v)", c.doctype, c.field, ok, perm)
		}
	}
}

// Guard: a future entry cannot ship without a category and a stated reason.
// The reason is not decoration - it is what ExplainSensitiveFields shows an
// administrator asking "why is this hidden", which 47.1.6's preview builds on.
func TestEverySensitiveRuleIsDocumented(t *testing.T) {
	known := map[string]bool{
		SensitiveCategoryPayroll: true, SensitiveCategoryBank: true,
		SensitiveCategoryGrievance: true, SensitiveCategoryCost: true,
		SensitiveCategorySecret: true, SensitiveCategoryPersonal: true,
	}
	for _, doctype := range SensitiveFieldDoctypes() {
		for _, field := range SensitiveFieldNames(doctype) {
			rule := sensitiveFields[doctype][field]
			if !known[rule.Category] {
				t.Errorf("%s.%s: category %q is not one of the six declared categories", doctype, field, rule.Category)
			}
			if strings.TrimSpace(rule.Why) == "" {
				t.Errorf("%s.%s: no reason given - ExplainSensitiveFields would show an administrator a blank", doctype, field)
			}
		}
	}
}

func TestExplainSensitiveFields(t *testing.T) {
	rows := ExplainSensitiveFields(RoleCashier, "Payslip")
	if len(rows) != len(SensitiveFieldNames("Payslip")) {
		t.Fatalf("expected one row per sensitive field, got %d", len(rows))
	}
	for _, row := range rows {
		if row.CanRead || row.CanWrite {
			t.Errorf("%s: Cashier explanation says read=%v write=%v", row.Field, row.CanRead, row.CanWrite)
		}
		if row.Category != SensitiveCategoryPayroll || row.Reason == "" {
			t.Errorf("%s: explanation is missing category/reason (%+v)", row.Field, row)
		}
	}
	for _, row := range ExplainSensitiveFields(RoleSuperAdmin, "Payslip") {
		if !row.CanRead || !row.CanWrite {
			t.Errorf("%s: Super Admin must be explained as having full access", row.Field)
		}
	}
	if ExplainSensitiveFields(RoleCashier, "POSCart") != nil {
		t.Error("a doctype with no sensitive fields must explain as nil, not an empty-but-present list")
	}
}

// End-to-end through the real choke points, against the live tenant schema:
// the same three functions every HTTP read, form render, write, CSV import
// and PIM bulk edit already call. This is what actually proves a doctype
// with zero field_permissions rows is no longer wide open.
func TestSensitivePolicyAppliesThroughFieldPermissionChokePoints(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"

	payslip := map[string]interface{}{
		"code": "PS-TEST", "employee_id": "EMP-1", "status": "Draft",
		"gross_pay": 50000, "net_pay": 42000, "tds_deduction": 3000,
	}
	filtered, err := FilterFieldsForRole(tenantID, RoleCashier, "Payslip", payslip)
	if err != nil {
		t.Fatalf("FilterFieldsForRole: %v", err)
	}
	for _, hidden := range []string{"gross_pay", "net_pay", "tds_deduction"} {
		if _, present := filtered[hidden]; present {
			t.Errorf("Cashier still receives Payslip.%s over the generic doc API", hidden)
		}
	}
	for _, kept := range []string{"code", "employee_id", "status"} {
		if _, present := filtered[kept]; !present {
			t.Errorf("Payslip.%s was stripped - only the sensitive fields should be", kept)
		}
	}

	// Write, single-document and bulk-import path (same function).
	if err := RejectRestrictedFieldWrites(tenantID, RoleCashier, "Payslip", map[string]interface{}{"net_pay": 99999}); err == nil {
		t.Error("Cashier was allowed to write Payslip.net_pay")
	}
	if err := RejectRestrictedFieldWrites(tenantID, RoleCashier, "Grievance", map[string]interface{}{"description": "unsafe ladder in the stockroom"}); err != nil {
		t.Errorf("grievance self-submission must still work for any role: %v", err)
	}
	if err := RejectRestrictedFieldWrites(tenantID, RoleSuperAdmin, "Payslip", map[string]interface{}{"net_pay": 42000}); err != nil {
		t.Errorf("Super Admin must still be able to write payroll: %v", err)
	}

	// Form metadata: a field the role cannot read must not even render.
	meta := []FieldMeta{{Fieldname: "code"}, {Fieldname: "net_pay"}, {Fieldname: "status"}}
	visible, err := FilterFieldMetaForRole(tenantID, RoleCashier, "Payslip", meta)
	if err != nil {
		t.Fatalf("FilterFieldMetaForRole: %v", err)
	}
	for _, f := range visible {
		if f.Fieldname == "net_pay" {
			t.Error("Payslip.net_pay is still offered on the Cashier's form")
		}
	}
	if len(visible) != 2 {
		t.Errorf("expected 2 visible fields, got %d", len(visible))
	}
}
