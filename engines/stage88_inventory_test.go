package engines

import (
	"custom_erp/db"
	"testing"
	"time"
)

// Stage 88.5: the MRP Change document (D14) and the Inventory dashboard.

const (
	s88iSKU = "TEST-S88I-SKU"
	s88iLoc = "TEST-S88I-LOC"
)

func s88iFixture(t *testing.T) string {
	t.Helper()
	spInitDB()
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	cleanup := func() {
		for _, stmt := range []string{
			`DELETE FROM ` + schema + `.async_jobs WHERE job_type = 'inventory_mrp_change_apply' AND payload->>'mrp_change_id' IN (SELECT id FROM ` + schema + `.documents WHERE doctype = 'MRPChange' AND data->>'items' LIKE '%` + s88iSKU + `%')`,
			`DELETE FROM ` + schema + `.documents WHERE doctype = 'MRPChange' AND data->>'items' LIKE '%` + s88iSKU + `%'`,
			`DELETE FROM ` + schema + `.documents WHERE id = '` + s88iSKU + `'`,
			`DELETE FROM ` + schema + `.inventory_availability WHERE sku = '` + s88iSKU + `'`,
		} {
			if _, err := db.DB.Exec(stmt); err != nil {
				t.Fatalf("cleanup: %v", err)
			}
		}
	}
	cleanup()
	t.Cleanup(cleanup)
	if _, err := db.DB.Exec(`INSERT INTO `+schema+`.documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')`,
		s88iSKU, `{"code":"`+s88iSKU+`","name":"Stage 88 MRP item","mrp":100,"sale_price":90}`); err != nil {
		t.Fatalf("item: %v", err)
	}
	if _, err := db.DB.Exec(`INSERT INTO `+schema+`.inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 5, 5)`, s88iSKU, s88iLoc); err != nil {
		t.Fatalf("stock: %v", err)
	}
	return schema
}

func s88iMRP(t *testing.T, schema string) float64 {
	t.Helper()
	var mrp float64
	if err := db.DB.QueryRow(`SELECT (data->>'mrp')::numeric FROM `+schema+`.documents WHERE id = $1`, s88iSKU).Scan(&mrp); err != nil {
		t.Fatalf("read mrp: %v", err)
	}
	return mrp
}

// TestMRPChangeAppliesNowAndSchedules: today's change writes the item's MRP
// and keeps the old one on the document; a later one waits Scheduled with its
// job queued for that day, and once cancelled its job applies nothing.
func TestMRPChangeAppliesNowAndSchedules(t *testing.T) {
	schema := s88iFixture(t)
	id, status, err := CreateMRPChange("default", MRPChangeInput{Reason: "test", Lines: []MRPChangeLine{{SKU: s88iSKU, NewMRP: 120}}}, "tester")
	if err != nil || status != "Applied" {
		t.Fatalf("today's change: %s %s %v", id, status, err)
	}
	if got := s88iMRP(t, schema); got != 120 {
		t.Errorf("item MRP after the change = %v, want 120", got)
	}
	data, _, _, _ := fetchDocument("default", MRPChangeDoctype, id)
	line := jsonLines(data["items"])[0]
	if numFromInterface(line["old_mrp"]) != 100 || numFromInterface(line["new_mrp"]) != 120 || numFromInterface(line["on_hand"]) != 5 {
		t.Errorf("history line = %v, want old 100, new 120, on hand 5", line)
	}
	if _, _, err := CreateMRPChange("default", MRPChangeInput{Lines: []MRPChangeLine{{SKU: s88iSKU, NewMRP: 120}}}, "tester"); err == nil {
		t.Error("a change to the MRP the item already has was accepted")
	}

	tomorrow := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	later, status, err := CreateMRPChange("default", MRPChangeInput{EffectiveDate: tomorrow, Lines: []MRPChangeLine{{SKU: s88iSKU, NewMRP: 150}}}, "tester")
	if err != nil || status != "Scheduled" {
		t.Fatalf("tomorrow's change: %s %s %v", later, status, err)
	}
	if got := s88iMRP(t, schema); got != 120 {
		t.Errorf("a scheduled change moved the MRP early to %v", got)
	}
	var due time.Time
	if err := db.DB.QueryRow(`SELECT next_attempt_at FROM `+schema+`.async_jobs WHERE job_type = 'inventory_mrp_change_apply' AND payload->>'mrp_change_id' = $1`, later).Scan(&due); err != nil {
		t.Fatalf("scheduled job: %v", err)
	}
	if !due.After(time.Now()) {
		t.Errorf("the job runs at %v, want tomorrow", due)
	}
	if err := CancelMRPChange("default", later, "tester"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if err := ApplyMRPChange("default", later, "system"); err != nil {
		t.Fatalf("job run on a cancelled change: %v", err)
	}
	if got := s88iMRP(t, schema); got != 120 {
		t.Errorf("a cancelled change applied: MRP %v", got)
	}
	if err := CancelMRPChange("default", id, "tester"); err == nil {
		t.Error("an applied change was cancelled")
	}
}

// TestInventoryDashboardByStore: the fixture store's units are on it.
func TestInventoryDashboardByStore(t *testing.T) {
	s88iFixture(t)
	d, err := InventoryDashboard("default", s88iLoc)
	if err != nil {
		t.Fatalf("InventoryDashboard: %v", err)
	}
	rows, _ := d["by_location"].([]map[string]interface{})
	if len(rows) != 1 || rows[0]["location"] != s88iLoc || rows[0]["units"] != int64(5) {
		t.Errorf("by_location = %v, want %s with 5 units", rows, s88iLoc)
	}
}

// TestInventoryAdjustmentPostsStockAndGL is 88.5.4 (D13): with no approval
// rule a take-off posts at once - stock, the ledger line and Dr 5190 / Cr 1200
// at the item's cost; it refuses more than is free and a line with no reason;
// with a rule it waits as Pending Approval and posts when someone else approves.
func TestInventoryAdjustmentPostsStockAndGL(t *testing.T) {
	schema := s88iFixture(t)
	var ready bool
	_ = db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM ` + schema + `.doctype_meta WHERE name = 'InventoryAdjustment')`).Scan(&ready)
	if !ready {
		t.Skip("db/migrations_stage88_5_inventory_adjustment.sql has not been applied to this database")
	}
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO `+schema+`.documents (id, doctype, data, status, created_by) VALUES ($1, 'Location', $2, 'Active', 'system') ON CONFLICT DO NOTHING`,
		s88iLoc, `{"code":"`+s88iLoc+`","name":"Stage 88 store"}`)
	exec(`UPDATE `+schema+`.documents SET data = data || '{"cost_price": 40}' WHERE id = $1`, s88iSKU)
	exec(`DELETE FROM ` + schema + `.approval_rules WHERE doctype = 'InventoryAdjustment'`)
	t.Cleanup(func() {
		_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.approval_rules WHERE doctype = 'InventoryAdjustment'`)
		_, _ = db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE doctype IN ('InventoryAdjustment', 'StockLedgerEntry') AND (data->>'location' = $1 OR data->>'warehouse_id' = $1)`, s88iLoc)
		_, _ = db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE id = $1`, s88iLoc)
	})
	onHand := func() int {
		var n int
		_ = db.DB.QueryRow(`SELECT on_hand FROM `+schema+`.inventory_availability WHERE sku = $1 AND location_code = $2`, s88iSKU, s88iLoc).Scan(&n)
		return n
	}
	line := func(qty int, reason string) InventoryAdjustmentInput {
		return InventoryAdjustmentInput{Location: s88iLoc, Lines: []InventoryAdjustmentLine{{SKU: s88iSKU, Qty: qty, ReasonCode: reason}}}
	}

	if _, _, err := CreateInventoryAdjustment("default", line(-9, "IA-DAMAGE"), "maker", "Store Manager"); err == nil {
		t.Error("a take-off of 9 with 5 free was accepted")
	}
	if _, _, err := CreateInventoryAdjustment("default", line(-1, ""), "maker", "Store Manager"); err == nil {
		t.Error("a line with no reason was accepted")
	}
	id, status, err := CreateInventoryAdjustment("default", line(-2, "IA-DAMAGE"), "maker", "Store Manager")
	if err != nil || status != "Posted" {
		t.Fatalf("adjustment: %s %s %v", id, status, err)
	}
	if got := onHand(); got != 3 {
		t.Errorf("on hand after taking off 2 = %d, want 3", got)
	}
	var debit5190, credit1200 int64
	_ = db.DB.QueryRow(`SELECT COALESCE(SUM(debit) FILTER (WHERE account_code = '5190'), 0), COALESCE(SUM(credit) FILTER (WHERE account_code = '1200'), 0)
		FROM `+schema+`.gl_postings WHERE document_type = 'InventoryAdjustment' AND document_id = $1`, id).Scan(&debit5190, &credit1200)
	if debit5190 != 8000 || credit1200 != 8000 {
		t.Errorf("GL = Dr 5190 %d / Cr 1200 %d paise, want 8000 each (2 x 40)", debit5190, credit1200)
	}
	var ledgerQty float64
	_ = db.DB.QueryRow(`SELECT COALESCE(SUM((data->>'qty')::numeric), 0) FROM `+schema+`.documents WHERE doctype = 'StockLedgerEntry' AND data->>'voucher_type' = 'InventoryAdjustment' AND data->>'warehouse_id' = $1`, s88iLoc).Scan(&ledgerQty)
	if ledgerQty != -2 {
		t.Errorf("stock ledger for the adjustment = %v, want -2", ledgerQty)
	}

	if _, err := UpsertApprovalRule("default", InventoryAdjustmentDoctype, 0, nil, "HR/Admin", nil); err != nil {
		t.Fatalf("approval rule: %v", err)
	}
	held, status, err := CreateInventoryAdjustment("default", line(4, "IA-FOUND"), "maker", "Store Manager")
	if err != nil || status != "Pending Approval" {
		t.Fatalf("gated adjustment: %s %s %v", held, status, err)
	}
	if got := onHand(); got != 3 {
		t.Errorf("a pending adjustment moved stock: on hand %d", got)
	}
	if err := DecideApproval("default", InventoryAdjustmentDoctype, held, "checker", "HR/Admin", "", "Approved", ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if got := onHand(); got != 7 {
		t.Errorf("on hand after the approved +4 = %d, want 7", got)
	}
	if _, st, _, _ := fetchDocument("default", InventoryAdjustmentDoctype, held); st != "Posted" {
		t.Errorf("approved adjustment is %s, want Posted", st)
	}
}
