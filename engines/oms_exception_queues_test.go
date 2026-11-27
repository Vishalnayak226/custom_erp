package engines

import (
	"custom_erp/db"
	"fmt"
	"testing"
	"time"
)

// Item 267.3: a queue longer than omsQueueRowCap keeps its oldest rows - the
// top of the backlog - and says it is capped. Before the fix the cap was an
// unordered LIMIT, so the oldest row could be the one left out.
func TestOMSExceptionQueueCapKeepsOldest(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	// omsQueueRowCap newer channel changes first, then the oldest one inserted
	// last, so a scan in storage order meets it after the cap.
	if _, err := db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.documents (id, doctype, data, status, created_by, updated_at)
		SELECT 'SO-CAP-' || g, 'SalesOrder', '{"channel":"Shop","channel_change_pending":"Edited after picking"}'::jsonb, 'Delivered', 'system', CURRENT_TIMESTAMP
		  FROM generate_series(1, %d) g`, schema, omsQueueRowCap)); err != nil {
		t.Fatalf("seed newer rows: %v", err)
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.documents (id, doctype, data, status, created_by, updated_at)
		VALUES ('SO-CAP-OLDEST', 'SalesOrder', '{"channel":"Shop","channel_change_pending":"Cancelled after picking"}'::jsonb, 'Delivered', 'system', CURRENT_TIMESTAMP - INTERVAL '10 days')`, schema)); err != nil {
		t.Fatalf("seed oldest row: %v", err)
	}

	rows, capped, err := OMSExceptionQueueCapped(tenantID, "channel")
	if err != nil {
		t.Fatalf("channel queue: %v", err)
	}
	if len(rows) != omsQueueRowCap || !capped {
		t.Fatalf("want %d rows and capped, got %d rows, capped=%v", omsQueueRowCap, len(rows), capped)
	}
	if rows[0]["order_id"] != "SO-CAP-OLDEST" {
		t.Fatalf("the oldest change must lead the capped queue, got %v", rows[0]["order_id"])
	}
	if age := time.Since(rows[0]["since"].(time.Time)); age < 9*24*time.Hour {
		t.Fatalf("first row should be ten days old, got %v", age)
	}

	// The console's count says there are more; a short queue does not.
	for _, c := range OMSExceptionQueueCounts(tenantID) {
		switch c["id"] {
		case "channel":
			if c["more"] != true || c["count"].(int) != omsQueueRowCap {
				t.Fatalf("channel count %v", c)
			}
		case "payment":
			if c["more"] != false {
				t.Fatalf("payment queue is not capped: %v", c)
			}
		}
	}
}
