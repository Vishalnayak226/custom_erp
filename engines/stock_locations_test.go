package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"testing"
)

// Stage 85.B: store stock locations. Pinned: QC Hold / Damage are pools
// outside available (a damaged return no longer lowers the good stock's
// sellable figure); the client's Sellable choice reaches both the figure and
// the sale itself; a move keeps every sum right; a receipt lands in its
// transaction's default location; stock is never stranded in a location
// that is switched off.
func TestComputeATSPolicy(t *testing.T) {
	// available 10, reserved 1, safety 1, blocked 2, qc 3, damaged 4, buffer 0, held 1
	if got := computeATS(ATSPolicy{}, 10, 1, 1, 2, 3, 4, 0, 1); got != 5 {
		t.Errorf("default policy: got %d, want 5 (QC Hold and Damage are not subtracted twice)", got)
	}
	if got := computeATS(ATSPolicy{DamagedSellable: true}, 10, 1, 1, 2, 3, 4, 0, 1); got != 9 {
		t.Errorf("damage sellable: got %d, want 9", got)
	}
	if got := computeATS(ATSPolicy{QCHoldSellable: true, DamagedSellable: true}, 10, 1, 1, 2, 3, 4, 0, 1); got != 12 {
		t.Errorf("both sellable: got %d, want 12", got)
	}
}

func TestStockLocationsMoveSellAndReceive(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const (
		store = "S85B-STORE"
		sku   = "S85B-SKU"
		alter = "S85BALTER"
	)
	restore := func() {
		db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
		db.DB.Exec("DELETE FROM "+schema+".stock_location_balance WHERE sku = $1", sku)
		db.DB.Exec("DELETE FROM "+schema+".store_stock_locations WHERE location_code = $1", store)
		db.DB.Exec("DELETE FROM "+schema+".stock_locations WHERE code = $1", alter)
		db.DB.Exec("UPDATE " + schema + ".stock_locations SET sellable = FALSE, active = TRUE, description = 'Damaged stock' WHERE code = 'DAMAGE'")
		db.DB.Exec("UPDATE " + schema + ".stock_location_defaults SET to_location = 'MAIN' WHERE txn_type = 'Goods Receipt'")
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1 OR (doctype = 'StockMove' AND data->>'location' = $1)", store)
		invalidateATSPolicy(schema)
	}
	restore()
	defer restore()

	raw, _ := json.Marshal(map[string]interface{}{"code": store, "name": "S85B Store", "type": "Store", "status": "Active"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Location', $2, 'Active', 'system')", store, raw); err != nil {
		t.Fatalf("location: %v", err)
	}
	// 10 good + 2 returned damaged, the way a returns QC writes them.
	if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available, damaged) VALUES ($1, $2, 12, 10, 2)", sku, store); err != nil {
		t.Fatalf("stock: %v", err)
	}
	ats := func() int {
		res, err := GetAvailableToSell(tenantID, sku, store)
		if err != nil {
			t.Fatalf("ats: %v", err)
		}
		return res["ats"].(int)
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
	if got := ats(); got != 10 {
		t.Fatalf("10 good + 2 damaged must sell 10, got %d", got)
	}
	if locQty("MAIN") != 10 || locQty("DAMAGE") != 2 {
		t.Fatalf("by location: Main %d, Damage %d", locQty("MAIN"), locQty("DAMAGE"))
	}

	// Main -> Damage 3.
	if _, err := MoveStock(tenantID, StockMoveRequest{Store: store, SKU: sku, Qty: 3, From: "MAIN", To: "DAMAGE", Reason: "test"}, "system", "tester"); err != nil {
		t.Fatalf("move to damage: %v", err)
	}
	if ats() != 7 || locQty("DAMAGE") != 5 {
		t.Fatalf("after Main->Damage 3: ats %d, damage %d", ats(), locQty("DAMAGE"))
	}

	// The client's own location: stock stays in available, blocked holds it.
	if err := SaveStockLocation(tenantID, StockLocation{Code: alter, Name: "S85B Alteration", AppliesTo: "Both", DefaultForNew: true}, "tester"); err != nil {
		t.Fatalf("custom location: %v", err)
	}
	if _, err := MoveStock(tenantID, StockMoveRequest{Store: store, SKU: sku, Qty: 2, From: "MAIN", To: alter}, "system", "tester"); err != nil {
		t.Fatalf("move to custom: %v", err)
	}
	if ats() != 5 || locQty(alter) != 2 || locQty("MAIN") != 5 {
		t.Fatalf("after Main->Alteration 2: ats %d, alteration %d, main %d", ats(), locQty(alter), locQty("MAIN"))
	}
	var ve *ValidationError
	if _, err := MoveStock(tenantID, StockMoveRequest{Store: store, SKU: sku, Qty: 9, From: "MAIN", To: "DAMAGE"}, "system", "tester"); !errors.As(err, &ve) || ve.Code != "STKLOC-0864" {
		t.Errorf("moving more than Main holds: want STKLOC-0864, got %v", err)
	}
	if _, err := MoveStock(tenantID, StockMoveRequest{Store: store, SKU: sku, Qty: 1, From: "MAIN", To: "INTRANSIT"}, "system", "tester"); !errors.As(err, &ve) || ve.Code != "STKLOC-0863" {
		t.Errorf("moving into In-Transit: want STKLOC-0863, got %v", err)
	}

	// Damage marked sellable: the figure counts it and a sale beyond Main
	// takes it from Damage instead of refusing.
	if err := SaveStockLocation(tenantID, StockLocation{Code: "DAMAGE", Name: "Damage", Sellable: true, AppliesTo: "Both", DefaultForNew: true, Active: true, DisplayOrder: 50}, "tester"); err != nil {
		t.Fatalf("damage sellable: %v", err)
	}
	if got := ats(); got != 10 {
		t.Fatalf("Main 5 + sellable Damage 5: got %d", got)
	}
	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := PostInventoryLedgerWithVoucherTx(tx, schema, store, []interface{}{map[string]interface{}{"sku": sku, "qty": -9}}, false); err != nil {
		tx.Rollback()
		t.Fatalf("sale of 9 with Main 5 and Damage 5 sellable: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var available, damaged, blocked int
	db.DB.QueryRow("SELECT available, damaged, blocked FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = $2", sku, store).Scan(&available, &damaged, &blocked)
	// Main 5 sold, 4 more from Damage; the 2 in Alteration are untouched.
	if available != 2 || damaged != 1 || blocked != 2 {
		t.Errorf("after the sale: available %d (want 2 = the Alteration units), damaged %d (want 1), blocked %d (want 2)", available, damaged, blocked)
	}
	tx, _ = db.DB.Begin()
	if _, _, err := PostInventoryLedgerWithVoucherTx(tx, schema, store, []interface{}{map[string]interface{}{"sku": sku, "qty": -2}}, false); err == nil {
		t.Error("a sale must not take the units set aside in Alteration")
	}
	tx.Rollback()

	// A location holding stock cannot be switched off.
	if err := SaveStockLocation(tenantID, StockLocation{Code: "DAMAGE", Name: "Damage", Sellable: false, AppliesTo: "Both", DefaultForNew: true, Active: false, DisplayOrder: 50}, "tester"); !errors.As(err, &ve) || ve.Code != "STKLOC-0861" {
		t.Errorf("switching off Damage with stock in it: want STKLOC-0861, got %v", err)
	}

	// Goods Receipt default Inward: the receipt lands there, not in Main.
	if err := SetStockLocationDefault(tenantID, "Goods Receipt", "", "INWARD", "tester"); err != nil {
		t.Fatalf("grn default: %v", err)
	}
	if _, err := PostInventoryLedgerWithVoucher(tenantID, store, []interface{}{map[string]interface{}{"sku": sku, "qty": 4}}, false, "GRN", "S85B-GRN", "tester"); err != nil {
		t.Fatalf("grn post: %v", err)
	}
	if got := locQty("INWARD"); got != 4 {
		t.Errorf("GRN to Inward: inward %d, want 4", got)
	}
	db.DB.QueryRow("SELECT available FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = $2", sku, store).Scan(&available)
	if available != 2 {
		t.Errorf("GRN to Inward must not raise available: %d", available)
	}
	if err := SetStockLocationDefault(tenantID, "POS Sale", "DAMAGE", "", "tester"); err != nil {
		t.Errorf("a fixed default is left as it is, not refused: %v", err)
	}
}
