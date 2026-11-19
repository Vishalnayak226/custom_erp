package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// 103.4.1 / 42.5.4: the slotting report suggests a pick face only where the
// fast mover's stock really fits, and still moves a slow mover out of one.
func TestSlottingReportChecksPickFaceRoom(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	const loc, fast, slow = "WH-SLOT-V2", "SKU-SV2-FAST", "SKU-SV2-SLOW"
	clean := func() {
		db.DB.Exec("DELETE FROM "+schema+".bin_stock WHERE location_code = $1", loc)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE (doctype = 'Bin' AND data->>'location' = $1) OR (doctype = 'POSCart' AND data->>'code' LIKE 'CART-SV2-%')", loc)
	}
	clean()
	defer clean()
	bin := func(code, typ string, capacity int) {
		b, _ := json.Marshal(map[string]interface{}{"bin_code": code, "location": loc, "bin_type": typ, "capacity": capacity})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Bin', $2, 'Active', 'system')", "BINDOC-"+code, b); err != nil {
			t.Fatal(err)
		}
	}
	stock := func(binCode, sku string, qty int) {
		if _, err := db.DB.Exec("INSERT INTO "+schema+".bin_stock (bin_code, sku, location_code, condition, qty) VALUES ($1,$2,$3,'Good',$4)", binCode, sku, loc, qty); err != nil {
			t.Fatal(err)
		}
	}
	bin("SV2-PICK-FULL", "PickFace", 10) // too small for the 50 units
	bin("SV2-PICK-OK", "PickFace", 500)
	bin("SV2-RES", "Reserve", 1000)
	stock("SV2-RES", fast, 50)
	stock("SV2-PICK-OK", slow, 5)
	for i := 0; i < 5; i++ {
		// 232.1: items is an array, as a real till cart stores it. As a string
		// the sales were never counted, both SKUs tied at velocity 0 and the
		// tiers fell to row order, which a long-lived database (ci.yml) flips.
		data, _ := json.Marshal(map[string]interface{}{"code": "CART-SV2-" + string(rune('a'+i)), "items": []map[string]interface{}{{"sku": fast, "qty": 10}}, "location": loc})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'POSCart', $2, 'Paid', 'system')", "CART-SV2-"+string(rune('a'+i)), data); err != nil {
			t.Fatal(err)
		}
	}
	for sku, n := range map[string]int{fast: 100, slow: 5} {
		if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1,$2,$3,$3) ON CONFLICT (sku,location_code) DO UPDATE SET on_hand=$3, available=$3", sku, loc, n); err != nil {
			t.Fatal(err)
		}
	}
	defer db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE location_code = $1", loc)

	_, rows, _, err := RunReport(tenantID, "slotting-suggestions", RoleSuperAdmin, "system", map[string]string{"location_code": loc})
	if err != nil {
		t.Fatal(err)
	}
	var in, out bool
	for _, r := range rows {
		if r["sku"] == fast && r["action"] == "Move to PickFace" {
			in = true
			if r["to_bin_code"] != "SV2-PICK-OK" {
				t.Errorf("the fast mover must go to the pick face with room, got %v", r["to_bin_code"])
			}
		}
		if r["sku"] == slow && r["action"] == "Move to Reserve" {
			out = true
		}
	}
	if !in || !out {
		t.Errorf("want the fast mover into the roomy pick face (%v) and the slow mover out (%v): %+v", in, out, rows)
	}
}
