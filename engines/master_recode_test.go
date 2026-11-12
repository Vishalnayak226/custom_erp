package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// Stage 76.6: the old-long-code clean-up for every master - which types it
// covers, which codes count as old (a typed SKU with a "/" does not), the
// per-column preview, the 24-hour backup gate, the Item warning, the rewrite
// on a throwaway tenant, and the old code still resolving afterwards.
func TestStage766RecodeEveryMaster(t *testing.T) {
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

	for dt, want := range map[string]bool{"Vendor": true, "Customer": true, "Item": true, "ProductFamily": true, "Department": true,
		"Currency": false, "UOM": false, "HSNCode": false, "PurchaseOrder": false} {
		if got := isRecodeMaster(tenantID, dt); got != want {
			t.Errorf("isRecodeMaster(%s) = %v, want %v", dt, got, want)
		}
	}
	if _, err := PlanMasterRecode(tenantID, "PurchaseOrder", nil); code(err) != "ADMINC-0311" {
		t.Errorf("a transaction is not cleaned here, got %v", err)
	}

	// A vendor on the old default shape, a vendor whose typed code merely has
	// a "/" (kept), and a purchase order pointing at the old one.
	oldV, typedV := "Vendor/HQ/2026/000001", "ACME/NORTH"
	doc(oldV, "Vendor", map[string]interface{}{"code": oldV, "name": "Old Shape Traders"})
	doc(typedV, "Vendor", map[string]interface{}{"code": typedV, "name": "Typed Code Co"})
	doc("PO-766", "PurchaseOrder", map[string]interface{}{"vendor": oldV, "note": oldV + " is not a whole value"})

	plan, err := PlanMasterRecodeWithTouches(tenantID, "Vendor", NewMasterCodeFormat("V", 5))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Rows) != 1 || plan.Rows[0].Old != oldV || strings.Contains(plan.Rows[0].New, "/") || plan.Warning != "" {
		t.Fatalf("Vendor preview: want only %s with a short code and no warning, got %+v", oldV, plan)
	}
	poTouched := false
	for _, tc := range plan.Touches {
		if tc.Table == "documents" && tc.Column == "data" && tc.Rows == 1 {
			poTouched = true
		}
	}
	if !poTouched || plan.References != 1 {
		t.Errorf("preview: want the purchase order counted once under documents.data, got refs %d %+v", plan.References, plan.Touches)
	}

	// The backup gate: production with only a 30-hour-old backup refuses,
	// a backup from an hour ago lets it run. (A developer machine with no
	// backup ever recorded is not held to it.)
	const host = "stage766-test-host"
	db.DB.Exec(`DELETE FROM public.backup_runs WHERE host = $1`, host)
	defer db.DB.Exec(`DELETE FROM public.backup_runs WHERE host = $1`, host)
	run := func(age time.Duration) {
		t.Helper()
		if _, err := db.DB.Exec(`INSERT INTO public.backup_runs (scope, tenant_schema, outcome, file_name, size_bytes, duration_ms, host, started_at, finished_at)
			VALUES ('tenant', $1, 'Success', 'stage766.dump.enc', 1, 1, $2, $3, $3)`, schema, host, time.Now().Add(-age)); err != nil {
			t.Fatalf("seed backup run: %v", err)
		}
	}
	t.Setenv("ENV", "production")
	run(30 * time.Hour)
	if b := recodeBackupStatus(schema); b.OK && b.Newest != nil && time.Since(*b.Newest) < 24*time.Hour {
		t.Log("a fresh backup for this schema already exists elsewhere - the refusal case is not checked")
	} else if _, err := ApplyMasterRecode(tenantID, "Vendor", NewMasterCodeFormat("V", 5), false, "tester"); code(err) != "MASTER-0990" {
		t.Fatalf("no backup in 24 h: want MASTER-0990, got %v", err)
	}
	run(time.Hour)

	// The Item warning must be acknowledged.
	oldI := "Item/HQ/2026/000001"
	doc(oldI, "Item", map[string]interface{}{"code": oldI, "name": "Old Shape Shirt"})
	if p, _ := PlanMasterRecode(tenantID, "Item", nil); p.Warning == "" || len(p.Rows) != 1 {
		t.Fatalf("Item preview: want the warning and one row, got %+v", p)
	}
	if _, err := ApplyMasterRecode(tenantID, "Item", nil, false, "tester"); code(err) != "MASTER-0991" {
		t.Fatalf("Item without acknowledging: want MASTER-0991, got %v", err)
	}
	if done, err := ApplyMasterRecode(tenantID, "Item", nil, true, "tester"); err != nil || len(done.Rows) != 1 {
		t.Fatalf("Item acknowledged: %v %+v", err, done)
	}

	done, err := ApplyMasterRecode(tenantID, "Vendor", NewMasterCodeFormat("V", 5), false, "tester")
	if err != nil || len(done.Rows) != 1 {
		t.Fatalf("Vendor apply: %v %+v", err, done)
	}
	newV := done.Rows[0].New
	var vendorRef, note string
	db.DB.QueryRow("SELECT data->>'vendor', data->>'note' FROM "+schema+".documents WHERE id = 'PO-766'").Scan(&vendorRef, &note)
	if vendorRef != newV || note != oldV+" is not a whole value" {
		t.Errorf("the purchase order should point at %s and keep its note, got %q / %q", newV, vendorRef, note)
	}
	var typedLeft bool
	db.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM "+schema+".documents WHERE id = $1 AND doctype = 'Vendor')", typedV).Scan(&typedLeft)
	if !typedLeft {
		t.Errorf("the typed code %s must not be renumbered", typedV)
	}
	if got := RenamedMasterCode(tenantID, "Vendor", oldV); got != newV {
		t.Errorf("the old code should resolve to %s, got %q", newV, got)
	}
	if p, _ := PlanMasterRecode(tenantID, "Vendor", nil); len(p.Rows) != 0 {
		t.Errorf("after the clean-up nothing should be left: %+v", p.Rows)
	}
}
