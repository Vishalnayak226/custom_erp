package engines

import (
	"custom_erp/db"
	"errors"
	"fmt"
	"testing"
	"time"
)

type poInsertFn func(id, doctype, status string, data map[string]interface{})

// poRulesTenant is a scratch client with a PO band (0 and up -> Accountant),
// two vendors in different groups, two items in different categories and
// users in the roles the tests need.
func poRulesTenant(t *testing.T) (tenantID, schema string, insert poInsertFn) {
	t.Helper()
	tenantID, schema, cleanup, ins := gateEntryTestTenant(t)
	t.Cleanup(cleanup)
	for u, role := range map[string]string{"pr_maker": RoleCashier, "pr_mgr": RoleStoreManager, "pr_cover": RoleCashier, "pr_acct": "Accountant"} {
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.users (id, username, password_hash, role, status, location_code) VALUES ($1, $1, 'x', $2, 'Active', 'HO')`, schema), u, role); err != nil {
			t.Fatalf("user %s: %v", u, err)
		}
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.approval_rules WHERE doctype = 'PurchaseOrder'`, schema)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.approval_rules (doctype, min_amount, required_role) VALUES ('PurchaseOrder', 0, 'Accountant')`, schema)); err != nil {
		t.Fatal(err)
	}
	ins("V-LOCAL", "Vendor", "Active", map[string]interface{}{"code": "V-LOCAL", "name": "Local", "vendor_group": "Local", "state": "Karnataka"})
	ins("V-FAR", "Vendor", "Active", map[string]interface{}{"code": "V-FAR", "name": "Far", "vendor_group": "Import", "state": "Karnataka"})
	ins("I-FAB", "Item", "Active", map[string]interface{}{"sku": "I-FAB", "name": "Cloth", "category": "Fabric"})
	// A new client is not cloned the default client's reason codes, so the ones the tests use are added.
	ins("GBP-URGENT", "ReasonCode", "Active", map[string]interface{}{"code": "GBP-URGENT", "description": "Urgent", "category": ReasonReceivedBeforePO, "status": "Active"})
	ins("GRN-REJ-QUALITY", "ReasonCode", "Active", map[string]interface{}{"code": "GRN-REJ-QUALITY", "description": "Quality", "category": ReasonGRNRejection, "status": "Active"})
	ins("I-MISC", "Item", "Active", map[string]interface{}{"sku": "I-MISC", "name": "Misc", "category": "Misc"})
	return tenantID, schema, ins
}

func poDoc(vendor, sku string, amount float64) map[string]interface{} {
	return map[string]interface{}{"vendor": vendor, "total_amount": amount, "base_total_amount": amount,
		"items": fmt.Sprintf(`[{"sku":"%s","qty":1,"unit_price":%v}]`, sku, amount)}
}

func addRoutingRule(insert poInsertFn, id string, d map[string]interface{}) {
	if _, ok := d["status"]; !ok {
		d["status"] = "Active"
	}
	d["rule_name"] = id
	insert(id, "PORoutingRule", "Active", d)
}

// 148.39: no rule on file = today's routing exactly; a rule routes by vendor
// group and item category, the most specific wins, and a PO outside every
// rule's band falls back to the bands.
func TestStage14839NoRuleIsTodaysRoutingThenMostSpecificWins(t *testing.T) {
	tenantID, _, insert := poRulesTenant(t)
	role := func(vendor, sku string, amount float64) string {
		r, err := requiredApproverRoleFor(tenantID, "PurchaseOrder", poDoc(vendor, sku, amount), amount)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	if got := role("V-LOCAL", "I-FAB", 500); got != "Accountant" {
		t.Fatalf("no rule: %q, want the band's Accountant", got)
	}
	if got, _ := requiredApproverRole(tenantID, "PurchaseOrder", 500); got != "Accountant" {
		t.Fatalf("band: %q", got)
	}
	addRoutingRule(insert, "R-LOCAL", map[string]interface{}{"min_amount": 0, "max_amount": 10000, "vendor_group": "Local", "required_role": RoleStoreManager})
	addRoutingRule(insert, "R-LOCALFAB", map[string]interface{}{"min_amount": 0, "max_amount": 10000, "vendor_group": "Local", "item_category": "Fabric", "required_role": RoleCashier})
	addRoutingRule(insert, "R-OFF", map[string]interface{}{"min_amount": 0, "vendor_group": "Import", "required_role": RoleCashier, "status": "Inactive"})
	for _, c := range []struct {
		vendor, sku string
		amount      float64
		want        string
	}{
		{"V-LOCAL", "I-MISC", 500, RoleStoreManager},
		{"V-LOCAL", "I-FAB", 500, RoleCashier},
		{"V-FAR", "I-FAB", 500, "Accountant"},
		{"V-LOCAL", "I-FAB", 20000, "Accountant"},
		{"V-FAR", "I-MISC", 500, "Accountant"},
	} {
		if got := role(c.vendor, c.sku, c.amount); got != c.want {
			t.Errorf("%s/%s/%v: %q, want %q", c.vendor, c.sku, c.amount, got, c.want)
		}
	}
	// Another doctype never reads PO rules.
	want, _ := requiredApproverRole(tenantID, "Campaign", 500)
	if got, _ := requiredApproverRoleFor(tenantID, "Campaign", poDoc("V-LOCAL", "I-FAB", 500), 500); got != want {
		t.Fatalf("a PO rule leaked to another doctype: %q, want %q", got, want)
	}
}

func poDraft(t *testing.T, schema, id, vendor, sku string, amount float64, createdBy string, insert poInsertFn) {
	t.Helper()
	d := poDoc(vendor, sku, amount)
	d["code"] = id
	insert(id, "PurchaseOrder", "Draft", d)
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET created_by = $2 WHERE id = $1`, schema), id, createdBy); err != nil {
		t.Fatal(err)
	}
}

// A rule routes the real submit and decide path, and a role the rule does not
// name is refused as any wrong approver is today.
func TestStage14839RuleRoutesSubmitAndDecide(t *testing.T) {
	tenantID, schema, insert := poRulesTenant(t)
	addRoutingRule(insert, "R-LOCAL", map[string]interface{}{"min_amount": 0, "vendor_group": "Local", "required_role": RoleStoreManager})
	poDraft(t, schema, "PO-RT1", "V-LOCAL", "I-MISC", 500, "pr_maker", insert)
	poDraft(t, schema, "PO-RT2", "V-FAR", "I-MISC", 500, "pr_maker", insert)
	for _, id := range []string{"PO-RT1", "PO-RT2"} {
		if err := SubmitForApproval(tenantID, "PurchaseOrder", id, "pr_maker", RoleCashier); err != nil {
			t.Fatalf("submit %s: %v", id, err)
		}
	}
	if err := DecideApproval(tenantID, "PurchaseOrder", "PO-RT1", "pr_acct", "Accountant", "HO", "Approved", ""); !errors.Is(err, ErrApprovalRoleMismatch) {
		t.Fatalf("Accountant on a Local-vendor PO: %v, want a role mismatch", err)
	}
	if err := DecideApproval(tenantID, "PurchaseOrder", "PO-RT1", "pr_mgr", RoleStoreManager, "HO", "Approved", ""); err != nil {
		t.Fatalf("the rule's role: %v", err)
	}
	// No rule for the other vendor: the bands, exactly as before.
	if err := DecideApproval(tenantID, "PurchaseOrder", "PO-RT2", "pr_mgr", RoleStoreManager, "HO", "Approved", ""); !errors.Is(err, ErrApprovalRoleMismatch) {
		t.Fatalf("Store Manager on a banded PO: %v, want a role mismatch", err)
	}
	if err := DecideApproval(tenantID, "PurchaseOrder", "PO-RT2", "pr_acct", "Accountant", "HO", "Approved", ""); err != nil {
		t.Fatalf("the band's role: %v", err)
	}
}

// After N days waiting the rule's escalation role may decide too; the existing
// reminder job stamps it once and leaves the wait clock alone.
func TestStage14839EscalationAfterDaysThroughTheReminderJob(t *testing.T) {
	tenantID, schema, insert := poRulesTenant(t)
	addRoutingRule(insert, "R-ESC", map[string]interface{}{"min_amount": 0, "vendor_group": "Local", "required_role": RoleStoreManager, "escalate_after_days": 2, "escalate_role": "Accountant"})
	poDraft(t, schema, "PO-ES1", "V-LOCAL", "I-MISC", 500, "pr_maker", insert)
	poDraft(t, schema, "PO-ES2", "V-LOCAL", "I-MISC", 500, "pr_maker", insert)
	for _, id := range []string{"PO-ES1", "PO-ES2"} {
		if err := SubmitForApproval(tenantID, "PurchaseOrder", id, "pr_maker", RoleCashier); err != nil {
			t.Fatal(err)
		}
	}
	for id, days := range map[string]int{"PO-ES1": 3, "PO-ES2": 1} {
		if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET updated_at = NOW() - make_interval(days => $2) WHERE id = $1`, schema), id, days); err != nil {
			t.Fatal(err)
		}
	}
	if err := DecideApproval(tenantID, "PurchaseOrder", "PO-ES1", "pr_acct", "Accountant", "HO", "Approved", ""); !errors.Is(err, ErrApprovalRoleMismatch) {
		t.Fatalf("before escalation: %v, want a role mismatch", err)
	}
	for i := 0; i < 2; i++ { // the second run must not repeat
		if err := RunPOApprovalReminders(tenantID, schema); err != nil {
			t.Fatal(err)
		}
	}
	if n := notificationLogCount(t, schema, "PO Approval Escalation", "PO-ES1"); n != 1 {
		t.Errorf("PO-ES1 escalation notice %d times, want once", n)
	}
	if n := notificationLogCount(t, schema, "PO Approval Escalation", "PO-ES2"); n != 0 {
		t.Errorf("PO-ES2 has waited 1 day, too soon: %d", n)
	}
	var esc string
	var aged bool
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'escalated_to_role',''), updated_at < NOW() - interval '2 days' FROM %s.documents WHERE id = 'PO-ES1'`, schema)).Scan(&esc, &aged); err != nil || esc != "Accountant" || !aged {
		t.Fatalf("PO-ES1 escalated_to_role=%q aged=%v err=%v, want Accountant and an untouched clock", esc, aged, err)
	}
	if err := DecideApproval(tenantID, "PurchaseOrder", "PO-ES2", "pr_acct", "Accountant", "HO", "Approved", ""); !errors.Is(err, ErrApprovalRoleMismatch) {
		t.Fatalf("PO-ES2 not escalated: %v, want a role mismatch", err)
	}
	if err := DecideApproval(tenantID, "PurchaseOrder", "PO-ES1", "pr_acct", "Accountant", "HO", "Approved", ""); err != nil {
		t.Fatalf("after escalation: %v", err)
	}
	// An edit that sends it back for approval starts a fresh, un-escalated round.
	data, _, _, _ := fetchDocument(tenantID, "PurchaseOrder", "PO-ES1")
	if err := ResetToPendingOnEdit(tenantID, "PurchaseOrder", "PO-ES1", "pr_maker", RoleCashier, data); err != nil {
		t.Fatal(err)
	}
	if again, _, _, _ := fetchDocument(tenantID, "PurchaseOrder", "PO-ES1"); strField(again, "escalated_to_role") != "" {
		t.Fatalf("escalation survived the reset")
	}
}

// A delegate covers an approver inside the dates only, and never decides a PO
// they raised themselves, whatever approvals.self_approval says.
func TestStage14839DelegateCoversApproverInDatesOnly(t *testing.T) {
	tenantID, schema, insert := poRulesTenant(t)
	if err := SetSetting(tenantID, SettingSelfApproval, SelfApprovalAnyone, "test"); err != nil {
		t.Fatal(err)
	}
	addRoutingRule(insert, "R-LOCAL", map[string]interface{}{"min_amount": 0, "vendor_group": "Local", "required_role": RoleStoreManager})
	today, _ := time.Parse("2006-01-02", BusinessToday())
	day := func(n int) string { return today.AddDate(0, 0, n).Format("2006-01-02") }
	insert("DL-NOW", "ApprovalDelegate", "Active", map[string]interface{}{"approver_user": "pr_mgr", "delegate_user": "pr_cover", "from_date": day(-1), "to_date": day(1), "status": "Active"})
	insert("DL-OLD", "ApprovalDelegate", "Active", map[string]interface{}{"approver_user": "pr_mgr", "delegate_user": "pr_maker", "from_date": day(-10), "to_date": day(-5), "status": "Active"})
	for _, id := range []string{"PO-DL1", "PO-DL3", "PO-DL4", "PO-DL5"} {
		by := "pr_maker"
		if id == "PO-DL3" {
			by = "pr_cover"
		}
		poDraft(t, schema, id, "V-LOCAL", "I-MISC", 500, by, insert)
		if err := SubmitForApproval(tenantID, "PurchaseOrder", id, by, RoleCashier); err != nil {
			t.Fatal(err)
		}
	}
	// A cover whose dates ended cannot decide.
	if err := DecideApproval(tenantID, "PurchaseOrder", "PO-DL5", "pr_maker", RoleCashier, "HO", "Approved", ""); err == nil {
		t.Fatalf("a delegate whose dates ended decided a PO")
	}
	// In the dates: the cover decides it.
	if err := DecideApproval(tenantID, "PurchaseOrder", "PO-DL1", "pr_cover", RoleCashier, "HO", "Approved", ""); err != nil {
		t.Fatalf("delegate in dates: %v", err)
	}
	// They raised this one: refused although the cover is in force.
	if err := DecideApproval(tenantID, "PurchaseOrder", "PO-DL3", "pr_cover", RoleCashier, "HO", "Approved", ""); err == nil {
		t.Fatalf("a delegate approved a PO they raised")
	}
	// Switched off, the cover stops at once.
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || '{"status":"Inactive"}' WHERE id = 'DL-NOW'`, schema)); err != nil {
		t.Fatal(err)
	}
	if err := DecideApproval(tenantID, "PurchaseOrder", "PO-DL4", "pr_cover", RoleCashier, "HO", "Approved", ""); err == nil {
		t.Fatalf("an inactive delegate decided a PO")
	}
	var st string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT status FROM %s.documents WHERE id = 'PO-DL3'`, schema)).Scan(&st)
	if st != "Pending Approval" {
		t.Fatalf("the PO the delegate raised is %q, want still Pending Approval", st)
	}
}
