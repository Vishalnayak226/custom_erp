package engines

import (
	"custom_erp/db"
	"fmt"
	"testing"
	"time"
)

// Stage 147.1: the OMS order line's Available is the SKU's ATS everywhere -
// each location's figure is what GetAvailableToSell gives it, the total their
// sum, and a location with none to sell is left out.
func TestNetworkAvailableToSellSumsEveryLocation(t *testing.T) {
	db.InitDB(testConnStr())
	sku := fmt.Sprintf("S147-ATS-%d", time.Now().UnixNano())
	rows := []struct {
		loc                                  string
		available, reserved, safety, blocked int
	}{
		{"HO", 12, 2, 1, 1},     // 8 to sell
		{"S147-WH", 5, 0, 0, 0}, // 5
		{"S147-ST", 2, 2, 0, 0}, // none
	}
	for _, r := range rows {
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.inventory_availability
			(sku, location_code, on_hand, available, committed, reserved, safety_stock,
			 blocked, qc_hold, damaged, channel_buffer, hold_qty)
			VALUES ($1, $2, $3, $3, 0, $4, $5, $6, 0, 0, 0, 0)`, sku, r.loc, r.available, r.reserved, r.safety, r.blocked); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.DB.Exec(`DELETE FROM tenant_default.inventory_availability WHERE sku = $1`, sku)
	})

	got, err := GetNetworkAvailableToSell("default", sku)
	if err != nil {
		t.Fatal(err)
	}
	want := 0
	perLoc := map[string]int{}
	for _, r := range rows {
		one, err := GetAvailableToSell("default", sku, r.loc)
		if err != nil {
			t.Fatal(err)
		}
		ats := one["ats"].(int)
		perLoc[r.loc] = ats
		if ats > 0 {
			want += ats
		}
	}
	if got["ats"] != want || want == 0 {
		t.Fatalf("ats = %v, want %d (per location %v)", got["ats"], want, perLoc)
	}
	locs := got["locations"].([]LocationATS)
	if len(locs) != 2 || locs[0].LocationCode != "HO" || locs[0].ATS != perLoc["HO"] || locs[1].LocationCode != "S147-WH" {
		t.Fatalf("locations = %+v, want HO then S147-WH, none for S147-ST (per location %v)", locs, perLoc)
	}

	none, err := GetNetworkAvailableToSell("default", sku+"-NONE")
	if err != nil || none["ats"] != 0 || len(none["locations"].([]LocationATS)) != 0 {
		t.Fatalf("unknown SKU: %v %v", none, err)
	}
}
