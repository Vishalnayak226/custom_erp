package engines

import (
	"custom_erp/db"
	"testing"
)

// Stage 47.18.3: the integration and hook logs age out after the retention
// window; an event not yet sent never does; a retention of 0 is a hold.
func TestStage47IntegrationLogRetention(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	exec := func(q string) {
		t.Helper()
		if _, err := db.DB.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	seed := func() {
		for _, age := range []string{"200 days", "1 day"} {
			exec(`INSERT INTO ` + schema + `.integration_event_outbox (event_name, payload, status, attempts, created_at) VALUES ('t', '{}', 'Processed', 1, now() - interval '` + age + `')`)
			exec(`INSERT INTO ` + schema + `.integration_event_log (event_id, channel, status, created_at) VALUES (gen_random_uuid(), 'WMS', 'Success', now() - interval '` + age + `')`)
			exec(`INSERT INTO ` + schema + `.extension_hook_log (hook_id, request_payload_hash, latency_ms, called_at) VALUES (gen_random_uuid(), 'h', 1, now() - interval '` + age + `')`)
		}
		exec(`INSERT INTO ` + schema + `.integration_event_outbox (event_name, payload, status, attempts, created_at) VALUES ('unsent', '{}', 'Pending', 0, now() - interval '400 days')`)
	}
	count := func(table string) int {
		var n int
		if err := db.DB.QueryRow(`SELECT count(*) FROM ` + schema + `.` + table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// A hold: nothing goes.
	if err := SetSetting(tenantID, "platform.integration_log_retention_days", "0", "test"); err != nil {
		t.Fatalf("hold: %v", err)
	}
	seed()
	if swept, err := SweepIntegrationLogRetention(tenantID); err != nil || len(swept) != 0 || count("integration_event_log") != 2 {
		t.Fatalf("a hold must sweep nothing: %v %v, log rows %d", swept, err, count("integration_event_log"))
	}

	// The default 90 days: the 200-day-old rows go, the day-old ones and the unsent event stay.
	if err := SetSetting(tenantID, "platform.integration_log_retention_days", "90", "test"); err != nil {
		t.Fatal(err)
	}
	swept, err := SweepIntegrationLogRetention(tenantID)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	for table, want := range map[string]int{"integration_event_outbox": 2, "integration_event_log": 1, "extension_hook_log": 1} {
		if got := count(table); got != want {
			t.Errorf("%s: %d rows left, want %d (swept %v)", table, got, want, swept)
		}
	}
	var unsent int
	db.DB.QueryRow(`SELECT count(*) FROM ` + schema + `.integration_event_outbox WHERE status = 'Pending'`).Scan(&unsent)
	if unsent != 1 {
		t.Fatalf("the unsent event must stay, whatever its age (pending %d)", unsent)
	}
}
