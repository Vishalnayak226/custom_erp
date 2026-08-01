package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// Stage 26.5 (WMS Enterprise Maturity Sprint) engine tests. Kept in its own
// file (rather than appended to engines_test.go, which other concurrent
// Stage 26.12 work was mid-editing) - same db.InitDB/tenantID="default"
// setup convention as TestEngines.
func TestWMSEnterprise(t *testing.T) {
	connStr := testConnStr()
	db.InitDB(connStr)

	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}

	t.Run("GRNReceiptWithQC", func(t *testing.T) {
		sku := "SKU-WMSENT-QC"
		location := "WH01"
		_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)

		items := []interface{}{
			map[string]interface{}{"sku": sku, "qty": 10.0, "accepted_qty": 7.0, "rejected_qty": 2.0, "damaged_qty": 1.0},
		}
		if _, err := PostGRNReceiptWithQC(tenantID, location, items, "system", "GRN-WMSENT-QC"); err != nil {
			t.Fatalf("PostGRNReceiptWithQC failed: %v", err)
		}

		var onHand, available, qcHold, damaged int
		if err := db.DB.QueryRow("SELECT on_hand, available, qc_hold, damaged FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = $2", sku, location).
			Scan(&onHand, &available, &qcHold, &damaged); err != nil {
			t.Fatalf("Failed to read inventory_availability: %v", err)
		}
		if onHand != 10 {
			t.Errorf("expected on_hand=10, got %d", onHand)
		}
		if available != 7 {
			t.Errorf("expected available=7 (accepted qty), got %d", available)
		}
		if qcHold != 2 {
			t.Errorf("expected qc_hold=2 (rejected qty), got %d", qcHold)
		}
		if damaged != 1 {
			t.Errorf("expected damaged=1, got %d", damaged)
		}

		// Backward-compat: a line with no accepted/rejected/damaged split at
		// all still posts its whole qty as accepted, exactly like
		// PostInventoryLedger always did.
		sku2 := "SKU-WMSENT-QC-LEGACY"
		_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku2)
		legacyItems := []interface{}{map[string]interface{}{"sku": sku2, "qty": 5.0}}
		if _, err := PostGRNReceiptWithQC(tenantID, location, legacyItems, "system", "GRN-WMSENT-QC-LEGACY"); err != nil {
			t.Fatalf("PostGRNReceiptWithQC (legacy) failed: %v", err)
		}
		var legacyAvailable int
		if err := db.DB.QueryRow("SELECT available FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = $2", sku2, location).Scan(&legacyAvailable); err != nil {
			t.Fatalf("Failed to read legacy inventory_availability: %v", err)
		}
		if legacyAvailable != 5 {
			t.Errorf("expected legacy available=5 (whole-line accept), got %d", legacyAvailable)
		}
	})

	t.Run("CrossDockPutaway", func(t *testing.T) {
		sku := "SKU-WMSENT-XDOCK"
		location := "WH01"
		_, _ = db.DB.Exec("DELETE FROM "+schema+".bin_stock WHERE sku = $1", sku)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'FulfillmentTask' AND id = 'FT-WMSENT-XDOCK'")

		if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 10, 10)", sku, location); err != nil {
			t.Fatalf("Failed to seed inventory_availability: %v", err)
		}
		taskData, _ := json.Marshal(map[string]interface{}{
			"location_code": location,
			"items":         []map[string]interface{}{{"sku": sku, "qty": 6, "picked_qty": 0, "short_qty": 0}},
		})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'FulfillmentTask', $2, 'Pending', 'system')", "FT-WMSENT-XDOCK", taskData); err != nil {
			t.Fatalf("Failed to seed FulfillmentTask: %v", err)
		}

		matched, opportunities, err := CheckCrossDockOpportunity(tenantID, sku, location)
		if err != nil {
			t.Fatalf("CheckCrossDockOpportunity failed: %v", err)
		}
		if matched != 6 {
			t.Errorf("expected matched qty=6, got %d", matched)
		}
		if len(opportunities) != 1 || opportunities[0].RefID != "FT-WMSENT-XDOCK" {
			t.Errorf("expected one opportunity referencing FT-WMSENT-XDOCK, got %+v", opportunities)
		}

		staged, _, err := CrossDockPutaway(tenantID, sku, location, 10, "system")
		if err != nil {
			t.Fatalf("CrossDockPutaway failed: %v", err)
		}
		if staged != 6 {
			t.Errorf("expected staged=6 (capped to matched demand, not the requested 10), got %d", staged)
		}
		var binQty int
		if err := db.DB.QueryRow("SELECT qty FROM "+schema+".bin_stock WHERE bin_code = $1 AND sku = $2 AND condition = 'Good'", "XDOCK-"+location, sku).Scan(&binQty); err != nil {
			t.Fatalf("Failed to read cross-dock bin_stock: %v", err)
		}
		if binQty != 6 {
			t.Errorf("expected XDOCK bin qty=6, got %d", binQty)
		}
	})

	t.Run("LPNGrouping", func(t *testing.T) {
		sku := "SKU-WMSENT-LPN"
		location := "WH01"
		binCode := "BIN-WMSENT-LPN"
		_, _ = db.DB.Exec("DELETE FROM "+schema+".bin_stock_lpn WHERE bin_code = $1", binCode)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".bin_stock WHERE bin_code = $1", binCode)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".bin_stock (bin_code, sku, location_code, condition, qty) VALUES ($1, $2, $3, 'Good', 20)", binCode, sku, location); err != nil {
			t.Fatalf("Failed to seed bin_stock: %v", err)
		}

		if err := AssignToLPN(tenantID, "LPN-WMSENT-1", binCode, sku, "Good", 5, "system"); err != nil {
			t.Fatalf("AssignToLPN failed: %v", err)
		}
		contents, err := GetLPNContents(tenantID, "LPN-WMSENT-1")
		if err != nil {
			t.Fatalf("GetLPNContents failed: %v", err)
		}
		if len(contents) != 1 || contents[0].Qty != 5 {
			t.Errorf("expected one content line with qty=5, got %+v", contents)
		}

		if err := AssignToLPN(tenantID, "LPN-WMSENT-2", binCode, sku, "Good", 20, "system"); err == nil {
			t.Errorf("expected AssignToLPN to reject exceeding bin's net-of-already-assigned qty, got no error")
		}
	})

	t.Run("BinReplenishment", func(t *testing.T) {
		sku := "SKU-WMSENT-REPL"
		location := "WH01"
		pickFace := "BIN-WMSENT-PF"
		reserve := "BIN-WMSENT-RSV"
		_, _ = db.DB.Exec("DELETE FROM "+schema+".bin_stock WHERE sku = $1", sku)
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'BinReplenishmentRule' AND id = 'BRR-WMSENT-1'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Bin' AND id IN ('BIN-WMSENT-PF-DOC','BIN-WMSENT-RSV-DOC')")

		if _, err := db.DB.Exec("INSERT INTO "+schema+".bin_stock (bin_code, sku, location_code, condition, qty) VALUES ($1, $2, $3, 'Good', 2)", pickFace, sku, location); err != nil {
			t.Fatalf("Failed to seed pick-face bin_stock: %v", err)
		}
		if _, err := db.DB.Exec("INSERT INTO "+schema+".bin_stock (bin_code, sku, location_code, condition, qty) VALUES ($1, $2, $3, 'Good', 50)", reserve, sku, location); err != nil {
			t.Fatalf("Failed to seed reserve bin_stock: %v", err)
		}
		reserveBinDoc, _ := json.Marshal(map[string]interface{}{"bin_code": reserve, "location": location, "status": "Active", "bin_type": "Reserve"})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Bin', $2, 'Active', 'system')", "BIN-WMSENT-RSV-DOC", reserveBinDoc); err != nil {
			t.Fatalf("Failed to seed reserve Bin document: %v", err)
		}
		ruleData, _ := json.Marshal(map[string]interface{}{"bin_code": pickFace, "sku": sku, "min_qty": 5, "max_qty": 20})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'BinReplenishmentRule', $2, 'Active', 'system')", "BRR-WMSENT-1", ruleData); err != nil {
			t.Fatalf("Failed to seed BinReplenishmentRule: %v", err)
		}

		suggestions, err := GetBinReplenishmentSuggestions(tenantID, location)
		if err != nil {
			t.Fatalf("GetBinReplenishmentSuggestions failed: %v", err)
		}
		found := false
		for _, s := range suggestions {
			if s.BinCode == pickFace && s.Sku == sku {
				found = true
				if s.Shortage != 18 {
					t.Errorf("expected shortage=18 (max 20 - current 2), got %d", s.Shortage)
				}
				if s.FromBinCode != reserve || s.MoveQty != 18 {
					t.Errorf("expected to draw 18 from the tagged Reserve bin %s, got from=%s move=%d", reserve, s.FromBinCode, s.MoveQty)
				}
			}
		}
		if !found {
			t.Fatalf("expected a suggestion for bin %s / sku %s, got %+v", pickFace, sku, suggestions)
		}

		if err := ExecuteBinReplenishment(tenantID, reserve, pickFace, sku, 18, "system"); err != nil {
			t.Fatalf("ExecuteBinReplenishment failed: %v", err)
		}
		var pfQty, rsvQty int
		_ = db.DB.QueryRow("SELECT qty FROM "+schema+".bin_stock WHERE bin_code = $1 AND sku = $2 AND condition = 'Good'", pickFace, sku).Scan(&pfQty)
		_ = db.DB.QueryRow("SELECT qty FROM "+schema+".bin_stock WHERE bin_code = $1 AND sku = $2 AND condition = 'Good'", reserve, sku).Scan(&rsvQty)
		if pfQty != 20 || rsvQty != 32 {
			t.Errorf("expected pick-face=20 and reserve=32 after replenishment, got pf=%d rsv=%d", pfQty, rsvQty)
		}
	})

	t.Run("WavePicking", func(t *testing.T) {
		sku := "SKU-WMSENT-WAVE"
		location := "WH01"
		waveID := "WAVE-WMSENT-1"
		_, _ = db.DB.Exec("DELETE FROM "+schema+".bin_stock WHERE sku = $1", sku)
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'FulfillmentTask' AND id IN ('FT-WMSENT-WAVE-1','FT-WMSENT-WAVE-2')")

		if _, err := db.DB.Exec("INSERT INTO "+schema+".bin_stock (bin_code, sku, location_code, condition, qty, updated_at) VALUES ($1, $2, $3, 'Good', 5, NOW() - INTERVAL '2 days')", "BIN-WMSENT-WAVE-OLD", sku, location); err != nil {
			t.Fatalf("Failed to seed old bin_stock: %v", err)
		}
		if _, err := db.DB.Exec("INSERT INTO "+schema+".bin_stock (bin_code, sku, location_code, condition, qty, updated_at) VALUES ($1, $2, $3, 'Good', 10, NOW())", "BIN-WMSENT-WAVE-NEW", sku, location); err != nil {
			t.Fatalf("Failed to seed new bin_stock: %v", err)
		}

		task1, _ := json.Marshal(map[string]interface{}{"location_code": location, "items": []map[string]interface{}{{"sku": sku, "qty": 4}}})
		task2, _ := json.Marshal(map[string]interface{}{"location_code": location, "items": []map[string]interface{}{{"sku": sku, "qty": 8}}})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'FulfillmentTask', $2, 'Pending', 'system')", "FT-WMSENT-WAVE-1", task1); err != nil {
			t.Fatalf("Failed to seed task1: %v", err)
		}
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'FulfillmentTask', $2, 'Pending', 'system')", "FT-WMSENT-WAVE-2", task2); err != nil {
			t.Fatalf("Failed to seed task2: %v", err)
		}

		tagged, err := AssignTasksToWave(tenantID, waveID, []string{"FT-WMSENT-WAVE-1", "FT-WMSENT-WAVE-2"}, "system")
		if err != nil {
			t.Fatalf("AssignTasksToWave failed: %v", err)
		}
		if tagged != 2 {
			t.Errorf("expected 2 tasks tagged, got %d", tagged)
		}

		pickLines, allocations, err := GenerateWavePickList(tenantID, waveID)
		if err != nil {
			t.Fatalf("GenerateWavePickList failed: %v", err)
		}
		totalPick := 0
		for _, pl := range pickLines {
			totalPick += pl.PickQty
		}
		if totalPick != 12 {
			t.Errorf("expected total consolidated pick qty=12 (4+8), got %d", totalPick)
		}
		// FIFO: the older bin (5 units) must be fully consumed before the
		// newer one contributes anything.
		for _, pl := range pickLines {
			if pl.BinCode == "BIN-WMSENT-WAVE-OLD" && pl.PickQty != 5 {
				t.Errorf("expected the older bin to be drained (5), got %d", pl.PickQty)
			}
		}
		totalAlloc := 0
		for _, a := range allocations {
			totalAlloc += a.AllocatedQty
			if a.Shortfall != 0 {
				t.Errorf("expected no shortfall (12 available >= 12 demand), got %+v", a)
			}
		}
		if totalAlloc != 12 {
			t.Errorf("expected total allocated=12, got %d", totalAlloc)
		}
	})

	t.Run("Cartonization", func(t *testing.T) {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'CartonType' AND id = 'CT-WMSENT-1'")
		cartonData, _ := json.Marshal(map[string]interface{}{"code": "BOX-S", "name": "Small Box", "max_qty_capacity": 10})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'CartonType', $2, 'Active', 'system')", "CT-WMSENT-1", cartonData); err != nil {
			t.Fatalf("Failed to seed CartonType: %v", err)
		}

		boxes, err := SuggestCartonization(tenantID, "CT-WMSENT-1", []CartonizationItem{{Sku: "A", Qty: 15}, {Sku: "B", Qty: 3}})
		if err != nil {
			t.Fatalf("SuggestCartonization failed: %v", err)
		}
		totalPacked := 0
		for _, b := range boxes {
			if b.UsedCapacity > b.MaxCapacity {
				t.Errorf("box %s exceeds capacity: used=%d max=%d", b.BoxID, b.UsedCapacity, b.MaxCapacity)
			}
			for _, it := range b.Items {
				totalPacked += it.Qty
			}
		}
		if totalPacked != 18 {
			t.Errorf("expected 18 total units packed across boxes, got %d", totalPacked)
		}
	})

	t.Run("ABCCycleCountPlan", func(t *testing.T) {
		sku := "SKU-WMSENT-ABC"
		location := "WH01"
		_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 10, 10)", sku, location); err != nil {
			t.Fatalf("Failed to seed inventory_availability: %v", err)
		}

		plan, err := GetABCCycleCountPlan(tenantID, location, 30, 60, 90)
		if err != nil {
			t.Fatalf("GetABCCycleCountPlan failed: %v", err)
		}
		found := false
		for _, s := range plan {
			if s.Sku == sku {
				found = true
				if s.Tier != "A" && s.Tier != "B" && s.Tier != "C" {
					t.Errorf("expected a valid tier, got %q", s.Tier)
				}
				if !s.Due {
					t.Errorf("expected a never-counted SKU to be Due, got false")
				}
				if s.DaysSinceLastCount != -1 {
					t.Errorf("expected DaysSinceLastCount=-1 (never counted), got %d", s.DaysSinceLastCount)
				}
			}
		}
		if !found {
			t.Fatalf("expected %s in the ABC plan for %s", sku, location)
		}
	})

	t.Run("BlindRecountAndVarianceReason", func(t *testing.T) {
		sku := "SKU-WMSENT-RECOUNT"
		location := "WH01"
		_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'CycleCountLine' AND data->>'sku' = '" + sku + "'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'ReasonCode' AND id = 'RC-WMSENT-CCVAR'")

		if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 10, 10)", sku, location); err != nil {
			t.Fatalf("Failed to seed inventory_availability: %v", err)
		}
		reasonData, _ := json.Marshal(map[string]interface{}{"code": "RC-CCVAR", "description": "Shrinkage", "category": "Cycle Count Variance", "status": "Active"})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'ReasonCode', $2, 'Active', 'system')", "RC-WMSENT-CCVAR", reasonData); err != nil {
			t.Fatalf("Failed to seed Cycle Count Variance ReasonCode: %v", err)
		}

		// --- Recount workflow ---
		origData, _ := json.Marshal(map[string]interface{}{"count_session": "SESSION-WMSENT", "location": location, "sku": sku, "counted_qty": 3.0, "system_qty": 10, "variance": -7})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'CycleCountLine', $2, 'Pending Approval', 'system')", "CCL-WMSENT-ORIG", origData); err != nil {
			t.Fatalf("Failed to seed original CycleCountLine: %v", err)
		}

		newLineID, err := RequestRecount(tenantID, "CCL-WMSENT-ORIG", "system")
		if err != nil {
			t.Fatalf("RequestRecount failed: %v", err)
		}
		var origStatus string
		_ = db.DB.QueryRow("SELECT status FROM "+schema+".documents WHERE id = $1", "CCL-WMSENT-ORIG").Scan(&origStatus)
		if origStatus != "Recount Requested" {
			t.Errorf("expected original line status='Recount Requested', got %q", origStatus)
		}
		var newDataStr, newStatus string
		if err := db.DB.QueryRow("SELECT data, status FROM "+schema+".documents WHERE id = $1", newLineID).Scan(&newDataStr, &newStatus); err != nil {
			t.Fatalf("Failed to read recount line: %v", err)
		}
		if newStatus != "Draft" {
			t.Errorf("expected recount line status='Draft', got %q", newStatus)
		}
		var newData map[string]interface{}
		_ = json.Unmarshal([]byte(newDataStr), &newData)
		if _, hasCountedQty := newData["counted_qty"]; hasCountedQty {
			t.Errorf("expected recount line to be blind (no carried-over counted_qty), got %v", newData["counted_qty"])
		}
		if newData["recount_of"] != "CCL-WMSENT-ORIG" {
			t.Errorf("expected recount_of='CCL-WMSENT-ORIG', got %v", newData["recount_of"])
		}

		if err := SubmitRecountValue(tenantID, newLineID, 4, "system"); err != nil {
			t.Fatalf("SubmitRecountValue failed: %v", err)
		}
		var recountedQty float64
		_ = db.DB.QueryRow("SELECT (data->>'counted_qty')::numeric FROM "+schema+".documents WHERE id = $1", newLineID).Scan(&recountedQty)
		if recountedQty != 4 {
			t.Errorf("expected recounted counted_qty=4, got %v", recountedQty)
		}

		// --- Variance root-cause gate on PostCycleCountAdjustment ---
		gateData, _ := json.Marshal(map[string]interface{}{"sku": sku, "location": location, "variance": -3})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'CycleCountLine', $2, 'Approved', 'system')", "CCL-WMSENT-GATE", gateData); err != nil {
			t.Fatalf("Failed to seed gate-test CycleCountLine: %v", err)
		}
		if err := PostCycleCountAdjustment(tenantID, "CCL-WMSENT-GATE", "system"); err == nil {
			t.Errorf("expected PostCycleCountAdjustment to reject a line with no variance_reason_code")
		}
		if err := SetCycleCountVarianceReason(tenantID, "CCL-WMSENT-GATE", "RC-WMSENT-CCVAR", "system"); err != nil {
			t.Fatalf("SetCycleCountVarianceReason failed: %v", err)
		}
		beforeAvailable := 0
		_ = db.DB.QueryRow("SELECT available FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = $2", sku, location).Scan(&beforeAvailable)
		if err := PostCycleCountAdjustment(tenantID, "CCL-WMSENT-GATE", "system"); err != nil {
			t.Fatalf("PostCycleCountAdjustment failed after setting variance reason: %v", err)
		}
		var afterAvailable int
		_ = db.DB.QueryRow("SELECT available FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = $2", sku, location).Scan(&afterAvailable)
		if afterAvailable != beforeAvailable-3 {
			t.Errorf("expected available to drop by the -3 variance (from %d to %d), got %d", beforeAvailable, beforeAvailable-3, afterAvailable)
		}
		if err := PostCycleCountAdjustment(tenantID, "CCL-WMSENT-GATE", "system"); err == nil {
			t.Errorf("expected a second PostCycleCountAdjustment call to reject an already-Posted line")
		}
	})
}
