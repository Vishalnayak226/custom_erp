package engines

import (
	"custom_erp/db"
	"testing"
)

func notificationLogCount(t *testing.T, schema, event, orderID string) int {
	t.Helper()
	var n int
	if err := db.DB.QueryRow("SELECT count(*) FROM "+schema+".documents WHERE doctype = 'NotificationLog' AND data->>'event' = $1 AND data->>'order_id' = $2", event, orderID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Stage 146.4.6(a): a PO waiting for approval gets a reminder, then an
// escalation, once each, after the hours the client set. 0 hours = off.
func TestStage14646aPOApprovalReminderThenEscalation(t *testing.T) {
	tenantID, schema, cleanup, insert := gateEntryTestTenant(t)
	defer cleanup()
	insert("PO-AR1", "PurchaseOrder", "Pending Approval", map[string]interface{}{"code": "PO-AR1", "vendor": "V-1", "total_amount": 100.0})
	insert("PO-AR2", "PurchaseOrder", "Pending Approval", map[string]interface{}{"code": "PO-AR2", "vendor": "V-1", "total_amount": 100.0})
	insert("PO-AR3", "PurchaseOrder", "Approved", map[string]interface{}{"code": "PO-AR3", "vendor": "V-1"})
	for id, h := range map[string]int{"PO-AR1": 10, "PO-AR2": 1, "PO-AR3": 10} {
		if _, err := db.DB.Exec("UPDATE "+schema+".documents SET updated_at = NOW() - make_interval(hours => $2) WHERE id = $1", id, h); err != nil {
			t.Fatal(err)
		}
	}
	// Off by default: nothing happens.
	if err := RunPOApprovalReminders(tenantID, schema); err != nil {
		t.Fatal(err)
	}
	if notificationLogCount(t, schema, "PO Approval Reminder", "PO-AR1") != 0 {
		t.Fatalf("settings are off by default - no reminder")
	}
	_ = SetSetting(tenantID, SettingPOApprovalReminderHours, "4", "admin")
	_ = SetSetting(tenantID, SettingPOApprovalEscalationHours, "8", "admin")
	for i := 0; i < 2; i++ { // the second run must not repeat
		if err := RunPOApprovalReminders(tenantID, schema); err != nil {
			t.Fatal(err)
		}
	}
	if n := notificationLogCount(t, schema, "PO Approval Reminder", "PO-AR1"); n != 1 {
		t.Errorf("PO-AR1 reminder once, got %d", n)
	}
	if n := notificationLogCount(t, schema, "PO Approval Escalation", "PO-AR1"); n != 1 {
		t.Errorf("PO-AR1 escalation once, got %d", n)
	}
	if notificationLogCount(t, schema, "PO Approval Reminder", "PO-AR2") != 0 {
		t.Errorf("PO-AR2 has waited 1 hour - too soon")
	}
	if notificationLogCount(t, schema, "PO Approval Reminder", "PO-AR3") != 0 {
		t.Errorf("an approved PO is not reminded")
	}
}
