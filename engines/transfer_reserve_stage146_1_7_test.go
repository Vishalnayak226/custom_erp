package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// Stage 146.1.7: with inventory.transfer_reserve_on_approval on, an approved
// transfer holds its stock; dispatch consumes the hold, cancel and a lowered
// quantity release it, an old hold shows on the cockpit. Off = no change.
func TestStage14617ApprovedTransferHoldsStock(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	schema, _ := db.GetTenantSchema(tenantID)
	const sku, from, to = "SKU-S14617", "WH-S14617-FROM", "WH-S14617-TO"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_reservation WHERE sku = $1", sku)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'TO-S14617-%' OR id = 'RC-S14617-CANCEL'")
		_, _ = db.DB.Exec("DELETE FROM "+schema+".stock_ledger WHERE item_id = $1", sku)
	}
	cleanup()
	t.Cleanup(cleanup)
	t.Cleanup(func() {
		_ = SetSetting(tenantID, TransferReserveSetting, "false", "system")
		_ = SetSetting(tenantID, TransferHoldStaleDaysSetting, "3", "system")
	})
	set := func(on bool) {
		v := "false"
		if on {
			v = "true"
		}
		if err := SetSetting(tenantID, TransferReserveSetting, v, "system"); err != nil {
			t.Fatalf("set: %v", err)
		}
	}
	stock := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".inventory_availability WHERE sku = '" + sku + "'")
		if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 10, 10)", sku, from); err != nil {
			t.Fatalf("stock: %v", err)
		}
	}
	transfer := func(id string, qty int) {
		items, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "qty": qty}})
		data, _ := json.Marshal(map[string]interface{}{"id": id, "transfer_number": id, "from_warehouse": from, "to_warehouse": to, "status": "Approved", "items": string(items)})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'TransferOrder', $2, 'Approved', 'system')", id, data); err != nil {
			t.Fatalf("seed transfer: %v", err)
		}
	}
	ats := func() int {
		m, err := GetAvailableToSell(tenantID, sku, from)
		if err != nil {
			t.Fatalf("ats: %v", err)
		}
		return m["ats"].(int)
	}
	reservedRows := func() (n, qty int) {
		_ = db.DB.QueryRow("SELECT COUNT(*), COALESCE(SUM(quantity),0) FROM "+schema+".inventory_reservation WHERE sku = $1", sku).Scan(&n, &qty)
		return
	}

	t.Run("OffChangesNothing", func(t *testing.T) {
		set(false)
		stock()
		transfer("TO-S14617-OFF", 6)
		SyncTransferReservation(tenantID, "TO-S14617-OFF")
		if n, _ := reservedRows(); n != 0 || ats() != 10 {
			t.Fatalf("setting off: want no hold and ats 10, got %d rows, ats %d", n, ats())
		}
	})

	t.Run("OnHoldsLowersAndDispatches", func(t *testing.T) {
		set(true)
		stock()
		transfer("TO-S14617-ON", 6)
		SyncTransferReservation(tenantID, "TO-S14617-ON")
		if n, q := reservedRows(); n != 1 || q != 6 || ats() != 4 {
			t.Fatalf("approved 6 of 10: want 1 hold of 6 and ats 4, got %d rows qty %d ats %d", n, q, ats())
		}
		// a lowered quantity releases the difference
		items, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "qty": 4}})
		_, _ = db.DB.Exec("UPDATE "+schema+".documents SET data = jsonb_set(data, '{items}', to_jsonb($1::text)) WHERE id = 'TO-S14617-ON'", string(items))
		SyncTransferReservation(tenantID, "TO-S14617-ON")
		if _, q := reservedRows(); q != 4 || ats() != 6 {
			t.Fatalf("lowered to 4: want hold 4, ats 6, got hold %d ats %d", q, ats())
		}
		// the stale list: not yet, then 5 days old
		if got := cockpitStaleTransferHolds(tenantID, schema, from); len(got) != 0 {
			t.Fatalf("a fresh hold is not stale: %v", got)
		}
		_, _ = db.DB.Exec("UPDATE "+schema+".inventory_reservation SET created_at = CURRENT_TIMESTAMP - interval '5 days' WHERE sku = $1", sku)
		if got := cockpitStaleTransferHolds(tenantID, schema, from); len(got) != 1 || got[0].TransferID != "TO-S14617-ON" || got[0].Qty != 4 || got[0].Days < 5 {
			t.Fatalf("a 5-day-old hold must be listed: %v", got)
		}
		// dispatch consumes the hold and moves the stock
		if err := DispatchTransferOrder(tenantID, "TO-S14617-ON", "system"); err != nil {
			t.Fatalf("dispatch: %v", err)
		}
		var onHand, reserved int
		_ = db.DB.QueryRow("SELECT on_hand, reserved FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = $2", sku, from).Scan(&onHand, &reserved)
		if n, _ := reservedRows(); n != 0 || onHand != 6 || reserved != 0 || ats() != 6 {
			t.Fatalf("after dispatch: want no hold, on_hand 6, reserved 0, ats 6; got %d rows, on_hand %d, reserved %d, ats %d", n, onHand, reserved, ats())
		}
	})

	t.Run("CancelReleases", func(t *testing.T) {
		set(true)
		stock()
		transfer("TO-S14617-CAN", 7)
		SyncTransferReservation(tenantID, "TO-S14617-CAN")
		if ats() != 3 {
			t.Fatalf("want ats 3 while held, got %d", ats())
		}
		rc, _ := json.Marshal(map[string]interface{}{"code": "RC-S14617-CANCEL", "name": "Cancel", "category": "Cancellation", "status": "Active"})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ('RC-S14617-CANCEL', 'ReasonCode', $1, 'Active', 'system')", rc); err != nil {
			t.Fatalf("reason: %v", err)
		}
		if err := CancelTransferOrder(tenantID, "TO-S14617-CAN", "system", "RC-S14617-CANCEL", ""); err != nil {
			t.Fatalf("cancel: %v", err)
		}
		if n, _ := reservedRows(); n != 0 || ats() != 10 {
			t.Fatalf("after cancel: want no hold and ats 10, got %d rows, ats %d", n, ats())
		}
	})
}
