package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// Stage 26.5.12/26.5.13/26.5.15/26.5.16 (WMS Enterprise Maturity Sprint P2
// follow-up) engine tests. Kept in its own file, same convention
// wms_enterprise_test.go/reports_stage26_10_test.go already established
// for staying out of engines_test.go while other concurrent work is
// mid-editing it.
func TestWMSP2(t *testing.T) {
	connStr := testConnStr()
	db.InitDB(connStr)

	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}

	seedBin := func(binCode, location, binType, ownerID string) {
		data := map[string]interface{}{"bin_code": binCode, "location": location, "bin_type": binType, "capacity": 1000}
		if ownerID != "" {
			data["owner_id"] = ownerID
		}
		b, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Bin', $2, 'Active', 'system') ON CONFLICT (id) DO UPDATE SET data = $2", "BINDOC-"+binCode, b); err != nil {
			t.Fatalf("failed to seed bin %s: %v", binCode, err)
		}
	}
	seedBinStock := func(binCode, sku, location string, qty int) {
		if _, err := db.DB.Exec("INSERT INTO "+schema+".bin_stock (bin_code, sku, location_code, condition, qty) VALUES ($1,$2,$3,'Good',$4) ON CONFLICT (bin_code, sku, condition) DO UPDATE SET qty = $4", binCode, sku, location, qty); err != nil {
			t.Fatalf("failed to seed bin_stock: %v", err)
		}
	}
	seedPaidCart := func(id, sku, location string, qty float64) {
		items, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "qty": qty}})
		data, _ := json.Marshal(map[string]interface{}{"code": id, "items": string(items), "location": location})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'POSCart', $2, 'Paid', 'system')", id, data); err != nil {
			t.Fatalf("failed to seed POSCart: %v", err)
		}
	}

	t.Run("SlottingSuggestions", func(t *testing.T) {
		location := "WH-SLOT-TEST"
		skuFast := "SKU-SLOT-FAST"
		skuSlow := "SKU-SLOT-SLOW"
		pickBin := "BIN-SLOT-PICK"
		reserveBin := "BIN-SLOT-RESERVE"
		_, _ = db.DB.Exec("DELETE FROM "+schema+".bin_stock WHERE sku IN ($1,$2)", skuFast, skuSlow)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id IN ('BINDOC-"+pickBin+"','BINDOC-"+reserveBin+"') OR (doctype='POSCart' AND data->>'code' LIKE 'CART-SLOT-%')")

		seedBin(pickBin, location, "PickFace", "")
		seedBin(reserveBin, location, "Reserve", "")
		// Fast mover sitting only in Reserve - should be suggested INTO PickFace.
		seedBinStock(reserveBin, skuFast, location, 50)
		// Slow mover sitting in PickFace - should be suggested OUT to Reserve.
		seedBinStock(pickBin, skuSlow, location, 20)
		// Give the fast SKU real sales so it actually ranks Tier A (velocity-driven).
		for i := 0; i < 5; i++ {
			seedPaidCart("CART-SLOT-FAST-"+string(rune('a'+i)), skuFast, location, 10)
		}
		if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1,$2,100,100) ON CONFLICT (sku,location_code) DO UPDATE SET on_hand=100, available=100", skuFast, location); err != nil {
			t.Fatalf("failed to seed inventory_availability for %s: %v", skuFast, err)
		}
		if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1,$2,20,20) ON CONFLICT (sku,location_code) DO UPDATE SET on_hand=20, available=20", skuSlow, location); err != nil {
			t.Fatalf("failed to seed inventory_availability for %s: %v", skuSlow, err)
		}

		suggestions, err := GetSlottingSuggestions(tenantID, location)
		if err != nil {
			t.Fatalf("GetSlottingSuggestions failed: %v", err)
		}
		var sawFastSuggestion, sawSlowSuggestion bool
		for _, s := range suggestions {
			if s.Sku == skuFast && s.Action == "Move to PickFace" {
				sawFastSuggestion = true
				if s.ToBinCode != pickBin {
					t.Errorf("expected fast-mover suggestion to target %s, got %s", pickBin, s.ToBinCode)
				}
			}
			if s.Sku == skuSlow && s.Action == "Move to Reserve" {
				sawSlowSuggestion = true
			}
		}
		if !sawFastSuggestion {
			t.Errorf("expected a 'Move to PickFace' suggestion for %s, got %+v", skuFast, suggestions)
		}
		if !sawSlowSuggestion {
			t.Errorf("expected a 'Move to Reserve' suggestion for %s, got %+v", skuSlow, suggestions)
		}
	})

	t.Run("LaborProductivityLogging", func(t *testing.T) {
		userID := "manager1"
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'TaskCompletionLog' AND data->>'user_id' = $1", userID)

		logTaskCompletion(tenantID, "Putaway", userID, "WH-PROD-TEST", "BIN-X", 5)
		logTaskCompletion(tenantID, "Putaway", userID, "WH-PROD-TEST", "BIN-Y", 3)

		var count int
		if err := db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".documents WHERE doctype = 'TaskCompletionLog' AND data->>'user_id' = $1", userID).Scan(&count); err != nil {
			t.Fatalf("failed to count TaskCompletionLog rows: %v", err)
		}
		if count != 2 {
			t.Errorf("expected 2 TaskCompletionLog rows, got %d", count)
		}

		productivity, err := GetLaborProductivity(tenantID, "", "")
		if err != nil {
			t.Fatalf("GetLaborProductivity failed: %v", err)
		}
		found := false
		for _, p := range productivity {
			if p.UserID == userID && p.TaskType == "Putaway" {
				found = true
				if p.TaskCount != 2 {
					t.Errorf("expected task_count=2, got %d", p.TaskCount)
				}
			}
		}
		if !found {
			t.Errorf("expected a productivity row for %s/Putaway, got %+v", userID, productivity)
		}
	})

	t.Run("StorageBillingReport", func(t *testing.T) {
		owner := "OWNER-3PL-TEST"
		location := "WH-3PL-TEST"
		bin := "BIN-3PL-TEST"
		sku := "SKU-3PL-TEST"
		rateID := "RATE-3PL-TEST"
		_, _ = db.DB.Exec("DELETE FROM "+schema+".bin_stock WHERE sku = $1", sku)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id IN ($1, 'BINDOC-"+bin+"')", rateID)

		seedBin(bin, location, "Reserve", owner)
		seedBinStock(bin, sku, location, 10)

		rateData, _ := json.Marshal(map[string]interface{}{
			"code": rateID, "owner_id": owner, "location_code": location,
			"storage_rate_per_unit_per_day": 2, "handling_rate_per_task": 5,
		})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'StorageBillingRate', $2, 'Active', 'system')", rateID, rateData); err != nil {
			t.Fatalf("failed to seed StorageBillingRate: %v", err)
		}

		rows, err := GetStorageBillingReport(tenantID, owner, "2020-01-01", "2020-01-05") // 5-day fixed window, independent of real "today"
		if err != nil {
			t.Fatalf("GetStorageBillingReport failed: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("expected exactly 1 billing row for owner %s, got %+v", owner, rows)
		}
		r := rows[0]
		if r.CurrentUnits != 10 {
			t.Errorf("expected current_units=10, got %d", r.CurrentUnits)
		}
		if r.Days != 5 {
			t.Errorf("expected days=5, got %d", r.Days)
		}
		wantStorage := 10.0 * 2 * 5
		if r.StorageCharge != wantStorage {
			t.Errorf("expected storage_charge=%v, got %v", wantStorage, r.StorageCharge)
		}
	})

	// Stage 42.1.10 - a rate that names both an Item and a BillingUOM counts
	// only that item's units (not the whole location) and bills against the
	// converted quantity, e.g. "$5 per CASE/day" on 24 EA of stock = 2 CASE.
	t.Run("StorageBillingReportWithUOM", func(t *testing.T) {
		owner := "OWNER-3PL-UOM-TEST"
		location := "WH-3PL-UOM-TEST"
		bin := "BIN-3PL-UOM-TEST"
		sku := "SKU-3PL-UOM-TEST"
		rateID := "RATE-3PL-UOM-TEST"
		_, _ = db.DB.Exec("DELETE FROM "+schema+".bin_stock WHERE sku = $1", sku)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id IN ($1, 'BINDOC-"+bin+"')", rateID)
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'UOMConversion' AND data->>'item' = '" + sku + "'")
		defer func() {
			_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'UOMConversion' AND data->>'item' = '" + sku + "'")
		}()

		seedBin(bin, location, "Reserve", owner)
		seedBinStock(bin, sku, location, 24)

		convData, _ := json.Marshal(map[string]interface{}{
			"item": sku, "from_uom": "CASE", "to_uom": "EA", "factor": 12, "status": "Active",
		})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'UOMConversion', $2, 'Active', 'system')",
			"UOMCONV-3PL-1", convData); err != nil {
			t.Fatalf("seed UOMConversion: %v", err)
		}
		rateData, _ := json.Marshal(map[string]interface{}{
			"code": rateID, "owner_id": owner, "location_code": location,
			"storage_rate_per_unit_per_day": 5, "handling_rate_per_task": 0,
			"item": sku, "billing_uom": "CASE",
		})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'StorageBillingRate', $2, 'Active', 'system')",
			rateID, rateData); err != nil {
			t.Fatalf("seed StorageBillingRate: %v", err)
		}

		rows, err := GetStorageBillingReport(tenantID, owner, "2020-01-01", "2020-01-05")
		if err != nil {
			t.Fatalf("GetStorageBillingReport failed: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("expected exactly 1 billing row for owner %s, got %+v", owner, rows)
		}
		r := rows[0]
		if r.CurrentUnits != 24 {
			t.Errorf("expected current_units=24 (raw each-count, unchanged), got %d", r.CurrentUnits)
		}
		if r.BillingUOM != "CASE" || r.BillingUnits != 2 {
			t.Errorf("expected billing_uom=CASE, billing_units=2 (24 EA / 12), got %q / %v", r.BillingUOM, r.BillingUnits)
		}
		wantStorage := 2.0 * 5 * 5 // 2 CASE * $5/CASE/day * 5 days
		if r.StorageCharge != wantStorage {
			t.Errorf("expected storage_charge=%v (billed by CASE, not EA), got %v", wantStorage, r.StorageCharge)
		}
	})

	t.Run("RoboticsEvents", func(t *testing.T) {
		apiKey := "test-robotics-key-12345"
		credID := "ROBOT-CRED-TEST"
		binCode := "BIN-ROBOT-TEST"
		sku := "SKU-ROBOT-TEST"
		location := "WH-ROBOT-TEST"
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id IN ($1, 'BINDOC-"+binCode+"')", credID)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".bin_stock WHERE sku = $1", sku)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)

		credData, _ := json.Marshal(map[string]interface{}{"code": credID, "api_key": apiKey})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'RoboticsIntegrationCredential', $2, 'Active', 'system')", credID, credData); err != nil {
			t.Fatalf("failed to seed RoboticsIntegrationCredential: %v", err)
		}
		if !VerifyRoboticsAPIKey(tenantID, apiKey) {
			t.Error("expected the seeded API key to verify")
		}
		if VerifyRoboticsAPIKey(tenantID, "wrong-key") {
			t.Error("expected a wrong API key to fail verification")
		}

		seedBin(binCode, location, "PickFace", "")
		if _, err := PostInventoryLedgerWithVoucher(tenantID, location, []interface{}{map[string]interface{}{"sku": sku, "qty": 20.0}}, false, "StockAdjustment", "ROBOT-SEED", "tester"); err != nil {
			t.Fatalf("failed to seed on-hand stock: %v", err)
		}

		result, err := ProcessRoboticsPutaway(tenantID, binCode, sku, 15, "DEVICE-1")
		if err != nil {
			t.Fatalf("ProcessRoboticsPutaway failed: %v", err)
		}
		if result.Status != "putaway_confirmed" {
			t.Errorf("expected status=putaway_confirmed, got %s", result.Status)
		}
		var binQty int
		if err := db.DB.QueryRow("SELECT qty FROM "+schema+".bin_stock WHERE bin_code = $1 AND sku = $2 AND condition = 'Good'", binCode, sku).Scan(&binQty); err != nil {
			t.Fatalf("failed to read bin_stock: %v", err)
		}
		if binQty != 15 {
			t.Errorf("expected bin_stock qty=15 after robotics putaway, got %d", binQty)
		}

		matchResult, err := ProcessRoboticsWeightConfirm(tenantID, binCode, sku, 15, "SCALE-1")
		if err != nil {
			t.Fatalf("ProcessRoboticsWeightConfirm (match) failed: %v", err)
		}
		if matchResult.Status != "weight_confirmed" {
			t.Errorf("expected weight_confirmed, got %s", matchResult.Status)
		}
		mismatchResult, err := ProcessRoboticsWeightConfirm(tenantID, binCode, sku, 999, "SCALE-1")
		if err != nil {
			t.Fatalf("ProcessRoboticsWeightConfirm (mismatch) failed: %v", err)
		}
		if mismatchResult.Status != "weight_mismatch" {
			t.Errorf("expected weight_mismatch, got %s", mismatchResult.Status)
		}
	})
}
