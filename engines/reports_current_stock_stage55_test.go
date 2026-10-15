package engines

import (
	"custom_erp/db"
	"fmt"
	"testing"
	"time"
)

func TestCurrentStockReportUsesReservationSellableMath(t *testing.T) {
	db.InitDB(testConnStr())
	sku := fmt.Sprintf("S55-ATS-%d", time.Now().UnixNano())
	_, err := db.DB.Exec(`INSERT INTO tenant_default.inventory_availability
		(sku, location_code, on_hand, available, committed, reserved, safety_stock,
		 blocked, qc_hold, damaged, channel_buffer, hold_qty)
		VALUES ($1, 'HO', 20, 12, 1, 2, 1, 1, 1, 1, 1, 2)`, sku)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.DB.Exec(`DELETE FROM tenant_default.inventory_availability WHERE sku=$1 AND location_code='HO'`, sku)
	})

	check := func(want int) {
		t.Helper()
		rows, err := GetCurrentStockReport("default")
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if row["sku"] != sku {
				continue
			}
			if got := row["available_to_sell"].(int); got != want {
				t.Fatalf("sellable=%d, want %d", got, want)
			}
			if row["available"].(int) != 12 {
				t.Fatal("base available was overwritten")
			}
			return
		}
		t.Fatalf("report omitted %s", sku)
	}
	check(3)
	if _, err := db.DB.Exec(`UPDATE tenant_default.inventory_availability SET hold_qty=3 WHERE sku=$1 AND location_code='HO'`, sku); err != nil {
		t.Fatal(err)
	}
	check(2)
}
