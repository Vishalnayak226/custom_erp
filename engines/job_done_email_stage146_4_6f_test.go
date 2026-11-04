package engines

import (
	"custom_erp/db"
	"testing"
)

// Stage 146.4.6(f): an import or export of many rows tells the person who
// started it - an in-app notification and an enqueued outbox email.
func TestStage14646fLongJobDoneEmail(t *testing.T) {
	tenantID, schema, cleanup, _ := gateEntryTestTenant(t)
	defer cleanup()
	if _, err := db.DB.Exec("INSERT INTO " + schema + ".users (id, username, password_hash, email, role) VALUES ('u-jd', 'jd', 'x', 'jd@example.com', 'Store Manager')"); err != nil {
		t.Fatal(err)
	}
	outbox := func() int {
		var n int
		_ = db.DB.QueryRow("SELECT count(*) FROM " + schema + ".integration_event_outbox WHERE event_name = 'email.send' AND payload->>'to' = 'jd@example.com'").Scan(&n)
		return n
	}
	NotifyLongJobDone(tenantID, "u-jd", "Import", "Items.csv", 5000)
	if outbox() != 0 || notificationLogCount(t, schema, "Long Job Finished", "Items.csv") != 0 {
		t.Fatalf("off by default")
	}
	_ = SetSetting(tenantID, SettingJobDoneMinRows, "1000", "admin")
	NotifyLongJobDone(tenantID, "u-jd", "Import", "Small.csv", 50)
	if outbox() != 0 {
		t.Fatalf("a short job sends nothing")
	}
	NotifyLongJobDone(tenantID, "u-jd", "Import", "Items.csv", 5000)
	if outbox() != 1 {
		t.Errorf("one email enqueued, got %d", outbox())
	}
	if notificationLogCount(t, schema, "Long Job Finished", "Items.csv") != 1 {
		t.Errorf("one in-app notification")
	}
	NotifyLongJobDone(tenantID, "u-nobody", "Export", "X", 5000) // no such user: the in-app one still goes, no email
	if outbox() != 1 {
		t.Errorf("no address, no email")
	}
}
