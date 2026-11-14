package engines

import (
	"strings"
	"testing"

	"custom_erp/db"
)

func retentionTestSetup(t *testing.T) (string, string) {
	t.Helper()
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("GetTenantSchema: %v", err)
	}
	return "default", schema
}

func seedGatePass(t *testing.T, schema, id string, ageDays int) {
	t.Helper()
	db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id = '" + id + "'")
	if _, err := db.DB.Exec(`INSERT INTO `+schema+`.documents (id, doctype, data, status, created_by, created_at)
		VALUES ($1,'GatePass','{"driver_phone":"9876543210"}','Active','system', CURRENT_TIMESTAMP - ($2 || ' days')::interval)`, id, ageDays); err != nil {
		t.Fatalf("seed gate pass: %v", err)
	}
}

func gatePassPhone(t *testing.T, schema, id string) string {
	t.Helper()
	var p string
	if err := db.DB.QueryRow(`SELECT COALESCE(data->>'driver_phone','') FROM `+schema+`.documents WHERE id = $1`, id).Scan(&p); err != nil {
		t.Fatalf("read gate pass: %v", err)
	}
	return p
}

// The seeded table is today's behaviour: 90 days for the driver phone.
func TestRetentionSeededRowsKeepTodaysBehaviour(t *testing.T) {
	tenant, schema := retentionTestSetup(t)
	if d := RetentionDays(tenant, "GatePass", "driver_phone", 7); d != 90 {
		t.Fatalf("seeded GatePass/driver_phone = %d days, want 90", d)
	}
	if d := RetentionDays(tenant, "NoSuchType", "x", 33); d != 33 {
		t.Fatalf("no row must fall back to the default, got %d", d)
	}
	seedGatePass(t, schema, "I170-GP-OLD", 100)
	seedGatePass(t, schema, "I170-GP-NEW", 30)
	defer db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'I170-GP-%'")
	if _, err := SweepPersonalFieldRetention(tenant); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if gatePassPhone(t, schema, "I170-GP-OLD") != "" {
		t.Error("a 100-day gate pass keeps its phone under the 90-day seed")
	}
	if gatePassPhone(t, schema, "I170-GP-NEW") == "" {
		t.Error("a 30-day gate pass lost its phone under the 90-day seed")
	}
}

// Editing the row changes what the sweep does.
func TestRetentionEditedRowChangesSweep(t *testing.T) {
	tenant, schema := retentionTestSetup(t)
	seedGatePass(t, schema, "I170-GP-MID", 30)
	defer db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'I170-GP-%'")
	defer SaveRetentionPolicy(tenant, "test", RetentionPolicy{RecordType: "GatePass", FieldName: "driver_phone", KeepFor: 90, KeepUnit: "days", ThenAction: "clear field"})
	if err := SaveRetentionPolicy(tenant, "test", RetentionPolicy{RecordType: "GatePass", FieldName: "driver_phone", KeepFor: 10, KeepUnit: "days", ThenAction: "clear field"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := SweepPersonalFieldRetention(tenant); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if gatePassPhone(t, schema, "I170-GP-MID") != "" {
		t.Error("the 10-day rule did not clear a 30-day-old phone")
	}
}

// A legal hold still wins over a shorter rule.
func TestRetentionLegalHoldStillWins(t *testing.T) {
	tenant, schema := retentionTestSetup(t)
	var was bool
	if err := db.DB.QueryRow(`SELECT COALESCE(legal_hold, FALSE) FROM public.tenants WHERE tenant_id = $1`, tenant).Scan(&was); err != nil {
		t.Skipf("no tenant row to hold: %v", err)
	}
	seedGatePass(t, schema, "I170-GP-HELD", 400)
	defer db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'I170-GP-%'")
	db.DB.Exec(`UPDATE public.tenants SET legal_hold = TRUE WHERE tenant_id = $1`, tenant)
	defer db.DB.Exec(`UPDATE public.tenants SET legal_hold = $2 WHERE tenant_id = $1`, tenant, was)
	if _, err := SweepPersonalFieldRetention(tenant); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if gatePassPhone(t, schema, "I170-GP-HELD") == "" {
		t.Error("the sweep cleared a field under legal hold")
	}
}

// The audit-log address cannot be set below its floor.
func TestRetentionFloorAndShapeRefused(t *testing.T) {
	tenant, _ := retentionTestSetup(t)
	err := SaveRetentionPolicy(tenant, "test", RetentionPolicy{RecordType: "audit_logs", FieldName: "source_ip", KeepFor: 30, KeepUnit: "days", ThenAction: "clear field"})
	if ve, ok := err.(*ValidationError); !ok || ve.Code != "SEC-1701" {
		t.Errorf("30 days for the audit address: got %v, want SEC-1701", err)
	}
	err = SaveRetentionPolicy(tenant, "test", RetentionPolicy{RecordType: "GatePass", KeepFor: 5, KeepUnit: "weeks", ThenAction: "clear field"})
	if ve, ok := err.(*ValidationError); !ok || ve.Code != "SEC-1700" || !strings.Contains(ve.Message, "not valid") {
		t.Errorf("bad unit: got %v, want SEC-1700", err)
	}
}
