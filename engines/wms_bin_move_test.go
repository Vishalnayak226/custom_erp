package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// Stage 57.9 Location Movement: MoveBinStock's destination rules and lot
// handling.
func TestMoveBinStock(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const loc, other = "TEST-MV-LOC", "TEST-MV-OTHER"
	const sku, lotSKU = "TEST-MV-SKU", "TEST-MV-LOT"
	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'TEST-MV-%'")
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'StockLedgerEntry' AND data->>'item_id' IN ($1, $2)", sku, lotSKU)
		db.DB.Exec("DELETE FROM "+schema+".bin_stock WHERE sku IN ($1, $2)", sku, lotSKU)
		db.DB.Exec("DELETE FROM "+schema+".bin_stock_batch WHERE sku IN ($1, $2)", sku, lotSKU)
	}
	cleanup()
	defer cleanup()

	bin := func(code, location, status, opState string) {
		b, _ := json.Marshal(map[string]interface{}{"bin_code": code, "location": location, "status": status, "bin_status": opState})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Bin', $2, $3, 'system')", code, b, status); err != nil {
			t.Fatalf("seed bin %s: %v", code, err)
		}
	}
	bin("TEST-MV-A", loc, "Active", "")
	bin("TEST-MV-B", loc, "Active", "")
	bin("TEST-MV-C", other, "Active", "")
	bin("TEST-MV-D", loc, "Active", "Blocked")
	stock := func(binCode, s string, qty int) {
		if _, err := db.DB.Exec("INSERT INTO "+schema+".bin_stock (bin_code, sku, location_code, condition, qty) VALUES ($1, $2, $3, 'Good', $4)", binCode, s, loc, qty); err != nil {
			t.Fatalf("seed stock: %v", err)
		}
	}
	stock("TEST-MV-A", sku, 10)
	qty := func(binCode, s string) int {
		var q int
		db.DB.QueryRow("SELECT COALESCE(SUM(qty),0) FROM "+schema+".bin_stock WHERE bin_code = $1 AND sku = $2 AND condition = 'Good'", binCode, s).Scan(&q)
		return q
	}

	if err := MoveBinStock(tenantID, BinMoveInput{FromBin: "TEST-MV-A", ToBin: "TEST-MV-B", SKU: sku, Qty: 4, Reason: "re-slot", UserID: "system"}); err != nil {
		t.Fatalf("move: %v", err)
	}
	if a, b := qty("TEST-MV-A", sku), qty("TEST-MV-B", sku); a != 6 || b != 4 {
		t.Fatalf("after move A=%d B=%d, want 6/4", a, b)
	}
	var ledger int
	db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".documents WHERE doctype = 'StockLedgerEntry' AND data->>'item_id' = $1 AND data->>'voucher_type' = 'BinMove' AND data->>'to_location_id' = 'TEST-MV-B'", sku).Scan(&ledger)
	if ledger != 1 {
		t.Fatalf("expected one BinMove ledger entry, got %d", ledger)
	}

	for name, in := range map[string]BinMoveInput{
		"other location": {FromBin: "TEST-MV-A", ToBin: "TEST-MV-C", SKU: sku, Qty: 1},
		"blocked bin":    {FromBin: "TEST-MV-A", ToBin: "TEST-MV-D", SKU: sku, Qty: 1},
		"missing bin":    {FromBin: "TEST-MV-A", ToBin: "TEST-MV-NOPE", SKU: sku, Qty: 1},
		"more than held": {FromBin: "TEST-MV-A", ToBin: "TEST-MV-B", SKU: sku, Qty: 7},
		"same bin":       {FromBin: "TEST-MV-A", ToBin: "TEST-MV-A", SKU: sku, Qty: 1},
	} {
		if err := MoveBinStock(tenantID, in); err == nil {
			t.Fatalf("%s: expected the move to be refused", name)
		}
	}
	if a := qty("TEST-MV-A", sku); a != 6 {
		t.Fatalf("a refused move changed stock: A=%d", a)
	}

	// Two lots in one bin: the lot must be named, and its breakdown moves.
	stock("TEST-MV-A", lotSKU, 8)
	for _, lot := range []struct {
		no  string
		qty int
	}{{"L1", 5}, {"L2", 3}} {
		db.DB.Exec("INSERT INTO "+schema+".bin_stock_batch (bin_code, sku, batch_no, condition, location_code, qty) VALUES ('TEST-MV-A', $1, $2, 'Good', $3, $4)", lotSKU, lot.no, loc, lot.qty)
	}
	if err := MoveBinStock(tenantID, BinMoveInput{FromBin: "TEST-MV-A", ToBin: "TEST-MV-B", SKU: lotSKU, Qty: 2}); err == nil {
		t.Fatalf("expected a move from a two-lot bin without a batch to be refused")
	}
	if err := MoveBinStock(tenantID, BinMoveInput{FromBin: "TEST-MV-A", ToBin: "TEST-MV-B", SKU: lotSKU, Qty: 2, BatchNo: "L2"}); err != nil {
		t.Fatalf("lot move: %v", err)
	}
	var l2A, l2B int
	db.DB.QueryRow("SELECT qty FROM "+schema+".bin_stock_batch WHERE bin_code = 'TEST-MV-A' AND sku = $1 AND batch_no = 'L2'", lotSKU).Scan(&l2A)
	db.DB.QueryRow("SELECT qty FROM "+schema+".bin_stock_batch WHERE bin_code = 'TEST-MV-B' AND sku = $1 AND batch_no = 'L2'", lotSKU).Scan(&l2B)
	if l2A != 1 || l2B != 2 {
		t.Fatalf("lot L2 after move A=%d B=%d, want 1/2", l2A, l2B)
	}

	rows, err := GetBinContents(tenantID, loc)
	if err != nil {
		t.Fatalf("contents: %v", err)
	}
	seen := map[string]int{}
	for _, r := range rows {
		seen[r.BinCode+"/"+r.SKU] = r.Qty
	}
	if seen["TEST-MV-A/"+sku] != 6 || seen["TEST-MV-B/"+lotSKU] != 2 {
		t.Fatalf("GetBinContents disagrees with the moves: %+v", seen)
	}
}
