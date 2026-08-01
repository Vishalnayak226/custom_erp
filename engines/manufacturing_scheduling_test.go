package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
	"time"
)

// Stage 26.9.10/26.9.11 (Manufacturing/MRP Sprint P2 follow-up) engine
// tests. Kept in its own file, same convention wms_enterprise_test.go/
// reports_stage26_10_test.go already established for staying out of
// engines_test.go while other concurrent work is mid-editing it.
func TestManufacturingSchedulingAndSubcontract(t *testing.T) {
	connStr := testConnStr()
	db.InitDB(connStr)

	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}

	t.Run("ProductionSchedule", func(t *testing.T) {
		wcID := "WC-SCHED-TEST"
		routingID := "ROUTE-SCHED-TEST"
		orderA := "PO-SCHED-A"
		orderB := "PO-SCHED-B"
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id IN ('" + wcID + "','" + routingID + "','" + orderA + "','" + orderB + "')")

		wcData, _ := json.Marshal(map[string]interface{}{"code": wcID, "capacity_hours_per_day": 1}) // 60 min/day
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'WorkCenter', $2, 'Active', 'system')", wcID, wcData); err != nil {
			t.Fatalf("failed to seed WorkCenter: %v", err)
		}

		ops := []routingOperation{{Seq: 1, WorkCenterID: wcID, SetupTimeMins: 0, RunTimeMinsPerUnit: 10}}
		opsJSON, _ := json.Marshal(ops)
		routingData, _ := json.Marshal(map[string]interface{}{"code": routingID, "operations": string(opsJSON)})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Routing', $2, 'Active', 'system')", routingID, routingData); err != nil {
			t.Fatalf("failed to seed Routing: %v", err)
		}

		// Order A: qty 4 -> needs 40 min, due tomorrow (sorts first).
		// Order B: qty 4 -> needs 40 min, due later, so A is scheduled
		// first and B's 40 min can't also fit in the same 60 min/day
		// work-center capacity - B must overflow to the next day.
		today := nowDateStr()
		tomorrow := addDaysStr(today, 1)
		nextWeek := addDaysStr(today, 7)
		dataA, _ := json.Marshal(map[string]interface{}{"code": orderA, "routing_id": routingID, "quantity": 4, "location": "HO", "bom_id": "BOM-X", "due_date": tomorrow})
		dataB, _ := json.Marshal(map[string]interface{}{"code": orderB, "routing_id": routingID, "quantity": 4, "location": "HO", "bom_id": "BOM-X", "due_date": nextWeek})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'ProductionOrder', $2, 'Draft', 'system')", orderA, dataA); err != nil {
			t.Fatalf("failed to seed order A: %v", err)
		}
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'ProductionOrder', $2, 'Draft', 'system')", orderB, dataB); err != nil {
			t.Fatalf("failed to seed order B: %v", err)
		}

		entries, err := GetProductionSchedule(tenantID)
		if err != nil {
			t.Fatalf("GetProductionSchedule failed: %v", err)
		}
		var entryA, entryB *ScheduleEntry
		for i := range entries {
			switch entries[i].OrderID {
			case orderA:
				entryA = &entries[i]
			case orderB:
				entryB = &entries[i]
			}
		}
		if entryA == nil || entryB == nil {
			t.Fatalf("expected schedule entries for both orders, got %+v", entries)
		}
		if entryA.FiniteDate != today {
			t.Errorf("expected order A (earlier due date, scheduled first) to land on today (%s) finite, got %s", today, entryA.FiniteDate)
		}
		if entryB.FiniteDate != tomorrow {
			t.Errorf("expected order B to overflow to tomorrow (%s) since A already used the work center's 60min/day capacity, got %s", tomorrow, entryB.FiniteDate)
		}
		// Infinite mode ignores capacity - both land on their own due date, not pushed by each other.
		if entryA.InfiniteDate != tomorrow {
			t.Errorf("expected order A infinite_date to be its own due date (%s), got %s", tomorrow, entryA.InfiniteDate)
		}
		if entryB.InfiniteDate != nextWeek {
			t.Errorf("expected order B infinite_date to be its own due date (%s), got %s", nextWeek, entryB.InfiniteDate)
		}
	})

	t.Run("SubcontractSendReceive", func(t *testing.T) {
		scID := "SC-TEST-1"
		rawSku := "SKU-SC-RAW"
		finishedSku := "SKU-SC-FINISHED"
		location := "HO"
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", scID)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku IN ($1, $2)", rawSku, finishedSku)

		// Seed enough raw material to send.
		if _, err := PostInventoryLedgerWithVoucher(tenantID, location, []interface{}{map[string]interface{}{"sku": rawSku, "qty": 50.0}}, false, "StockAdjustment", "SC-SEED", "tester"); err != nil {
			t.Fatalf("failed to seed raw material stock: %v", err)
		}

		scData, _ := json.Marshal(map[string]interface{}{
			"code": scID, "vendor_id": "VEND-SC", "location": location,
			"sent_item_id": rawSku, "sent_qty": 20.0,
			"received_item_id": finishedSku, "expected_received_qty": 18.0,
		})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'SubcontractOrder', $2, 'Draft', 'system')", scID, scData); err != nil {
			t.Fatalf("failed to seed SubcontractOrder: %v", err)
		}

		if err := SendToSubcontractor(tenantID, scID, "tester"); err != nil {
			t.Fatalf("SendToSubcontractor failed: %v", err)
		}
		var rawAvailable float64
		if err := db.DB.QueryRow("SELECT available FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = $2", rawSku, location).Scan(&rawAvailable); err != nil {
			t.Fatalf("failed to read raw material availability: %v", err)
		}
		if rawAvailable != 30.0 {
			t.Errorf("expected raw material available to drop to 30 (50-20), got %v", rawAvailable)
		}

		// Sending twice must be rejected (no longer Draft).
		if err := SendToSubcontractor(tenantID, scID, "tester"); err == nil {
			t.Error("expected a second Send on an already-Sent subcontract order to fail")
		}

		if err := ReceiveFromSubcontractor(tenantID, scID, "tester", 17); err != nil {
			t.Fatalf("ReceiveFromSubcontractor failed: %v", err)
		}
		var finishedAvailable float64
		if err := db.DB.QueryRow("SELECT available FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = $2", finishedSku, location).Scan(&finishedAvailable); err != nil {
			t.Fatalf("failed to read finished-goods availability: %v", err)
		}
		if finishedAvailable != 17 {
			t.Errorf("expected finished-goods available to be 17, got %v", finishedAvailable)
		}

		var status string
		if err := db.DB.QueryRow("SELECT status FROM "+schema+".documents WHERE id = $1", scID).Scan(&status); err != nil {
			t.Fatalf("failed to read final status: %v", err)
		}
		if status != "Received" {
			t.Errorf("expected final status Received, got %s", status)
		}

		// Stock Ledger should carry both legs, tagged SubcontractOrder.
		ledgerRows, err := GetStockLedgerReport(tenantID, rawSku, location, "SubcontractOrder", "", "")
		if err != nil {
			t.Fatalf("GetStockLedgerReport failed: %v", err)
		}
		if len(ledgerRows) != 1 || ledgerRows[0]["voucher_id"] != scID {
			t.Errorf("expected exactly 1 SubcontractOrder stock ledger row for the raw material leg tagged %s, got %+v", scID, ledgerRows)
		}
	})
}

func nowDateStr() string {
	return time.Now().Format("2006-01-02")
}
func addDaysStr(dateStr string, days int) string {
	t, _ := time.Parse("2006-01-02", dateStr)
	return t.AddDate(0, 0, days).Format("2006-01-02")
}
