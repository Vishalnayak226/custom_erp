package engines

import (
	"testing"

	"custom_erp/db"
)

// TestStage107StockLocationPhysical: In-Transit is never physically in the
// store and a client cannot say otherwise; a client's own location chooses,
// and a stock count's expected qty leaves out stock in one marked not here.
func TestStage107StockLocationPhysical(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	const code, sku, store = "QA107AWAY", "QA107-SKU", "HO"
	cleanup := func() {
		db.DB.Exec("DELETE FROM "+schema+".stock_location_balance WHERE stock_location = $1", code)
		db.DB.Exec("DELETE FROM "+schema+".stock_locations WHERE code = $1", code)
	}
	cleanup()
	defer cleanup()

	find := func(c string) StockLocation {
		all, err := ListStockLocations("default")
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range all {
			if l.Code == c {
				return l
			}
		}
		t.Fatalf("location %s not listed", c)
		return StockLocation{}
	}

	transit := find("INTRANSIT")
	if !transit.NotPhysical || transit.PhysicalEditable {
		t.Errorf("In-Transit: not_physical=%v editable=%v, want true/false", transit.NotPhysical, transit.PhysicalEditable)
	}
	if main := find("MAIN"); main.NotPhysical || main.PhysicalEditable {
		t.Errorf("Main: not_physical=%v editable=%v, want false/false", main.NotPhysical, main.PhysicalEditable)
	}
	transit.NotPhysical = false
	if err := SaveStockLocation("default", transit, "test"); err != nil {
		t.Fatal(err)
	}
	if !find("INTRANSIT").NotPhysical {
		t.Errorf("In-Transit was saved as physically in the store - it must stay not physical")
	}

	if err := SaveStockLocation("default", StockLocation{Code: code, Name: "QA 107 At repair", NotPhysical: true, AppliesTo: "Both"}, "test"); err != nil {
		t.Fatal(err)
	}
	away := find(code)
	if !away.NotPhysical || !away.PhysicalEditable {
		t.Errorf("own location: not_physical=%v editable=%v, want true/true", away.NotPhysical, away.PhysicalEditable)
	}

	if _, err := db.DB.Exec("INSERT INTO "+schema+".stock_location_balance (sku, location_code, stock_location, qty) VALUES ($1, $2, $3, 3)", sku, store, code); err != nil {
		t.Fatal(err)
	}
	if n := notPhysicalCustomQty(schema, sku, store); n != 3 {
		t.Errorf("count leaves out %d, want 3 (in a location marked not here)", n)
	}
	away.NotPhysical = false
	if err := SaveStockLocation("default", away, "test"); err != nil {
		t.Fatal(err)
	}
	if n := notPhysicalCustomQty(schema, sku, store); n != 0 {
		t.Errorf("count leaves out %d after marking the location in the store, want 0", n)
	}
}
