package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// Stage 135.23: a newly provisioned client gets the built-in stock locations
// and their transaction defaults, so its first Move Stock works.
func TestStage135NewClientHasStockLocations(t *testing.T) {
	tenantID, schema, insert := stage135Tenant(t)

	var wantLocations, wantDefaults int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.stock_locations WHERE bucket <> 'custom'`).Scan(&wantLocations); err != nil || wantLocations == 0 {
		t.Fatalf("tenant_default has no built-in stock locations (%v)", err)
	}
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.stock_location_defaults`).Scan(&wantDefaults)

	locations, err := ListStockLocations(tenantID)
	if err != nil {
		t.Fatalf("ListStockLocations: %v", err)
	}
	codes := map[string]bool{}
	for _, l := range locations {
		codes[l.Code] = true
	}
	if len(locations) != wantLocations || !codes["MAIN"] || !codes["DAMAGE"] {
		t.Fatalf("new client lists %d locations %v, want the %d built-in ones", len(locations), codes, wantLocations)
	}
	var defaults int
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".stock_location_defaults").Scan(&defaults)
	if defaults != wantDefaults {
		t.Fatalf("new client has %d transaction defaults, want %d", defaults, wantDefaults)
	}

	const store, sku = "S13523-STORE", "S13523-SKU"
	insert(store, "Location", "Active", map[string]interface{}{"code": store, "name": "S13523 Store", "type": "Store", "status": "Active"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 10, 10)", sku, store); err != nil {
		t.Fatalf("stock: %v", err)
	}
	if _, err := MoveStock(tenantID, StockMoveRequest{Store: store, SKU: sku, Qty: 3, From: "MAIN", To: "DAMAGE", Reason: "test"}, "system", "tester"); err != nil {
		t.Fatalf("Move Stock at a new client: %v", err)
	}
	if _, available, _ := stage135Stock(t, schema, sku, store); available != 7 {
		raw, _ := json.Marshal(locations)
		t.Fatalf("available after moving 3 to Damage = %d, want 7 (locations %s)", available, raw)
	}
}
