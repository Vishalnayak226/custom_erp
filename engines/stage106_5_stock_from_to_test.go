package engines

import (
	"database/sql"
	"encoding/json"
	"testing"

	"custom_erp/db"
)

// Stage 106.5: a document's own From / To (picked at the top of its screen)
// moves its stock for that document only, where the setting's row may be
// changed; a locked side ignores it; the header lists the store's choices.
func TestStage1065DocumentStockFromTo(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const (
		store = "S1065-STORE"
		sku   = "S1065-SKU"
		alter = "S1065ALT"
		docID = "S1065-GRN"
	)
	restore := func() {
		db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
		db.DB.Exec("DELETE FROM "+schema+".stock_location_balance WHERE sku = $1", sku)
		db.DB.Exec("DELETE FROM "+schema+".store_stock_locations WHERE location_code = $1", store)
		db.DB.Exec("DELETE FROM "+schema+".stock_locations WHERE code = $1", alter)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE id IN ($1, $2) OR (doctype = 'StockMove' AND data->>'location' = $1)", store, docID)
		invalidateATSPolicy(schema)
	}
	restore()
	defer restore()

	raw, _ := json.Marshal(map[string]interface{}{"code": store, "name": "S1065 Store", "type": "Store", "status": "Active"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Location', $2, 'Active', 'system')", store, raw); err != nil {
		t.Fatalf("location: %v", err)
	}
	if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 10, 10)", sku, store); err != nil {
		t.Fatalf("stock: %v", err)
	}
	if err := SaveStockLocation(tenantID, StockLocation{Code: alter, Name: "S1065 Display", AppliesTo: "Both", DefaultForNew: true}, "tester"); err != nil {
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
	inTx := func(f func(tx *sql.Tx) error) {
		t.Helper()
		tx, err := db.DB.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if err := f(tx); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}

	// The header: Goods Receipt's To is a drop-down of the store's locations,
	// Transfer In's From (In-Transit) is fixed text.
	h, err := StockTxnHeaderFor(tenantID, "Goods Receipt", store)
	if err != nil || h.From != nil || h.To == nil || !h.To.Editable {
		t.Fatalf("Goods Receipt header: want an editable To only, got %+v %v", h, err)
	}
	found := false
	for _, o := range h.To.Options {
		found = found || o.Code == alter
	}
	if !found {
		t.Errorf("the store's own location is a choice: %+v", h.To.Options)
	}
	if h, _ := StockTxnHeaderFor(tenantID, "Transfer In", store); h.From == nil || h.From.Editable || h.From.Code != "INTRANSIT" {
		t.Errorf("Transfer In From is fixed In-Transit, got %+v", h.From)
	}

	// A goods receipt naming its own To lands there; the setting stays Main.
	grn, _ := json.Marshal(map[string]interface{}{"stock_to": alter})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'GRN', $2, 'Draft', 'system')", docID, grn); err != nil {
		t.Fatalf("grn: %v", err)
	}
	inTx(func(tx *sql.Tx) error {
		return applyReceiptDefaultTx(tx, schema, "Goods Receipt", store, []PostedStockLine{{SKU: sku, Qty: 4}}, docStockField(tx, schema, docID, "stock_to"))
	})
	if locQty(alter) != 4 || locQty("MAIN") != 6 {
		t.Errorf("receipt to %s: %d (want 4), Main %d (want 6)", alter, locQty(alter), locQty("MAIN"))
	}

	// An outbound document taking its own From first: 3 of the 4 there.
	inTx(func(tx *sql.Tx) error {
		return drawFromDefaultFromTx(tx, schema, "Transfer Out", store, []interface{}{map[string]interface{}{"sku": sku, "qty": float64(-3)}}, alter)
	})
	if locQty(alter) != 1 || locQty("MAIN") != 9 {
		t.Errorf("draw 3 from %s: %d (want 1), Main %d (want 9)", alter, locQty(alter), locQty("MAIN"))
	}

	// Transfer Out's To is locked (In-Transit): a document cannot change it.
	if got := stockOverride("Transfer Out", false, []string{alter}); got != "" {
		t.Errorf("a locked side ignores the document, got %q", got)
	}

	// A return's QC choice is stamped on the document for ApplyReturnQC.
	if err := SetDocumentStockField(tenantID, docID, "stock_to", "main"); err != nil {
		t.Fatal(err)
	}
	inTx(func(tx *sql.Tx) error {
		if got := docStockField(tx, schema, docID, "stock_to"); got != "MAIN" {
			t.Errorf("stamped stock_to: want MAIN, got %q", got)
		}
		return nil
	})
	if err := SetDocumentStockField(tenantID, docID, "price", "1"); err == nil {
		t.Errorf("only stock_from / stock_to may be stamped")
	}
}
