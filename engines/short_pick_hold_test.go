package engines

import (
	"custom_erp/db"
	"fmt"
	"testing"
)

// Stage 146.1.5: with wms.short_pick_hold on, a short pick holds the missing
// pieces out of sale and raises a count task; off, it only releases the
// reservation, as before.
func TestStage14615ShortPickHoldsMissingPieces(t *testing.T) {
	if db.DB == nil {
		db.InitDB(testConnStr())
	}
	const tenantID = "default"
	schema, _ := db.GetTenantSchema(tenantID)
	reason := ensureShortPickReason(t, schema)

	run := func(t *testing.T, on bool, sku, bin, loc string) (ats int, holds int, counts int) {
		cleanup := func() {
			_, _ = db.DB.Exec("DELETE FROM "+schema+".bin_allocation WHERE sku = $1", sku)
			_, _ = db.DB.Exec("DELETE FROM "+schema+".bin_stock WHERE sku = $1", sku)
			_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'FulfillmentTask' AND data->>'items' LIKE '%' || $1 || '%'", sku)
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'Hold' AND data->>'sku' = $1", sku)
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'WarehouseTask' AND data->>'item' = $1", sku)
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'Bin' AND data->>'bin_code' = $1", bin)
		}
		cleanup()
		t.Cleanup(cleanup)
		val := "false"
		if on {
			val = "true"
		}
		if err := SetSetting(tenantID, ShortPickHoldSetting, val, "system"); err != nil {
			t.Fatalf("set: %v", err)
		}
		t.Cleanup(func() { _ = SetSetting(tenantID, ShortPickHoldSetting, "false", "system") })
		seedBinWithStock(t, schema, bin, sku, loc, 6)
		taskID, err := CreateFulfillmentTasks(tenantID, "ORD-"+sku, loc, []interface{}{map[string]interface{}{"sku": sku, "qty": 4}})
		if err != nil {
			t.Fatalf("task: %v", err)
		}
		if err := ReserveTaskBinStock(tenantID, taskID); err != nil {
			t.Fatalf("reserve: %v", err)
		}
		if err := ShortPickLine(tenantID, taskID, sku, reason); err != nil {
			t.Fatalf("short pick: %v", err)
		}
		m, err := GetAvailableToSell(tenantID, sku, loc)
		if err != nil {
			t.Fatalf("ats: %v", err)
		}
		ats = m["ats"].(int)
		_ = db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".documents WHERE doctype = 'Hold' AND status = 'Active' AND data->>'hold_code' = 'NOT-FOUND' AND data->>'sku' = $1", sku).Scan(&holds)
		_ = db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".documents WHERE doctype = 'WarehouseTask' AND data->>'task_type' = 'Count' AND data->>'item' = $1", sku).Scan(&counts)
		return
	}

	t.Run("On", func(t *testing.T) {
		ats, holds, counts := run(t, true, "SKU-S1465-ON", "BIN-S1465-ON", "WH-S1465-ON")
		if ats != 2 || holds != 1 || counts != 1 {
			t.Fatalf("6 in the bin, 4 short-picked: want ats 2 (4 held), 1 hold, 1 count task; got ats %d, %d holds, %d count tasks", ats, holds, counts)
		}
	})
	t.Run("Off", func(t *testing.T) {
		ats, holds, counts := run(t, false, "SKU-S1465-OFF", "BIN-S1465-OFF", "WH-S1465-OFF")
		if ats != 6 || holds != 0 || counts != 0 {
			t.Fatalf("setting off: want ats 6, no hold, no count task; got ats %d, %d holds, %d count tasks", ats, holds, counts)
		}
	})
	t.Run("CountReleasesHold", func(t *testing.T) {
		const sku, bin, loc = "SKU-S1465-REL", "BIN-S1465-REL", "WH-S1465-REL"
		ats, holds, _ := run(t, true, sku, bin, loc)
		if ats != 2 || holds != 1 {
			t.Fatalf("setup: ats %d holds %d", ats, holds)
		}
		releaseShortPickHolds(tenantID, sku, loc)
		m, _ := GetAvailableToSell(tenantID, sku, loc)
		var open int
		_ = db.DB.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s.documents WHERE doctype = 'WarehouseTask' AND data->>'item' = $1 AND status IN ('Pending','Assigned','In Progress')", schema), sku).Scan(&open)
		if m["ats"].(int) != 6 || open != 0 {
			t.Fatalf("after the count: want ats 6 and no open count task; got ats %v, %d open", m["ats"], open)
		}
	})
}
