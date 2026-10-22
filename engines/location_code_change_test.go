package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// Stage 123.3: a store's typed code - the checks (shape, taken, unknown
// store), the rewrite of the store and what points at it, the old code still
// resolving, and a second change carrying the first code along.
func TestStage1233LocationCodeChange(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	var migrated bool
	_ = db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema = $1 AND table_name = 'master_code_changes')`, schema).Scan(&migrated)
	if !migrated {
		t.Skip("needs migrations_stage93_location_cost_center.sql (master_code_changes) - run erp-server -migrate on this database")
	}
	doc := func(id, doctype string, data map[string]interface{}) {
		t.Helper()
		b, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1,$2,$3,'Active','tester')", id, doctype, b); err != nil {
			t.Fatal(err)
		}
	}
	code := func(err error) string {
		var v *ValidationError
		if errors.As(err, &v) {
			return v.Code
		}
		return ""
	}
	// A fresh backup of this client, so the 24-hour rule passes wherever the
	// test database has backups recorded.
	const host = "stage1233-test-host"
	defer db.DB.Exec(`DELETE FROM public.backup_runs WHERE host = $1`, host)
	if _, err := db.DB.Exec(`INSERT INTO public.backup_runs (scope, tenant_schema, outcome, file_name, size_bytes, duration_ms, host, started_at, finished_at)
		VALUES ('tenant', $1, 'Success', 'stage1233.dump.enc', 1, 1, $2, $3, $3)`, schema, host, time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("seed backup run: %v", err)
	}

	oldID := "Location/HQ/2026/000001"
	doc(oldID, "Location", map[string]interface{}{"code": oldID, "name": "BLR Warehouse", "type": "Warehouse"})
	doc("JAYA", "Location", map[string]interface{}{"code": "JAYA", "name": "Jayanagar"})
	doc("I00001", "Item", map[string]interface{}{"code": "I00001", "name": "Ring"})
	doc("TO-1233", "TransferOrder", map[string]interface{}{"from_location": oldID, "to_location": "JAYA", "note": oldID + " is not a whole value"})

	for _, c := range []struct{ id, code, want string }{
		{oldID, "BLR WH", "MASTER-0992"},
		{oldID, "BLR/WH", "MASTER-0992"},
		{"NO-SUCH-STORE", "BLRWH", "MASTER-0992"},
		{oldID, "jaya", "MASTER-0993"},
		{oldID, "I00001", "MASTER-0993"},
	} {
		if _, err := PlanLocationCodeChange(tenantID, c.id, c.code, false); code(err) != c.want {
			t.Errorf("%s -> %q: want %s, got %v", c.id, c.code, c.want, err)
		}
	}

	plan, err := PlanLocationCodeChange(tenantID, oldID, " blrwh ", true)
	if err != nil {
		t.Fatal(err)
	}
	if plan.New != "BLRWH" || plan.Name != "BLR Warehouse" || plan.References != 1 || len(plan.Touches) == 0 {
		t.Fatalf("preview: want BLRWH, its name and one reference, got %+v", plan)
	}
	if same, err := ApplyLocationCodeChange(tenantID, oldID, oldID, "tester"); err != nil || same.New != oldID {
		t.Fatalf("the same code changes nothing: %+v %v", same, err)
	}

	if _, err := ApplyLocationCodeChange(tenantID, oldID, "blrwh", "tester"); err != nil {
		t.Fatal(err)
	}
	var codeNow, from, note string
	if err := db.DB.QueryRow(`SELECT data->>'code' FROM ` + schema + `.documents WHERE doctype = 'Location' AND id = 'BLRWH'`).Scan(&codeNow); err != nil || codeNow != "BLRWH" {
		t.Fatalf("the store is BLRWH now: code %q, %v", codeNow, err)
	}
	_ = db.DB.QueryRow(`SELECT data->>'from_location', data->>'note' FROM `+schema+`.documents WHERE id = 'TO-1233'`).Scan(&from, &note)
	if from != "BLRWH" || note != oldID+" is not a whole value" {
		t.Errorf("the transfer points at BLRWH and its note is untouched, got %q / %q", from, note)
	}
	if got, err := ResolveLocationReference(tenantID, oldID); err != nil || got != "BLRWH" {
		t.Errorf("the old code still finds the store: %q %v", got, err)
	}

	// A second change: the first code follows to the newest one, and the
	// new code keeps no old mapping of its own.
	if _, err := ApplyLocationCodeChange(tenantID, "BLRWH", "BLR-WH2", "tester"); err != nil {
		t.Fatal(err)
	}
	if got := renamedMasterCode(schema, "Location", oldID); got != "BLR-WH2" {
		t.Errorf("the first code follows to BLR-WH2, got %q", got)
	}
	if got, err := ResolveLocationReference(tenantID, "BLRWH"); err != nil || got != "BLR-WH2" {
		t.Errorf("the middle code finds the store: %q %v", got, err)
	}
	var stale int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM ` + schema + `.master_code_changes WHERE doc_type = 'Location' AND old_code = 'BLR-WH2'`).Scan(&stale)
	if stale != 0 {
		t.Errorf("the live code keeps no old mapping, found %d", stale)
	}
}
