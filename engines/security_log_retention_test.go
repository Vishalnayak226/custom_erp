package engines

import (
	"custom_erp/db"
	"os"
	"strings"
	"testing"
)

// Stage 47.16.3: whatever the retention settings say, the sweeps never delete
// a security-log row younger than 180 days; rows older than that still go;
// and the payload logs the same settings sweep keep their shorter retention.
func TestStage47163SecurityLogsKeep180Days(t *testing.T) {
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
	for _, age := range []string{"100 days", "200 days"} {
		ago := `now() - interval '` + age + `'`
		exec(`INSERT INTO ` + schema + `.api_request_log (method, path, status_code, duration_ms, outcome, created_at) VALUES ('GET', '/t', 200, 1, 'handled', ` + ago + `)`)
		exec(`INSERT INTO ` + schema + `.system_error_logs (severity, module_source, error_message, created_at) VALUES ('ERROR', 't', 't', ` + ago + `)`)
		exec(`INSERT INTO ` + schema + `.integration_event_log (event_id, channel, status, created_at) VALUES (gen_random_uuid(), 'WMS', 'Success', ` + ago + `)`)
		exec(`INSERT INTO ` + schema + `.extension_hook_log (hook_id, request_payload_hash, latency_ms, called_at) VALUES (gen_random_uuid(), 'h', 1, ` + ago + `)`)
		exec(`INSERT INTO ` + schema + `.integration_event_outbox (event_name, payload, status, attempts, created_at) VALUES ('t', '{}', 'Processed', 1, ` + ago + `)`)
	}
	for key, v := range map[string]string{
		"platform.integration_log_retention_days": "30",
		"platform.error_log_retention_days":       "30",
		"platform.public_api_log_retention_days":  "30",
	} {
		if err := SetSetting(tenantID, key, v, "test"); err != nil {
			t.Fatalf("%s: %v", key, err)
		}
	}
	if _, err := SweepIntegrationLogRetention(tenantID); err != nil {
		t.Fatalf("integration sweep: %v", err)
	}
	if _, _, err := SweepPublicAPIRuntime(tenantID); err != nil {
		t.Fatalf("public API sweep: %v", err)
	}
	count := func(table, col, cond string) (n int) {
		db.DB.QueryRow(`SELECT count(*) FROM ` + schema + `.` + table + ` WHERE ` + col + ` ` + cond).Scan(&n)
		return
	}
	for _, table := range SecurityLogTables() {
		col := securityLogAgeColumns[table]
		if young := count(table, col, `> now() - interval '150 days'`); young != 1 {
			t.Errorf("%s: the 100-day-old row must survive a 30-day setting (CERT-In 180-day floor), %d left", table, young)
		}
		if old := count(table, col, `< now() - interval '150 days'`); old != 0 {
			t.Errorf("%s: the 200-day-old row is past the floor and the setting, so it goes - %d left", table, old)
		}
	}
	if n := count("integration_event_outbox", "created_at", `IS NOT NULL`); n != 0 {
		t.Errorf("integration_event_outbox holds payload copies, not access records: a 30-day setting sweeps it, %d rows left", n)
	}
}

// The floor's list, the state-store inventory and the CERT-In inventory page
// name the same tables, and every floored table is one a retention sweep
// actually reaches - a floor on a table nothing sweeps would be dead config.
func TestStage47163SecurityLogInventoryMatchesTheFloor(t *testing.T) {
	stores := map[string]bool{}
	for _, s := range SecurityStateStores() {
		stores[s.Name] = true
	}
	swept := map[string]bool{"api_request_log": true} // SweepPublicAPIRuntime
	for _, t := range integrationLogTables {
		swept[t.table] = true
	}
	doc, err := os.ReadFile("../docs/security/cert-in-log-inventory.md")
	if err != nil {
		t.Fatalf("the CERT-In log inventory page is missing: %v", err)
	}
	for _, table := range SecurityLogTables() {
		if !stores[table] {
			t.Errorf("%s is floored but not on SecurityStateStores", table)
		}
		if !swept[table] {
			t.Errorf("%s is floored but no retention sweep reaches it", table)
		}
		if !strings.Contains(string(doc), "`"+table+"`") {
			t.Errorf("docs/security/cert-in-log-inventory.md does not list %s", table)
		}
	}
	if SecurityLogMinRetentionDays != 180 || !strings.Contains(string(doc), "180 days") {
		t.Errorf("the floor is CERT-In's 180 days, and the page must say so")
	}
	if got := withSecurityLogFloor("pim_publish_log", "x"); got != "x" {
		t.Errorf("a table off the list keeps its own predicate, got %q", got)
	}
}
