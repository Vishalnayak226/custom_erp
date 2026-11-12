package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
)

// withSelfApproval sets the tenant's SettingSelfApproval for one test and puts
// the default back when it ends.
func withSelfApproval(t *testing.T, tenantID, value string) {
	t.Helper()
	if err := SetSetting(tenantID, SettingSelfApproval, value, "test"); err != nil {
		t.Fatalf("set %s=%s: %v", SettingSelfApproval, value, err)
	}
	t.Cleanup(func() { _ = SetSetting(tenantID, SettingSelfApproval, SelfApprovalAnyone, "test") })
}

// Stage 92: who may decide their own document, per setting - and the two
// second-admin flows never, whatever the setting says.
func TestStage92SubmitterMayDecideOwn(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	cases := []struct {
		setting, doctype, role string
		want                   bool
	}{
		{SelfApprovalAnyone, "PurchaseOrder", "Store Manager", true},
		{SelfApprovalAnyone, "PurchaseOrder", RoleSuperAdmin, true},
		{SelfApprovalAnyone, "PasswordResetRequest", RoleSuperAdmin, false},
		{SelfApprovalAnyone, "ItemTreatmentCorrection", RoleSuperAdmin, false},
		{SelfApprovalAdmins, "PurchaseOrder", "Store Manager", false},
		{SelfApprovalAdmins, "PurchaseOrder", "HR/Admin", true},
		{SelfApprovalNobody, "PurchaseOrder", RoleSuperAdmin, false},
	}
	for _, c := range cases {
		withSelfApproval(t, tenantID, c.setting)
		if got := SubmitterMayDecideOwn(tenantID, c.doctype, c.role); got != c.want {
			t.Errorf("setting=%s doctype=%s role=%s: got %v, want %v", c.setting, c.doctype, c.role, got, c.want)
		}
	}
}

// Stage 92: with the default setting the submitter sees their own document in
// My work and can decide it; the decision is marked in the approval history.
// Rejected, so no voucher reaches the GL.
func TestStage92SubmitterDecidesOwnByDefault(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".approval_log WHERE doctype = 'JournalVoucher' AND document_id IN (SELECT id FROM " + schema + ".documents WHERE doctype = 'JournalVoucher' AND data->>'narration' LIKE 'TEST-JV92%')")
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'JournalVoucher' AND data->>'narration' LIKE 'TEST-JV92%'")
	}
	cleanup()
	defer cleanup()
	withSelfApproval(t, tenantID, SelfApprovalAnyone)

	voucherID, err := CreateJournalVoucher(tenantID, "2026-07-25", "TEST-JV92 own decision", []JournalVoucherLine{{AccountCode: "5100", Debit: 92}, {AccountCode: "1100", Credit: 92}}, "admin")
	if err != nil {
		t.Fatalf("CreateJournalVoucher: %v", err)
	}
	if err := SubmitForApproval(tenantID, "JournalVoucher", voucherID, "admin", RoleSuperAdmin); err != nil {
		t.Fatalf("SubmitForApproval: %v", err)
	}

	inbox, err := ListTaskInbox(tenantID, TaskInboxScope{UserID: "admin", Role: RoleSuperAdmin})
	if err != nil {
		t.Fatalf("ListTaskInbox: %v", err)
	}
	listed := false
	for _, item := range inbox.Items {
		if item.Source == "Approval" && item.DocID == voucherID {
			listed = true
		}
	}
	if !listed {
		t.Fatal("My work must list the submitter's own document when they may decide it")
	}

	if err := DecideApproval(tenantID, "JournalVoucher", voucherID, "admin", RoleSuperAdmin, "", "Rejected", "wrong account"); err != nil {
		t.Fatalf("own decision refused: %v", err)
	}
	var status, comment string
	if err := db.DB.QueryRow("SELECT status FROM "+schema+".documents WHERE id = $1", voucherID).Scan(&status); err != nil {
		t.Fatalf("status: %v", err)
	}
	if status != "Rejected" {
		t.Fatalf("status = %q, want Rejected", status)
	}
	if err := db.DB.QueryRow("SELECT COALESCE(comment, '') FROM "+schema+".approval_log WHERE doctype = 'JournalVoucher' AND document_id = $1 AND action = 'Rejected'", voucherID).Scan(&comment); err != nil {
		t.Fatalf("approval_log: %v", err)
	}
	if !strings.HasPrefix(comment, "[decided by the submitter]") || !strings.Contains(comment, "wrong account") {
		t.Fatalf("approval_log comment = %q, want the submitter marker and the reason", comment)
	}
}
