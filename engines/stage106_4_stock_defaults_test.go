package engines

import (
	"encoding/json"
	"errors"
	"testing"

	"custom_erp/db"
)

// Stage 106.4: the Stock Locations > Transactions rows that were fixed text
// now move stock - POS Sale / Purchase Return / Transfer Out take their From
// first, a Good sales return lands in Sales Return's To.
func TestStage1064StockDefaultRowsMoveStock(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const (
		store = "S1064-STORE"
		sku   = "S1064-SKU"
		alter = "S1064ALT"
	)
	restore := func() {
		db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
		db.DB.Exec("DELETE FROM "+schema+".stock_location_balance WHERE sku = $1", sku)
		db.DB.Exec("DELETE FROM "+schema+".store_stock_locations WHERE location_code = $1", store)
		db.DB.Exec("DELETE FROM "+schema+".stock_locations WHERE code = $1", alter)
		db.DB.Exec("UPDATE " + schema + ".stock_locations SET sellable = FALSE WHERE code = 'DAMAGE'")
		db.DB.Exec("UPDATE " + schema + ".stock_location_defaults SET from_location = 'MAIN' WHERE txn_type IN ('POS Sale', 'Purchase Return', 'Transfer Out')")
		db.DB.Exec("UPDATE " + schema + ".stock_location_defaults SET to_location = 'MAIN' WHERE txn_type = 'Sales Return'")
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1 OR (doctype = 'StockMove' AND data->>'location' = $1)", store)
		invalidateATSPolicy(schema)
	}
	restore()
	defer restore()

	raw, _ := json.Marshal(map[string]interface{}{"code": store, "name": "S1064 Store", "type": "Store", "status": "Active"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Location', $2, 'Active', 'system')", store, raw); err != nil {
		t.Fatalf("location: %v", err)
	}
	if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 10, 10)", sku, store); err != nil {
		t.Fatalf("stock: %v", err)
	}
	if err := SaveStockLocation(tenantID, StockLocation{Code: alter, Name: "S1064 Display", AppliesTo: "Both", DefaultForNew: true}, "tester"); err != nil {
		t.Fatalf("custom location: %v", err)
	}
	locQty := func(code string) int {
		rows, err := StockLocationBalances(tenantID, sku, store)
		if err != nil {
			t.Fatalf("balances: %v", err)
		}
		for _, r := range rows {
			if r.Code == code {
				return r.Qty
			}
		}
		return -1
	}
	post := func(txn string, qty int) {
		t.Helper()
		tx, err := db.DB.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		items := []interface{}{map[string]interface{}{"sku": sku, "qty": float64(-qty)}}
		if err := drawFromDefaultFromTx(tx, schema, txn, store, items); err != nil {
			t.Fatalf("%s draw: %v", txn, err)
		}
		if _, _, err := PostInventoryLedgerWithVoucherTx(tx, schema, store, items, false); err != nil {
			t.Fatalf("%s post: %v", txn, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}

	// Purchase Return From the client's own location: its 3 go first, then 2
	// from Main.
	if _, err := MoveStock(tenantID, StockMoveRequest{Store: store, SKU: sku, Qty: 3, From: "MAIN", To: alter}, "system", "tester"); err != nil {
		t.Fatalf("move to custom: %v", err)
	}
	if err := SetStockLocationDefault(tenantID, "Purchase Return", alter, "", "tester"); err != nil {
		t.Fatalf("purchase return from: %v", err)
	}
	post("Purchase Return", 5)
	if locQty(alter) != 0 || locQty("MAIN") != 5 {
		t.Errorf("purchase return of 5 from %s(3): %s %d (want 0), Main %d (want 5)", alter, alter, locQty(alter), locQty("MAIN"))
	}

	// POS Sale From must be a location that sells.
	var ve *ValidationError
	if err := SetStockLocationDefault(tenantID, "POS Sale", "DAMAGE", "", "tester"); !errors.As(err, &ve) || ve.Code != "STKLOC-0862" {
		t.Errorf("POS Sale from a location that does not sell: want STKLOC-0862, got %v", err)
	}
	if err := SaveStockLocation(tenantID, StockLocation{Code: "DAMAGE", Name: "Damage", Sellable: true, AppliesTo: "Both", DefaultForNew: true, Active: true, DisplayOrder: 50}, "tester"); err != nil {
		t.Fatalf("damage sellable: %v", err)
	}
	if err := SetStockLocationDefault(tenantID, "POS Sale", "DAMAGE", "", "tester"); err != nil {
		t.Fatalf("POS Sale from Damage: %v", err)
	}
	if _, err := MoveStock(tenantID, StockMoveRequest{Store: store, SKU: sku, Qty: 2, From: "MAIN", To: "DAMAGE"}, "system", "tester"); err != nil {
		t.Fatalf("move to damage: %v", err)
	}
	post("POS Sale", 1)
	if locQty("DAMAGE") != 1 || locQty("MAIN") != 3 {
		t.Errorf("sale of 1 from Damage(2): Damage %d (want 1), Main %d (want 3)", locQty("DAMAGE"), locQty("MAIN"))
	}

	// Sales Return To: a Good return lands in the client's location.
	if err := SetStockLocationDefault(tenantID, "Sales Return", "", alter, "tester"); err != nil {
		t.Fatalf("sales return to: %v", err)
	}
	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := applyReturnedStockToBucket(tx, schema, sku, store, 2, "available"); err != nil {
		tx.Rollback()
		t.Fatalf("return to Main: %v", err)
	}
	if err := applyReceiptDefaultTx(tx, schema, "Sales Return", store, []PostedStockLine{{SKU: sku, Qty: 2}}); err != nil {
		tx.Rollback()
		t.Fatalf("return to its location: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if locQty(alter) != 2 || locQty("MAIN") != 3 {
		t.Errorf("Good return of 2 to %s: %s %d (want 2), Main %d (want 3)", alter, alter, locQty(alter), locQty("MAIN"))
	}

	// Move Stock out of a client's own location (an INSERT ... ON CONFLICT
	// with a negative qty used to trip the qty >= 0 check).
	if _, err := MoveStock(tenantID, StockMoveRequest{Store: store, SKU: sku, Qty: 1, From: alter, To: "MAIN"}, "system", "tester"); err != nil {
		t.Fatalf("move out of %s: %v", alter, err)
	}
	if locQty(alter) != 1 || locQty("MAIN") != 4 {
		t.Errorf("after %s->Main 1: %s %d (want 1), Main %d (want 4)", alter, alter, locQty(alter), locQty("MAIN"))
	}

	// Transfer Out: From changes, To stays In-Transit.
	if err := SetStockLocationDefault(tenantID, "Transfer Out", alter, "DAMAGE", "tester"); err != nil {
		t.Fatalf("transfer out: %v", err)
	}
	var from, to string
	db.DB.QueryRow("SELECT from_location, to_location FROM "+schema+".stock_location_defaults WHERE txn_type = 'Transfer Out'").Scan(&from, &to)
	if from != alter || to != "INTRANSIT" {
		t.Errorf("Transfer Out: from %q to %q, want %q to INTRANSIT", from, to, alter)
	}
}
