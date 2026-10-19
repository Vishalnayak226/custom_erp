package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
)

// TestTenantResetClassifiesEveryTable is the reset's guard (91.1): every
// table a client has is either kept, cleared or reset by ResetTenantData. A
// table added later fails this test until it is classified, so a reset never
// wipes - or keeps - something nobody decided about.
func TestTenantResetClassifiesEveryTable(t *testing.T) {
	db.InitDB(testConnStr())
	tables, err := tenantResetTables(db.DB, "tenant_default")
	if err != nil || len(tables) == 0 {
		t.Fatalf("tables: %v %v", err, tables)
	}
	var missing []string
	for _, table := range tables {
		if table == "documents" || table == "prefix_configs" || tenantResetKeep[table] != "" || tenantResetClear[table] != "" {
			continue
		}
		missing = append(missing, table)
	}
	if len(missing) > 0 {
		t.Fatalf("classify these in engines/tenant_reset.go (keep or clear): %s", strings.Join(missing, ", "))
	}
	for table := range tenantResetKeep {
		if tenantResetClear[table] != "" {
			t.Fatalf("%s is both kept and cleared", table)
		}
	}
}

// TestStage91TenantDataReset resets a throwaway client with business data in
// it: the records, stock, ledger, settings and counters go; the logins (with
// their password hashes), permissions and shipped reference records stay;
// numbering starts again from 1; and it refuses while the client is active or
// without a recent backup.
func TestStage91TenantDataReset(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("provision: %v", err)
	}
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	defer db.DB.Exec(`DELETE FROM public.backup_runs WHERE tenant_schema = $1`, schema)

	// Business data of every kind, and a person's login.
	exec(`INSERT INTO ` + schema + `.documents (id, doctype, data, status, created_by) VALUES
		('STORE-91', 'Location', '{"code":"STORE-91"}', 'Active', 'admin'),
		('ITEM-91', 'Item', '{"code":"ITEM-91"}', 'Active', 'admin'),
		('PO-91', 'PurchaseOrder', '{"code":"PO-91"}', 'Approved', 'admin'),
		('TEND-91', 'POSTender', '{"name":"Paytm"}', 'Active', 'admin')`)
	exec(`INSERT INTO ` + schema + `.inventory_availability (sku, location_code, on_hand, available) VALUES ('ITEM-91', 'STORE-91', 5, 5)`)
	exec(`INSERT INTO ` + schema + `.sequence_counters (doc_type, store_code, financial_year, current_val) VALUES ('PO', 'STORE-91', '2026-27', 41)`)
	exec(`INSERT INTO ` + schema + `.system_settings (key, value) VALUES ('procurement.po_design_number_mode', 'match')`)
	exec(`UPDATE ` + schema + `.prefix_configs SET prefix = 'CHANGED' WHERE doc_type = 'PO'`)
	exec(`INSERT INTO ` + schema + `.users (id, username, password_hash, email, role, status) VALUES ('clerk91', 'clerk91', '$2a$10$keepthishash', 'c@x.local', 'Store Manager', 'Active')`)
	var seedDocs, rolePerms int
	db.DB.QueryRow(`SELECT COUNT(*) FROM ` + schema + `.documents WHERE created_by = 'system'`).Scan(&seedDocs)
	db.DB.QueryRow(`SELECT COUNT(*) FROM ` + schema + `.role_permissions`).Scan(&rolePerms)

	plan, err := PlanTenantDataReset(tenantID)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Unclassified) > 0 {
		t.Fatalf("unclassified tables: %v", plan.Unclassified)
	}
	if plan.DocumentsRemoved["Item"] != 1 || plan.DocumentsRemoved["POSTender"] != 1 || plan.DocumentsKept["StatusTransitionRule"] == 0 {
		t.Fatalf("documents plan: removed %v kept %v", plan.DocumentsRemoved, plan.DocumentsKept)
	}
	if _, err := ResetTenantData(tenantID, "test", "clean slate"); err == nil || !strings.Contains(err.Error(), "suspend it first") {
		t.Fatalf("an active client must not be reset, got %v", err)
	}
	if err := SuspendTenant(tenantID, "test", "reset"); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	// A server that has recorded backups needs one from the last day.
	exec(`INSERT INTO public.backup_runs (scope, tenant_schema, outcome, finished_at) VALUES ('tenant', $1, 'Success', NOW() - INTERVAL '3 days')`, schema)
	if _, err := ResetTenantData(tenantID, "test", "clean slate"); err == nil || !strings.Contains(err.Error(), "backup") {
		t.Fatalf("a stale backup must refuse the reset, got %v", err)
	}
	exec(`INSERT INTO public.backup_runs (scope, tenant_schema, outcome, finished_at) VALUES ('tenant', $1, 'Success', NOW())`, schema)
	if _, err := ResetTenantData(tenantID, "test", ""); err == nil {
		t.Fatalf("a reset without a reason must be refused")
	}

	done, err := ResetTenantData(tenantID, "test", "clean slate")
	if err != nil || !done.Applied {
		t.Fatalf("reset: %v %+v", err, done)
	}
	count := func(q string) int {
		var n int
		db.DB.QueryRow(q).Scan(&n)
		return n
	}
	if n := count(`SELECT COUNT(*) FROM ` + schema + `.documents`); n != seedDocs {
		t.Fatalf("documents left %d, want only the %d shipped ones", n, seedDocs)
	}
	for _, table := range []string{"inventory_availability", "sequence_counters", "system_settings"} {
		if n := count(`SELECT COUNT(*) FROM ` + schema + `.` + table); n != 0 {
			t.Fatalf("%s still has %d row(s)", table, n)
		}
	}
	var hash string
	db.DB.QueryRow(`SELECT password_hash FROM ` + schema + `.users WHERE id = 'clerk91'`).Scan(&hash)
	if hash != "$2a$10$keepthishash" {
		t.Fatalf("a login's password hash must never change, got %q", hash)
	}
	if n := count(`SELECT COUNT(*) FROM ` + schema + `.role_permissions`); n != rolePerms {
		t.Fatalf("permissions %d, want %d kept", n, rolePerms)
	}
	if n := count(`SELECT COUNT(*) FROM ` + schema + `.prefix_configs WHERE prefix = 'CHANGED'`); n != 0 {
		t.Fatalf("number formats must be back to the shipped ones")
	}
	if n := count(`SELECT COUNT(*) FROM public.tenant_lifecycle_events WHERE tenant_id = '` + tenantID + `' AND event = 'data_reset'`); n != 1 {
		t.Fatalf("the reset must be evidenced once, got %d", n)
	}
	// Numbering starts again from 1.
	id, err := GenerateSequence(tenantID, "PO", "STORE-91", "2026-27")
	if err != nil || !strings.HasSuffix(id, "1") || strings.Contains(id, "42") {
		t.Fatalf("first number after the reset = %q (%v), want it to start from 1", id, err)
	}
}
