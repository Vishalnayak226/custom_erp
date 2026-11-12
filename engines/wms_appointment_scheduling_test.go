package engines

import (
	"encoding/json"
	"testing"

	"custom_erp/db"
)

// Stage 42.3 (Inbound depth) tests - DockDoor/Appointment scheduling,
// YardCheckIn transitions, Hold/HoldReleaseRequest's direct-edit guard,
// hazmat putaway compatibility, catch weight, and receipt validation
// tolerance. Same db.InitDB/tenantID="default" convention as
// wms_task_spine_p2_test.go.

func TestAppointmentScheduling(t *testing.T) {
	if db.DB == nil {
		db.InitDB(testConnStr())
	}
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}
	const doorCode = "DOOR-SCHED-TEST"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'DockDoor' AND data->>'code' = '" + doorCode + "'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Appointment' AND data->>'dock_door' = '" + doorCode + "'")
	}
	cleanup()
	defer cleanup()

	// Unknown door is refused.
	if err := validateAppointmentMasterRules(tenantID, "APPT-0", map[string]interface{}{
		"dock_door": doorCode, "appointment_type": "Inbound", "appointment_date": "2026-09-01", "start_time": "09:00", "end_time": "10:00", "status": "Scheduled",
	}); err == nil {
		t.Error("expected an unknown dock door to be refused")
	}

	doorData, _ := json.Marshal(map[string]interface{}{"code": doorCode, "door_type": "Inbound", "max_concurrent_appointments": 1, "status": "Active"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'DockDoor', $2, 'Active', 'system')",
		"DOORDOC-1", doorData); err != nil {
		t.Fatalf("seed DockDoor: %v", err)
	}

	// Outbound appointment against an Inbound-only door is refused.
	if err := validateAppointmentMasterRules(tenantID, "APPT-1", map[string]interface{}{
		"dock_door": doorCode, "appointment_type": "Outbound", "appointment_date": "2026-09-01", "start_time": "09:00", "end_time": "10:00", "status": "Scheduled",
	}); err == nil {
		t.Error("expected an Outbound appointment against an Inbound-only door to be refused")
	}

	// A valid first appointment is accepted.
	if err := validateAppointmentMasterRules(tenantID, "APPT-1", map[string]interface{}{
		"dock_door": doorCode, "appointment_type": "Inbound", "appointment_date": "2026-09-01", "start_time": "09:00", "end_time": "10:00", "status": "Scheduled",
	}); err != nil {
		t.Errorf("expected a valid appointment to be accepted, got %v", err)
	}
	apptData, _ := json.Marshal(map[string]interface{}{"dock_door": doorCode, "appointment_type": "Inbound", "appointment_date": "2026-09-01", "start_time": "09:00", "end_time": "10:00", "status": "Scheduled"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Appointment', $2, 'Scheduled', 'system')",
		"APPT-1", apptData); err != nil {
		t.Fatalf("seed Appointment: %v", err)
	}

	// An overlapping second appointment exceeds the door's capacity of 1.
	if err := validateAppointmentMasterRules(tenantID, "APPT-2", map[string]interface{}{
		"dock_door": doorCode, "appointment_type": "Inbound", "appointment_date": "2026-09-01", "start_time": "09:30", "end_time": "10:30", "status": "Scheduled",
	}); err == nil {
		t.Error("expected an overlapping appointment to be refused at capacity 1")
	}

	// A non-overlapping appointment the same day is fine.
	if err := validateAppointmentMasterRules(tenantID, "APPT-3", map[string]interface{}{
		"dock_door": doorCode, "appointment_type": "Inbound", "appointment_date": "2026-09-01", "start_time": "10:00", "end_time": "11:00", "status": "Scheduled",
	}); err != nil {
		t.Errorf("expected a non-overlapping appointment to be accepted, got %v", err)
	}

	// A Cancelled appointment never counts toward capacity.
	if err := validateAppointmentMasterRules(tenantID, "APPT-4", map[string]interface{}{
		"dock_door": doorCode, "appointment_type": "Inbound", "appointment_date": "2026-09-01", "start_time": "09:30", "end_time": "10:30", "status": "Cancelled",
	}); err != nil {
		t.Errorf("expected a Cancelled appointment to skip the capacity check, got %v", err)
	}
}

func TestYardCheckInTransitions(t *testing.T) {
	if db.DB == nil {
		db.InitDB(testConnStr())
	}
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}
	const yciID = "YCI-TRANSITION-TEST"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'YardCheckIn' AND id = '" + yciID + "'")
	}
	cleanup()
	defer cleanup()

	// Departed without checked_out_at is refused, even on create.
	if err := validateYardCheckInMasterRules(tenantID, "", map[string]interface{}{"status": "Departed"}); err == nil {
		t.Error("expected Departed without checked_out_at to be refused")
	}

	data, _ := json.Marshal(map[string]interface{}{"trailer_no": "TR-TEST", "status": "AtDoor"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'YardCheckIn', $2, 'AtDoor', 'system')",
		yciID, data); err != nil {
		t.Fatalf("seed YardCheckIn: %v", err)
	}

	// AtDoor -> Departed with a timestamp is fine.
	if err := validateYardCheckInMasterRules(tenantID, yciID, map[string]interface{}{"status": "Departed", "checked_out_at": "2026-09-01T10:00:00Z"}); err != nil {
		t.Errorf("expected a valid Departed transition to be accepted, got %v", err)
	}
	if _, err := db.DB.Exec("UPDATE "+schema+".documents SET status = 'Departed' WHERE id = $1", yciID); err != nil {
		t.Fatalf("advance fixture to Departed: %v", err)
	}

	// Departed -> InYard (backward) is refused.
	if err := validateYardCheckInMasterRules(tenantID, yciID, map[string]interface{}{"status": "InYard"}); err == nil {
		t.Error("expected a backward transition from Departed to InYard to be refused")
	}
}

func TestHoldDirectEditGuard(t *testing.T) {
	if db.DB == nil {
		db.InitDB(testConnStr())
	}
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}
	const holdID = "HOLD-GUARD-TEST"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Hold' AND id = '" + holdID + "'")
	}
	cleanup()
	defer cleanup()

	data, _ := json.Marshal(map[string]interface{}{"hold_code": "QC", "sku": "SKU-X", "location_code": "HO", "qty": 5, "status": "Active"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Hold', $2, 'Active', 'system')",
		holdID, data); err != nil {
		t.Fatalf("seed Hold: %v", err)
	}

	// A generic edit that tries to release an Active hold is refused - the
	// gap this closes: role_permissions.allow_update alone does not stop
	// HR/Admin, which bypasses RBAC entirely (confirmed live, 2026-08-20).
	if err := validateHoldMasterRules(tenantID, holdID, map[string]interface{}{"status": "Released"}); err == nil {
		t.Error("expected a direct Active -> Released edit to be refused")
	}
	// Editing any other field while leaving status alone is unaffected.
	if err := validateHoldMasterRules(tenantID, holdID, map[string]interface{}{"status": "Active", "reason": "corrected note"}); err != nil {
		t.Errorf("expected a non-release edit to be allowed, got %v", err)
	}

	// ReleaseHold's own direct-SQL path (not through this validator) is what
	// actually performs the transition - verified separately by
	// TestPlaceAndReleaseHold below via the real function, not a bypass of it.
}

func TestPlaceAndReleaseHold(t *testing.T) {
	if db.DB == nil {
		db.InitDB(testConnStr())
	}
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}
	const sku = "SKU-HOLD-ROUNDTRIP"
	const loc = "LOC-HOLD-ROUNDTRIP"
	const holdCode = "HOLD-CODE-ROUNDTRIP"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Hold' AND data->>'sku' = '" + sku + "'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'HoldCode' AND data->>'code' = '" + holdCode + "'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".inventory_availability WHERE sku = '" + sku + "'")
	}
	cleanup()
	defer cleanup()

	hcData, _ := json.Marshal(map[string]interface{}{"code": holdCode, "category": "Quality", "status": "Active"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'HoldCode', $2, 'Active', 'system')",
		"HC-ROUNDTRIP", hcData); err != nil {
		t.Fatalf("seed HoldCode: %v", err)
	}
	if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 20, 20)",
		sku, loc); err != nil {
		t.Fatalf("seed inventory_availability: %v", err)
	}

	holdID, err := PlaceHold(tenantID, holdCode, sku, loc, "", 5, "unit test hold", "system")
	if err != nil {
		t.Fatalf("PlaceHold: %v", err)
	}
	var heldAfterPlace int
	if err := db.DB.QueryRow("SELECT hold_qty FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = $2", sku, loc).Scan(&heldAfterPlace); err != nil {
		t.Fatalf("read hold_qty: %v", err)
	}
	if heldAfterPlace != 5 {
		t.Errorf("expected hold_qty=5 after PlaceHold, got %d", heldAfterPlace)
	}

	if err := ReleaseHold(tenantID, holdID); err != nil {
		t.Fatalf("ReleaseHold: %v", err)
	}
	var heldAfterRelease int
	var status string
	if err := db.DB.QueryRow("SELECT hold_qty FROM "+schema+".inventory_availability WHERE sku = $1 AND location_code = $2", sku, loc).Scan(&heldAfterRelease); err != nil {
		t.Fatalf("read hold_qty after release: %v", err)
	}
	if err := db.DB.QueryRow("SELECT status FROM "+schema+".documents WHERE doctype = 'Hold' AND id = $1", holdID).Scan(&status); err != nil {
		t.Fatalf("read Hold status: %v", err)
	}
	if heldAfterRelease != 0 {
		t.Errorf("expected hold_qty=0 after ReleaseHold, got %d", heldAfterRelease)
	}
	if status != "Released" {
		t.Errorf("expected Hold status Released, got %q", status)
	}
}

func TestHazmatPutawayCompatibility(t *testing.T) {
	if db.DB == nil {
		db.InitDB(testConnStr())
	}
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}
	const sku = "SKU-HAZMAT-TEST"
	const bin = "BIN-HAZMAT-TEST"
	const loc = "LOC-HAZMAT-TEST"
	const zone = "ZONE-HAZMAT-TEST"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".bin_stock WHERE bin_code = '" + bin + "'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".inventory_availability WHERE sku = '" + sku + "'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Bin' AND data->>'bin_code' = '" + bin + "'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Zone' AND data->>'code' = '" + zone + "'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Item' AND data->>'code' = '" + sku + "'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'WarehouseTask' AND data->>'location_code' = '" + loc + "'")
	}
	cleanup()
	defer cleanup()

	itemData, _ := json.Marshal(map[string]interface{}{"code": sku, "name": "Hazmat Test Item", "hazmat_class": "Class 3 Flammable Liquids"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')",
		"ITEM-"+sku, itemData); err != nil {
		t.Fatalf("seed Item: %v", err)
	}
	zoneData, _ := json.Marshal(map[string]interface{}{"code": zone, "hazmat_allowed": "No", "status": "Active"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Zone', $2, 'Active', 'system')",
		"ZONE-"+zone, zoneData); err != nil {
		t.Fatalf("seed Zone: %v", err)
	}
	binData, _ := json.Marshal(map[string]interface{}{"bin_code": bin, "location": loc, "zone": zone})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Bin', $2, 'Active', 'system')",
		"BINDOC-"+bin, binData); err != nil {
		t.Fatalf("seed Bin: %v", err)
	}
	if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 20, 20)",
		sku, loc); err != nil {
		t.Fatalf("seed inventory_availability: %v", err)
	}

	if err := PutawayToBin(tenantID, bin, sku, 5, "system"); err == nil {
		t.Error("expected putaway of a hazmat item into a hazmat_allowed=No zone to be refused")
	}

	// Flip the zone to allow hazmat, and the same putaway succeeds.
	if _, err := db.DB.Exec("UPDATE "+schema+".documents SET data = jsonb_set(data, '{hazmat_allowed}', '\"Yes\"') WHERE doctype = 'Zone' AND data->>'code' = $1", zone); err != nil {
		t.Fatalf("flip zone hazmat_allowed: %v", err)
	}
	if err := PutawayToBin(tenantID, bin, sku, 5, "system"); err != nil {
		t.Errorf("expected putaway to succeed once the zone allows hazmat, got %v", err)
	}
}

func TestCatchWeightAndReceiptTolerance(t *testing.T) {
	if db.DB == nil {
		db.InitDB(testConnStr())
	}
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}
	const sku = "SKU-CATCHWEIGHT-TEST"
	const vendor = "VENDOR-TOLERANCE-TEST"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Item' AND data->>'code' = '" + sku + "'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'ReceiptValidationRule' AND data->>'vendor' = '" + vendor + "'")
	}
	cleanup()
	defer cleanup()

	itemData, _ := json.Marshal(map[string]interface{}{"code": sku, "name": "Catch Weight Test Item", "is_catch_weight": "Yes"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')",
		"ITEM-"+sku, itemData); err != nil {
		t.Fatalf("seed Item: %v", err)
	}

	if err := ValidateReceiptCatchWeightLine(tenantID, sku, nil); err == nil {
		t.Error("expected a missing actual_weight on a catch-weight item to be refused")
	}
	zero := 0.0
	if err := ValidateReceiptCatchWeightLine(tenantID, sku, &zero); err == nil {
		t.Error("expected a zero actual_weight on a catch-weight item to be refused")
	}
	weight := 12.5
	if err := ValidateReceiptCatchWeightLine(tenantID, sku, &weight); err != nil {
		t.Errorf("expected a positive actual_weight to be accepted, got %v", err)
	}

	// No ReceiptValidationRule configured reproduces the pre-42.3.6 default
	// exactly: 0% tolerance, unexpected items blocked.
	tol, allowUnexpected, err := getReceiptValidationRule(tenantID, vendor)
	if err != nil {
		t.Fatalf("getReceiptValidationRule (no rule): %v", err)
	}
	if tol != 0 || allowUnexpected {
		t.Errorf("expected the zero-value default (0%%, blocked) with no rule configured, got tol=%v allowUnexpected=%v", tol, allowUnexpected)
	}

	ruleData, _ := json.Marshal(map[string]interface{}{"vendor": vendor, "over_receipt_tolerance_pct": 15, "allow_unexpected_items": "Yes", "status": "Active"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'ReceiptValidationRule', $2, 'Active', 'system')",
		"RVR-"+vendor, ruleData); err != nil {
		t.Fatalf("seed ReceiptValidationRule: %v", err)
	}
	tol, allowUnexpected, err = getReceiptValidationRule(tenantID, vendor)
	if err != nil {
		t.Fatalf("getReceiptValidationRule (with rule): %v", err)
	}
	if tol != 15 || !allowUnexpected {
		t.Errorf("expected the configured rule (15%%, allowed) to be resolved, got tol=%v allowUnexpected=%v", tol, allowUnexpected)
	}
}
