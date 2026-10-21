package engines

import (
	"custom_erp/db"
	"database/sql"
	"errors"
	"testing"
)

// Stage 74.1.5: the command spine posts once, all or nothing - a replay
// returns the first result, a reused key with another payload and a stale
// revision are 409s, a failed posting leaves nothing behind, and the stock
// ledger written in the posting's transaction refuses a closed period.
func TestStage74_1_5CommandSpine(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	if _, err := db.DB.Exec("INSERT INTO " + schemaName + ".documents (id, doctype, data, status, created_by) VALUES ('ADJ-S1', 'InventoryAdjustment', '{\"code\":\"ADJ-S1\"}', 'Draft', 'system')"); err != nil {
		t.Fatal(err)
	}
	ledgerRows := func(voucher string) int {
		var n int
		_ = db.DB.QueryRow("SELECT COUNT(*) FROM "+schemaName+".documents WHERE doctype = 'StockLedgerEntry' AND data->>'voucher_id' = $1", voucher).Scan(&n)
		return n
	}
	runs := 0
	post := func(tx *sql.Tx, schema string, doc map[string]interface{}) (map[string]interface{}, error) {
		runs++
		if err := WriteStockLedgerLinesTx(tx, schema, "HO", "InventoryAdjustment", "ADJ-S1", "tester", "", []PostedStockLine{{SKU: "SKU-S", Qty: 3}}); err != nil {
			return nil, err
		}
		return map[string]interface{}{"posted": doc["code"], "run": runs}, nil
	}
	cmd := EconomicCommand{Command: "test.post", Doctype: "InventoryAdjustment", DocID: "ADJ-S1", IdempotencyKey: "k-spine-1", ExpectedVersion: 1, Payload: map[string]interface{}{"a": 1}}
	first, err := RunEconomicCommand(tenantID, cmd, post)
	if err != nil || first["posted"] != "ADJ-S1" || ledgerRows("ADJ-S1") != 1 {
		t.Fatalf("first run: %v %v rows=%d", err, first, ledgerRows("ADJ-S1"))
	}
	again, err := RunEconomicCommand(tenantID, cmd, post)
	if err != nil || runs != 1 || again["posted"] != "ADJ-S1" || ledgerRows("ADJ-S1") != 1 {
		t.Errorf("a replay returns the first result without posting again: %v %v runs=%d rows=%d", err, again, runs, ledgerRows("ADJ-S1"))
	}
	other := cmd
	other.Payload = map[string]interface{}{"a": 2}
	var verr *ValidationError
	if _, err := RunEconomicCommand(tenantID, other, post); !errors.As(err, &verr) || verr.Code != "GLOBAL-0006" {
		t.Errorf("the same key with another payload is a 409: %v", err)
	}
	stale := cmd
	stale.IdempotencyKey, stale.ExpectedVersion = "k-spine-2", 7
	if _, err := RunEconomicCommand(tenantID, stale, post); !errors.As(err, &verr) || verr.Code != "GLOBAL-0006" {
		t.Errorf("a stale revision is a 409: %v", err)
	}

	// A closed period: the ledger write is refused and nothing is kept.
	if _, err := db.DB.Exec("INSERT INTO " + schemaName + ".accounting_periods (period_name, start_date, end_date, status, created_by) VALUES ('Closed now', CURRENT_DATE - 1, CURRENT_DATE + 1, 'Closed', 'tester')"); err != nil {
		t.Fatal(err)
	}
	closed := cmd
	closed.IdempotencyKey, closed.DocID = "k-spine-3", "ADJ-S2"
	if _, err := db.DB.Exec("INSERT INTO " + schemaName + ".documents (id, doctype, data, status, created_by) VALUES ('ADJ-S2', 'InventoryAdjustment', '{\"code\":\"ADJ-S2\"}', 'Draft', 'system')"); err != nil {
		t.Fatal(err)
	}
	postS2 := func(tx *sql.Tx, schema string, doc map[string]interface{}) (map[string]interface{}, error) {
		return nil, WriteStockLedgerLinesTx(tx, schema, "HO", "InventoryAdjustment", "ADJ-S2", "tester", "", []PostedStockLine{{SKU: "SKU-S", Qty: 1}})
	}
	if _, err := RunEconomicCommand(tenantID, closed, postS2); !errors.As(err, &verr) || verr.Code != "FIN-0260" || ledgerRows("ADJ-S2") != 0 {
		t.Errorf("a closed period refuses the stock posting and keeps nothing: %v rows=%d", err, ledgerRows("ADJ-S2"))
	}
}
