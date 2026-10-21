package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// Stage 74.2.3: a stock adjustment posts quantity and value in one
// transaction - a closed period refuses the whole posting and keeps the
// reason - and a Damage loss books to the damage event's posting profile.
func TestStage74_2_3AdjustmentAllOrNothing(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schemaName+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	gl := func(account string) int64 {
		var v int64
		_ = db.DB.QueryRow(`SELECT COALESCE(SUM(debit - credit), 0) FROM `+schemaName+`.gl_postings WHERE document_type = 'InventoryAdjustment' AND document_id = 'IA-T1' AND account_code = $1`, account).Scan(&v)
		return v
	}
	ledger := func() int {
		var n int
		_ = db.DB.QueryRow(`SELECT COUNT(*) FROM ` + schemaName + `.documents WHERE doctype = 'StockLedgerEntry' AND data->>'voucher_type' = 'InventoryAdjustment'`).Scan(&n)
		return n
	}
	insert("ST-A", "Location", "Active", map[string]interface{}{"code": "ST-A", "name": "Store A", "type": "Store", "sellable": "Yes"})
	insert("SKU-A", "Item", "Active", map[string]interface{}{"code": "SKU-A", "name": "Glass", "hsn_code": "7013", "gst_rate": 18.0})
	if _, err := PostInventoryLedger(tenantID, "ST-A", []interface{}{map[string]interface{}{"sku": "SKU-A", "qty": 10.0}}, false); err != nil {
		t.Fatal(err)
	}
	insert("IA-T1", "InventoryAdjustment", "Draft", map[string]interface{}{"code": "IA-T1", "location": "ST-A", "loss_class": "Damage",
		"items": `[{"sku":"SKU-A","qty":-2,"reason_code":"BROKEN","unit_cost":100,"value":-200}]`, "decrease_value": 200.0, "status": "Draft"})

	if _, err := db.DB.Exec("INSERT INTO " + schemaName + ".accounting_periods (period_name, start_date, end_date, status, created_by) VALUES ('Closed', CURRENT_DATE - 1, CURRENT_DATE + 1, 'Closed', 'tester')"); err != nil {
		t.Fatal(err)
	}
	if err := PostInventoryAdjustment(tenantID, "IA-T1", "tester"); err == nil {
		t.Fatalf("a closed period refuses the posting")
	}
	data, status, _, _ := fetchDocument(tenantID, "InventoryAdjustment", "IA-T1")
	if status != "Draft" || ledger() != 0 || gl("5190") != 0 || strField(data, "post_error") == "" {
		t.Errorf("nothing moved, the reason kept: status %s ledger %d gl %d err %q", status, ledger(), gl("5190"), strField(data, "post_error"))
	}

	if _, err := db.DB.Exec("DELETE FROM " + schemaName + ".accounting_periods"); err != nil {
		t.Fatal(err)
	}
	insert("PSP-DMG", "PostingProfile", "Approved", map[string]interface{}{"code": "PSP-DMG", "event": PostingEventAdjustmentDamage, "debit_account": "5330", "status": "Approved"})
	if err := PostInventoryAdjustment(tenantID, "IA-T1", "tester"); err != nil {
		t.Fatalf("post: %v", err)
	}
	_, status, _, _ = fetchDocument(tenantID, "InventoryAdjustment", "IA-T1")
	if status != "Posted" || ledger() != 1 || gl("5330") != 20000 || gl("1200") != -20000 || gl("5190") != 0 {
		t.Errorf("posted in one go to the damage profile: status %s ledger %d 5330 %d 1200 %d", status, ledger(), gl("5330"), gl("1200"))
	}
	if err := PostInventoryAdjustment(tenantID, "IA-T1", "tester"); err != nil || ledger() != 1 || gl("5330") != 20000 {
		t.Errorf("posting twice is a no-op: %v ledger %d", err, ledger())
	}
}

// Stage 74.2.3 (74.0.2 b): an approved cycle-count variance books its value
// with the stock, in one transaction.
func TestStage74_2_3CycleCountVarianceBooksValue(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schemaName+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	insert("SKU-C", "Item", "Active", map[string]interface{}{"code": "SKU-C", "name": "Cup", "standard_cost": 40.0})
	if _, err := PostInventoryLedger(tenantID, "ST-C", []interface{}{map[string]interface{}{"sku": "SKU-C", "qty": 10.0}}, false); err != nil {
		t.Fatal(err)
	}
	insert("CCL-1", "CycleCountLine", "Approved", map[string]interface{}{"sku": "SKU-C", "location": "ST-C", "variance": -3.0, "variance_reason_code": "MISSING", "status": "Approved"})
	if err := PostCycleCountAdjustment(tenantID, "CCL-1", "counter"); err != nil {
		t.Fatalf("post: %v", err)
	}
	var shrink, ledger int64
	_ = db.DB.QueryRow(`SELECT COALESCE(SUM(debit - credit), 0) FROM ` + schemaName + `.gl_postings WHERE document_type = 'CycleCountLine' AND document_id = 'CCL-1' AND account_code = '5190'`).Scan(&shrink)
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM ` + schemaName + `.documents WHERE doctype = 'StockLedgerEntry' AND data->>'voucher_id' = 'CCL-1'`).Scan(&ledger)
	if shrink != 12000 || ledger != 1 {
		t.Errorf("3 short at 40 books 120 to 5190 with its ledger row: gl %d ledger %d", shrink, ledger)
	}
}
