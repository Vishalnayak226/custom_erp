package engines

import (
	"custom_erp/db"
	"errors"
	"fmt"
	"testing"
	"time"
)

// approvalCoverTenant is poRulesTenant with purchase requisitions routed to
// Store Manager, the non-PO document the 73.4.9 tests use.
func approvalCoverTenant(t *testing.T) (tenantID, schema string, insert poInsertFn) {
	t.Helper()
	tenantID, schema, insert = poRulesTenant(t)
	if err := SetSetting(tenantID, SettingSelfApproval, SelfApprovalAnyone, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`DELETE FROM %[1]s.approval_rules WHERE doctype = 'PurchaseRequisition';
		INSERT INTO %[1]s.approval_rules (doctype, min_amount, required_role) VALUES ('PurchaseRequisition', 0, '%[2]s')`, schema, RoleStoreManager)); err != nil {
		t.Fatal(err)
	}
	return tenantID, schema, insert
}

// submitRequisition raises a requisition as createdBy, submits it, and backdates
// its wait by the given hours.
func submitRequisition(t *testing.T, tenantID, schema, id, createdBy string, waitedHours int, insert poInsertFn) {
	t.Helper()
	insert(id, "PurchaseRequisition", "Draft", map[string]interface{}{"code": id, "total_amount": 500.0, "base_total_amount": 500.0})
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET created_by = $2 WHERE id = $1`, schema), id, createdBy); err != nil {
		t.Fatal(err)
	}
	if err := SubmitForApproval(tenantID, "PurchaseRequisition", id, createdBy, RoleCashier); err != nil {
		t.Fatalf("submit %s: %v", id, err)
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET updated_at = NOW() - make_interval(hours => $2) WHERE id = $1`, schema), id, waitedHours); err != nil {
		t.Fatal(err)
	}
}

func inboxHas(t *testing.T, tenantID, userID, role, docID string) bool {
	t.Helper()
	inbox, err := ListTaskInbox(tenantID, TaskInboxScope{UserID: userID, Role: role, Location: "HO"})
	if err != nil {
		t.Fatal(err)
	}
	if e := inbox.Errors["approvals"]; e != "" {
		t.Fatalf("inbox approvals: %s", e)
	}
	for _, it := range inbox.Items {
		if it.Source == "Approval" && it.DocID == docID {
			return true
		}
	}
	return false
}

// 73.4.9: an approver's delegate covers every kind of approval, not only POs -
// in My work and at the decision - inside the dates, never for a document they
// raised, and never for one that always needs a second admin.
func TestApprovalDelegateCoversEveryDocument(t *testing.T) {
	tenantID, schema, insert := approvalCoverTenant(t)
	today, _ := time.Parse("2006-01-02", BusinessToday())
	day := func(n int) string { return today.AddDate(0, 0, n).Format("2006-01-02") }
	insert("DL-PR", "ApprovalDelegate", "Active", map[string]interface{}{"approver_user": "pr_mgr", "delegate_user": "pr_cover", "from_date": day(-1), "to_date": day(1), "status": "Active"})
	submitRequisition(t, tenantID, schema, "PR-DL1", "pr_maker", 0, insert)
	submitRequisition(t, tenantID, schema, "PR-DL2", "pr_cover", 0, insert)

	if !inboxHas(t, tenantID, "pr_cover", RoleCashier, "PR-DL1") {
		t.Errorf("the delegate's My work should list the requisition they cover")
	}
	if inboxHas(t, tenantID, "pr_cover", RoleCashier, "PR-DL2") {
		t.Errorf("the delegate's My work lists a requisition they raised")
	}
	if inboxHas(t, tenantID, "pr_maker", RoleCashier, "PR-DL1") {
		t.Errorf("a cashier with no cover sees a Store Manager's requisition")
	}
	if err := DecideApproval(tenantID, "PurchaseRequisition", "PR-DL2", "pr_cover", RoleCashier, "HO", "Approved", ""); err == nil {
		t.Fatalf("a delegate approved a requisition they raised")
	}
	if err := DecideApproval(tenantID, "PurchaseRequisition", "PR-DL1", "pr_acct", "Accountant", "HO", "Approved", ""); !errors.Is(err, ErrApprovalRoleMismatch) {
		t.Fatalf("someone with no cover decided: %v", err)
	}
	if err := DecideApproval(tenantID, "PurchaseRequisition", "PR-DL1", "pr_cover", RoleCashier, "HO", "Approved", ""); err != nil {
		t.Fatalf("delegate in dates: %v", err)
	}
	// A document that always needs a second admin is never covered.
	for _, dt := range []string{"PasswordResetRequest", "ItemTreatmentCorrection"} {
		if mayAlsoDecide(tenantID, dt, "X-1", map[string]interface{}{}, "someone", "pr_cover", RoleCashier, RoleStoreManager) {
			t.Errorf("a delegate may decide a %s", dt)
		}
	}
}

// 73.4.9: any approval-gated document still waiting gets the reminder, then
// the escalation, once each; from then the escalation role may decide it too.
// Off by default, purchase orders keep their own settings, and the role must
// exist.
func TestApprovalReminderThenEscalationForAnyDocument(t *testing.T) {
	tenantID, schema, insert := approvalCoverTenant(t)
	submitRequisition(t, tenantID, schema, "PR-ES1", "pr_maker", 10, insert)
	submitRequisition(t, tenantID, schema, "PR-ES2", "pr_maker", 1, insert)
	insert("PO-ES9", "PurchaseOrder", "Pending Approval", map[string]interface{}{"code": "PO-ES9", "total_amount": 100.0})
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET updated_at = NOW() - make_interval(hours => 10) WHERE id = 'PO-ES9'`, schema)); err != nil {
		t.Fatal(err)
	}

	// Off by default: no notice, and the Accountant may not decide.
	if err := RunApprovalReminders(tenantID, schema); err != nil {
		t.Fatal(err)
	}
	if notificationLogCount(t, schema, "Approval Reminder", "PR-ES1") != 0 {
		t.Fatalf("settings are off by default - no reminder")
	}
	if err := DecideApproval(tenantID, "PurchaseRequisition", "PR-ES1", "pr_acct", "Accountant", "HO", "Approved", ""); !errors.Is(err, ErrApprovalRoleMismatch) {
		t.Fatalf("escalation off, yet the Accountant decided: %v", err)
	}

	var verr *ValidationError
	if err := SetSetting(tenantID, SettingApprovalEscalateToRole, "Nobody Here", "admin"); !errors.As(err, &verr) || verr.Code != approvalEscalateRoleCode {
		t.Fatalf("an unknown escalation role must be refused with %s, got %v", approvalEscalateRoleCode, err)
	}
	for k, v := range map[string]string{SettingApprovalReminderHours: "4", SettingApprovalEscalationHours: "8", SettingApprovalEscalateToRole: "Accountant"} {
		if err := SetSetting(tenantID, k, v, "admin"); err != nil {
			t.Fatalf("%s: %v", k, err)
		}
	}
	for i := 0; i < 2; i++ { // the second run must not repeat
		if err := RunApprovalReminders(tenantID, schema); err != nil {
			t.Fatal(err)
		}
	}
	if n := notificationLogCount(t, schema, "Approval Reminder", "PR-ES1"); n != 1 {
		t.Errorf("PR-ES1 reminder once, got %d", n)
	}
	if n := notificationLogCount(t, schema, "Approval Escalation", "PR-ES1"); n != 1 {
		t.Errorf("PR-ES1 escalation once, got %d", n)
	}
	if notificationLogCount(t, schema, "Approval Reminder", "PR-ES2") != 0 {
		t.Errorf("PR-ES2 has waited 1 hour - too soon")
	}
	if notificationLogCount(t, schema, "Approval Reminder", "PO-ES9")+notificationLogCount(t, schema, "Approval Escalation", "PO-ES9") != 0 {
		t.Errorf("a purchase order goes by its own PO settings, not these")
	}

	// The escalation role sees and decides the one that waited past 8 hours only.
	if !inboxHas(t, tenantID, "pr_acct", "Accountant", "PR-ES1") {
		t.Errorf("the escalation role's My work should list PR-ES1")
	}
	if inboxHas(t, tenantID, "pr_acct", "Accountant", "PR-ES2") {
		t.Errorf("PR-ES2 has not waited long enough to escalate")
	}
	if err := DecideApproval(tenantID, "PurchaseRequisition", "PR-ES2", "pr_acct", "Accountant", "HO", "Approved", ""); !errors.Is(err, ErrApprovalRoleMismatch) {
		t.Fatalf("decided before the escalation wait: %v", err)
	}
	if err := DecideApproval(tenantID, "PurchaseRequisition", "PR-ES1", "pr_acct", "Accountant", "HO", "Approved", ""); err != nil {
		t.Fatalf("escalation role after the wait: %v", err)
	}
}
