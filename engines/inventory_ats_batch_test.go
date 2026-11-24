package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// 262.1: the till's item list asks for many SKUs' ATS in one call. Each figure
// must be exactly what GetAvailableToSell gives that SKU alone: holds taken off,
// a SKU with no row at the store at 0, and a Virtual bundle from its components.
func TestAvailableToSellManyMatchesOneByOne(t *testing.T) {
	spInitDB()
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	n := time.Now().UnixNano()
	loc := fmt.Sprintf("T262-LOC-%d", n)
	plain := fmt.Sprintf("T262-PLAIN-%d", n)
	held := fmt.Sprintf("T262-HELD-%d", n)
	missing := fmt.Sprintf("T262-NONE-%d", n)
	compA := fmt.Sprintf("T262-A-%d", n)
	compB := fmt.Sprintf("T262-B-%d", n)
	bundle := fmt.Sprintf("T262-BUNDLE-%d", n)
	bundleID := fmt.Sprintf("T262-BUNDLE-DOC-%d", n)
	all := []string{plain, held, missing, compA, compB, bundle}
	t.Cleanup(func() {
		_, _ = db.DB.Exec(`DELETE FROM `+schema+`.inventory_availability WHERE location_code = $1`, loc)
		_, _ = db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE id = $1`, bundleID)
	})
	stock := []struct {
		sku                                        string
		available, reserved, safety, blocked, hold int
	}{
		{plain, 7, 0, 0, 0, 0},
		{held, 10, 3, 1, 2, 1}, // 3 left to sell
		{compA, 5, 0, 0, 0, 0},
		{compB, 4, 0, 0, 0, 0},
	}
	for _, s := range stock {
		if _, err := db.DB.Exec(`INSERT INTO `+schema+`.inventory_availability
			(sku, location_code, on_hand, available, committed, reserved, safety_stock,
			 blocked, qc_hold, damaged, channel_buffer, hold_qty)
			VALUES ($1, $2, $3, $3, 0, $4, $5, $6, 0, 0, 0, $7)`, s.sku, loc, s.available, s.reserved, s.safety, s.blocked, s.hold); err != nil {
			t.Fatal(err)
		}
	}
	var ready bool
	if err := db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM ` + schema + `.doctype_meta WHERE name='ProductBundle')`).Scan(&ready); err != nil {
		t.Fatal(err)
	}
	if ready {
		data, _ := json.Marshal(map[string]interface{}{"code": bundleID, "bundle_sku": bundle, "name": "T262 bundle", "fulfillment_mode": "Virtual",
			"pricing_mode": "Parent Price", "status": "Active", "components": []map[string]interface{}{{"sku": compA, "quantity": 2}, {"sku": compB, "quantity": 1}}})
		if _, err := db.DB.Exec(`INSERT INTO `+schema+`.documents(id,doctype,data,status,created_by) VALUES($1,'ProductBundle',$2,'Active','system')`, bundleID, data); err != nil {
			t.Fatal(err)
		}
	}

	got, err := AvailableToSellMany("default", all, loc)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(all) {
		t.Fatalf("got %d SKUs, want %d: %v", len(got), len(all), got)
	}
	for _, sku := range all {
		one, err := GetAvailableToSell("default", sku, loc)
		want := 0
		if err == nil {
			want = one["ats"].(int)
		}
		if got[sku] != want {
			t.Fatalf("%s: batch ATS %d, one-by-one %d (%v)", sku, got[sku], want, one)
		}
	}
	if got[plain] != 7 || got[held] <= 0 || got[held] >= 10 || got[missing] != 0 {
		t.Fatalf("plain=%d held=%d missing=%d, want 7, between 1 and 9, 0", got[plain], got[held], got[missing])
	}
	if ready && got[bundle] != 2 {
		t.Fatalf("virtual bundle ATS = %d, want 2 (5 of A at 2 each, 4 of B)", got[bundle])
	}

	empty, err := AvailableToSellMany("default", nil, loc)
	if err != nil || len(empty) != 0 {
		t.Fatalf("no SKUs: %v, %v", empty, err)
	}
}
