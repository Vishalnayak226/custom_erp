package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// Stage 73.3.3: the inbox shows a viewer only the work that is theirs -
// approvals routed to their role that they did not submit, OMS queues when
// they may read orders, and PIM tasks assigned to them or to their role.
func TestTaskInboxAggregatesOnlyTheViewersWork(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec("DELETE FROM " + schema + ".approval_rules WHERE doctype = 'InboxTestDoc'")
	exec("INSERT INTO " + schema + ".approval_rules (doctype, min_amount, max_amount, required_role) VALUES ('InboxTestDoc', 0, NULL, 'Store Manager')")
	raw, _ := json.Marshal(map[string]interface{}{"code": "ITD-1", "total_amount": 500})
	exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ('ITD-1', 'InboxTestDoc', $1, 'Pending Approval', 'maker1')", raw)
	held, _ := json.Marshal(map[string]interface{}{"order_status": "On Hold", "payment_status": "Pending", "hold_reason": HoldPaymentPending})
	exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by, updated_at) VALUES ('SO-INBOX-1', 'SalesOrder', $1, 'On Hold', 'system', CURRENT_TIMESTAMP - INTERVAL '2 hours')", held)
	item, _ := json.Marshal(map[string]interface{}{"code": "SKU-INBOX", "name": "Inbox item"})
	exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ('SKU-INBOX', 'Item', $1, 'Active', 'system')", item)
	if _, err := CreatePIMTask(tenantID, "lead", PIMTaskRequest{Title: "Write copy", ItemCode: "SKU-INBOX", AssigneeRole: "Store Manager", DueDate: "2026-01-01"}); err != nil {
		t.Fatalf("CreatePIMTask: %v", err)
	}

	count := func(inbox *TaskInbox, source string) int {
		n := 0
		for _, item := range inbox.Items {
			if item.Source == source {
				n++
			}
		}
		return n
	}
	checker, err := ListTaskInbox(tenantID, TaskInboxScope{UserID: "checker1", Role: "Store Manager", OMS: true, PIM: true})
	if err != nil {
		t.Fatal(err)
	}
	if count(checker, "Approval") != 1 || count(checker, "Order desk") != 1 || count(checker, "Catalog") != 1 {
		t.Fatalf("checker should see 1 approval, 1 order-desk queue and 1 catalog task, got %+v", checker.Items)
	}
	if len(checker.Errors) != 0 {
		t.Fatalf("no source should fail: %v", checker.Errors)
	}
	if !checker.Items[0].Overdue {
		t.Fatalf("overdue work sorts first, got %+v", checker.Items[0])
	}

	withSelfApproval(t, tenantID, SelfApprovalNobody) // Stage 92: the submitter may not decide
	maker, _ := ListTaskInbox(tenantID, TaskInboxScope{UserID: "maker1", Role: "Store Manager"})
	if count(maker, "Approval") != 0 {
		t.Fatal("the maker must not be asked to approve their own document")
	}
	if count(maker, "Order desk")+count(maker, "Catalog") != 0 {
		t.Fatal("OMS and PIM rows appear only when the scope allows them")
	}
	cashier, _ := ListTaskInbox(tenantID, TaskInboxScope{UserID: "c1", Role: "Cashier", PIM: true})
	if count(cashier, "Approval") != 0 || count(cashier, "Catalog") != 0 {
		t.Fatalf("a role the slab and task do not name sees neither, got %+v", cashier.Items)
	}
	admin, _ := ListTaskInbox(tenantID, TaskInboxScope{UserID: "admin", Role: RoleSuperAdmin})
	if count(admin, "Approval") != 1 {
		t.Fatal("a Super Admin may decide any approval")
	}
}
