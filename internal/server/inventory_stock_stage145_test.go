package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// Stage 145.2: Stock on Hand and the Inventory Dashboard read the store picked
// in the top bar whenever the lists follow it - a ?location= cannot widen
// them - and every store, or the one asked for, under "All stores".
func TestStage145InventoryFollowsTopBarStore(t *testing.T) {
	db.InitDB(testConnStr())
	schema, _ := db.GetTenantSchema("default")
	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".inventory_availability WHERE sku LIKE 'T145S-%'")
	}
	cleanup()
	defer cleanup()
	for _, s := range []struct{ sku, loc string }{{"T145S-A", "T145S-STORE-A"}, {"T145S-B", "T145S-STORE-B"}} {
		if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 4, 4)", s.sku, s.loc); err != nil {
			t.Fatalf("stock: %v", err)
		}
	}
	stock := func(storeView, asked string) (string, []string) {
		r := httptest.NewRequest("GET", "/api/v1/inventory/stock?q=t145s-&location="+asked, nil)
		r.Header.Set("Resolved-Tenant-ID", "default")
		r.Header.Set("Resolved-Role", engines.RoleStoreManager)
		r.Header.Set("Resolved-Location", "T145S-STORE-A")
		r.Header.Set("Resolved-Store-View", storeView)
		w := httptest.NewRecorder()
		handleInventoryStock(w, r)
		var out struct {
			Rows  []map[string]interface{} `json:"rows"`
			Store string                   `json:"store"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 {
			t.Fatalf("stock %s: %d %s", storeView, w.Code, w.Body.String())
		}
		skus := []string{}
		for _, row := range out.Rows {
			skus = append(skus, row["sku"].(string))
		}
		return out.Store, skus
	}
	if store, skus := stock(engines.StoreViewStore, "T145S-STORE-B"); store != "T145S-STORE-A" || len(skus) != 1 || skus[0] != "T145S-A" {
		t.Errorf("following the top bar: store %q rows %v, want only T145S-A at T145S-STORE-A", store, skus)
	}
	if store, skus := stock("", ""); store != "" || len(skus) != 2 {
		t.Errorf("All stores: store %q rows %v, want both", store, skus)
	}
	if _, skus := stock("", "T145S-STORE-B"); len(skus) != 1 || skus[0] != "T145S-B" {
		t.Errorf("All stores asking for B: %v", skus)
	}

	r := httptest.NewRequest("GET", "/api/v1/inventory/dashboard?location=T145S-STORE-B", nil)
	r.Header.Set("Resolved-Tenant-ID", "default")
	r.Header.Set("Resolved-Role", engines.RoleStoreManager)
	r.Header.Set("Resolved-Location", "T145S-STORE-A")
	r.Header.Set("Resolved-Store-View", engines.StoreViewStore)
	w := httptest.NewRecorder()
	handleInventoryDashboard(w, r)
	var d map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &d)
	locs, _ := d["by_location"].([]interface{})
	if w.Code != 200 || d["store"] != "T145S-STORE-A" || len(locs) != 1 {
		t.Errorf("dashboard following the top bar: %d store %v by_location %v", w.Code, d["store"], locs)
	}
}
