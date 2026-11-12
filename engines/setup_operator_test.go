package engines

import (
	"custom_erp/db"
	"testing"
)

// Stage 132.15: the Setup Operator - the account the Guide set-up assistant
// signs in with - sets a client up and does nothing else: no delete, no
// payment, no users / roles / security, and no SoD conflict of its own.
func TestSetupOperatorTemplate(t *testing.T) {
	tmpl, ok := RoleTemplateFor(RoleSetupOperator)
	if !ok {
		t.Fatalf("no %s template", RoleSetupOperator)
	}
	if HasFullAccess(RoleSetupOperator) || IsSuperAdmin(RoleSetupOperator) {
		t.Fatalf("%s must never be full access", RoleSetupOperator)
	}
	for module, level := range tmpl.Modules {
		if level == AccessManage {
			t.Errorf("module %s is Manage - a set-up account never deletes", module)
		}
	}
	for doctype, level := range tmpl.Doctypes {
		if level == AccessManage {
			t.Errorf("%s is Manage - a set-up account never deletes", doctype)
		}
	}
	for _, d := range []string{"VendorInvoice", "PaymentProposal"} {
		if tmpl.Doctypes[d] > AccessRead {
			t.Errorf("%s is %s - paying is not setting up", d, tmpl.Doctypes[d])
		}
	}
	for _, d := range []string{"Payslip", "EmployeeLoan", "PasswordResetRequest"} {
		if level, set := tmpl.Doctypes[d]; !set || level != AccessNone {
			t.Errorf("%s must be AccessNone, got %v (set=%v)", d, level, set)
		}
	}
	for _, c := range []string{"admin.setup-workbook", "admin.settings"} {
		if !RoleHasCapability(RoleSetupOperator, c) {
			t.Errorf("missing capability %s", c)
		}
	}
	for _, c := range []string{"admin.users", "admin.roles", "admin.role-permissions", "audit.logs", "hr.payroll", "finance.statements"} {
		if RoleHasCapability(RoleSetupOperator, c) {
			t.Errorf("holds %s - not a set-up capability", c)
		}
	}

	// Its own grants, expanded against the tenant's doctypes the way applying
	// the template does, raise no SoD conflict - and it can set up vendors.
	db.InitDB(testConnStr())
	grants, err := TemplateGrants("default", tmpl)
	if err != nil {
		t.Fatalf("TemplateGrants: %v", err)
	}
	if grants["Vendor"] < AccessOperate || grants["Location"] < AccessOperate || grants["Printer"] < AccessOperate {
		t.Errorf("cannot set up vendors / locations / printers: %v %v %v", grants["Vendor"], grants["Location"], grants["Printer"])
	}
	for doctype, level := range grants {
		if level == AccessManage {
			t.Errorf("%s expands to Manage", doctype)
		}
	}
	access := EffectiveAccess{Role: RoleSetupOperator, Grants: grants, ApproverFor: map[string]bool{}, Capabilities: tmpl.Capabilities}
	for _, f := range DetectSoDConflicts(access) {
		t.Errorf("SoD conflict %s: %s / %s", f.ConflictID, f.EvidenceA, f.EvidenceB)
	}
}

func TestSettingIsAdminOnly(t *testing.T) {
	for key, want := range map[string]bool{
		"security.loyalty_otp_validity_minutes":   true,
		"platform.integration_log_retention_days": true,
		"procurement.po_edit_window_days":         false,
		"no.such.setting":                         true, // unregistered fails closed
	} {
		if got := SettingIsAdminOnly(key); got != want {
			t.Errorf("SettingIsAdminOnly(%q) = %v, want %v", key, got, want)
		}
	}
}
