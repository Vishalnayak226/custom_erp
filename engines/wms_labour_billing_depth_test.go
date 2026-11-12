package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// TestStage42_6LabourBillingDepth exercises the phase's joined-up path rather
// than only its helpers: component masters -> open-task labour plan -> a
// completed owner task charge -> snapshot-backed storage -> captured-charge
// SalesInvoice/GL posting. Codes carry one unique run suffix so this test is
// safe against the shared local development database.
func TestStage42_6LabourBillingDepth(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	// The focused engine test is runnable before an operator applies the
	// embedded migration to a shared local database. Production gains these
	// registrations exclusively through migrations_stage42_6_*.sql; this small
	// idempotent setup only satisfies documents.doctype's FK for the test data.
	for _, doctype := range []string{
		"LaborOperation", "LaborElement", "LaborAllowance", "TravelSection", "LaborStandard",
		"Shift", "WeeklySchedule", "UserWorkSchedule", "ChargeCode", "StorageBillingRate",
		"CapturedCharge", "StorageBalanceSnapshot", "TaskCompletionLog",
	} {
		if _, err := db.DB.Exec("INSERT INTO "+schema+".doctype_meta (name, module, module_key, document_type) VALUES ($1,'Inventory','inventory','Master') ON CONFLICT (name) DO NOTHING", doctype); err != nil {
			t.Fatalf("register test doctype %s: %v", doctype, err)
		}
	}
	suffix := NewDocIDCompact("426")
	owner, location, bin, item := "OWNER-"+suffix, "WH-"+suffix, "BIN-"+suffix, "SKU-"+suffix
	opCode, elementCode, allowanceCode, travelCode, standardCode := "OP-"+suffix, "EL-"+suffix, "AL-"+suffix, "TR-"+suffix, "STD-"+suffix
	shiftCode, scheduleCode, chargeCode, manualChargeCode, rateCode := "SH-"+suffix, "WS-"+suffix, "CC-"+suffix, "MC-"+suffix, "SR-"+suffix
	today := time.Now().Format("2006-01-02")
	invoiceID := "INV-CHG-" + stage42ShortHash("CapturedCharges:"+owner+":"+today+":"+today)
	t.Cleanup(func() {
		// This package's tests share the local dev database. Remove only the
		// exact random fixture namespace and its posted GL rows, so a test run
		// cannot appear in a developer's real WMS or finance reports afterward.
		_, _ = db.DB.Exec("DELETE FROM "+schema+".gl_postings WHERE document_id = $1", invoiceID)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".bin_stock WHERE bin_code = $1", bin)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE (doctype = 'WarehouseTask' AND data->>'location_code' = $1) OR data->>'owner_id' = $2 OR data->>'customer' = $2 OR data->>'user_id' = $3", location, owner, "labor-"+suffix)
		for _, id := range []string{
			"DOC-" + opCode, "DOC-" + elementCode, "DOC-" + allowanceCode, "DOC-" + travelCode, "DOC-" + standardCode,
			"DOC-" + shiftCode, "DOC-" + scheduleCode, "DOC-UWS-" + suffix, "DOC-" + chargeCode, "DOC-" + manualChargeCode,
			"DOC-" + rateCode, "DOC-" + bin, "TCL-1-" + suffix, "TCL-2-" + suffix,
		} {
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
	})

	seed := func(doctype, id string, data map[string]interface{}, status string) {
		payload, _ := json.Marshal(data)
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1,$2,$3,$4,'system')`, schema), id, doctype, payload, status); err != nil {
			t.Fatalf("seed %s: %v", doctype, err)
		}
	}
	seed("LaborOperation", "DOC-"+opCode, map[string]interface{}{"code": opCode, "name": "Move", "task_type": "Move", "department": "Inbound", "labor_rate_per_hour": 120.0}, "Active")
	seed("LaborElement", "DOC-"+elementCode, map[string]interface{}{"code": elementCode, "operation_code": opCode, "description": "Place", "standard_seconds_per_unit": 10.0, "fixed_seconds": 5.0}, "Active")
	seed("LaborAllowance", "DOC-"+allowanceCode, map[string]interface{}{"code": allowanceCode, "description": "Personal", "allowance_pct": 10.0, "fixed_seconds": 2.0}, "Active")
	seed("TravelSection", "DOC-"+travelCode, map[string]interface{}{"code": travelCode, "description": "Aisle", "seconds_per_task": 3.0, "seconds_per_unit": 1.0}, "Active")
	seed("LaborStandard", "DOC-"+standardCode, map[string]interface{}{"code": standardCode, "name": "Putaway standard", "operation_code": opCode, "element_codes": elementCode, "allowance_codes": allowanceCode, "travel_section_code": travelCode}, "Active")
	seed("Shift", "DOC-"+shiftCode, map[string]interface{}{"code": shiftCode, "name": "Day", "start_time": "08:00", "end_time": "16:30", "unpaid_break_minutes": 30.0}, "Active")
	seed("WeeklySchedule", "DOC-"+scheduleCode, map[string]interface{}{"code": scheduleCode, "name": "Inbound day", "department": "Inbound", "shift_code": shiftCode, "work_days": "Mon,Tue,Wed,Thu,Fri"}, "Active")
	seed("UserWorkSchedule", "DOC-UWS-"+suffix, map[string]interface{}{"code": "UWS-" + suffix, "user_id": "system", "weekly_schedule_code": scheduleCode, "effective_from": "2020-01-01"}, "Active")
	seed("ChargeCode", "DOC-"+chargeCode, map[string]interface{}{"code": chargeCode, "name": "Move handling", "trigger_event": "Warehouse Task Completed", "task_type": "Move", "owner_id": owner, "location_code": location, "default_rate": 2.0, "tax_rate": 18.0}, "Active")
	seed("ChargeCode", "DOC-"+manualChargeCode, map[string]interface{}{"code": manualChargeCode, "name": "Manual accessorial", "trigger_event": "Manual", "owner_id": owner, "location_code": location, "default_rate": 1.0, "tax_rate": 0.0}, "Active")
	seed("StorageBillingRate", "DOC-"+rateCode, map[string]interface{}{"code": rateCode, "owner_id": owner, "location_code": location, "storage_rate_per_unit_per_day": 1.5, "handling_rate_per_task": 0.0, "tax_rate": 18.0}, "Active")
	seed("Bin", "DOC-"+bin, map[string]interface{}{"bin_code": bin, "location": location, "owner_id": owner, "bin_type": "PickFace", "capacity": 100}, "Active")
	if _, err := db.DB.Exec("INSERT INTO "+schema+".bin_stock (bin_code, sku, location_code, condition, qty) VALUES ($1,$2,$3,'Good',10)", bin, item, location); err != nil {
		t.Fatalf("seed bin stock: %v", err)
	}

	resolved, err := ResolveLaborStandardTime(tenantID, standardCode, 2)
	if err != nil || resolved.TotalSeconds != 35 { // (5 + 20) + (3 + 2) + 10%*30 + 2
		t.Fatalf("engineered standard = %+v, %v; want 35 seconds", resolved, err)
	}
	laborUser := "labor-" + suffix
	seed("TaskCompletionLog", "TCL-1-"+suffix, map[string]interface{}{"code": "TCL-1-" + suffix, "task_type": "Move", "location_code": location, "user_id": laborUser, "qty": 2.0}, "Active")
	seed("TaskCompletionLog", "TCL-2-"+suffix, map[string]interface{}{"code": "TCL-2-" + suffix, "task_type": "Move", "location_code": location, "user_id": laborUser, "qty": 2.0}, "Active")
	metrics, err := stage42TaskMetrics(tenantID, today, today)
	if err != nil {
		t.Fatalf("labour metrics: %v", err)
	}
	var metricSeconds float64
	for _, metric := range metrics {
		if metric.UserID == laborUser {
			metricSeconds += metric.StandardSeconds
		}
	}
	if metricSeconds != 70 { // fixed task components must count once per completion, not once per aggregation group
		t.Fatalf("labour report standard seconds = %v, want 70", metricSeconds)
	}
	if _, err := CreateWarehouseTask(tenantID, NewWarehouseTask{TaskType: "Move", LocationCode: location, Qty: 2}, "system"); err != nil {
		t.Fatalf("create open task: %v", err)
	}
	plans, err := GetLaborPlan(tenantID, location, "2026-08-17", "2026-08-21")
	if err != nil || len(plans) != 1 || plans[0].Department != "Inbound" || plans[0].ForecastHeadcount != 1 {
		t.Fatalf("labour plan = %+v, %v", plans, err)
	}

	LogCompletedWarehouseTask(tenantID, NewWarehouseTask{TaskType: "Move", LocationCode: location, ToBin: bin, Qty: 2}, "system")
	charges, err := ListCapturedCharges(tenantID, owner, "", "", "Captured")
	if err != nil || len(charges) != 1 || charges[0].TotalAmount != 4.72 {
		t.Fatalf("automatic task charge = %+v, %v; want total 4.72", charges, err)
	}

	if count, err := CaptureStorageBalanceSnapshot(tenantID, today, "system"); err != nil || count < 1 {
		t.Fatalf("snapshot count=%d err=%v", count, err)
	}
	storageRows, err := GetStorageBillingV2(tenantID, owner, today, today)
	if err != nil || len(storageRows) != 1 || storageRows[0].AverageUnits != 10 || storageRows[0].TotalAmount != 17.7 {
		t.Fatalf("storage billing = %+v, %v; want 10 units and total 17.70", storageRows, err)
	}
	if count, err := CaptureStorageCharges(tenantID, owner, today, today, "system"); err != nil || count != 1 {
		t.Fatalf("capture storage count=%d err=%v", count, err)
	}

	invoice, err := GenerateInvoiceFromCapturedCharges(tenantID, owner, today, today, "system")
	if err != nil || invoice.InvoiceState != "Approved" || invoice.ChargeCount != 2 || invoice.TotalAmount != 22.42 {
		t.Fatalf("charge invoice = %+v, %v; want approved total 22.42", invoice, err)
	}
	charges, err = ListCapturedCharges(tenantID, owner, today, today, "Billed")
	if err != nil || len(charges) != 2 {
		t.Fatalf("billed charges = %+v, %v", charges, err)
	}
	lateID, err := CaptureManualCharge(tenantID, "late-"+suffix, manualChargeCode, owner, location, 1, today, "system")
	if err != nil {
		t.Fatalf("capture late manual charge: %v", err)
	}
	retry, err := GenerateInvoiceFromCapturedCharges(tenantID, owner, today, today, "system")
	if err != nil || retry.ChargeCount != 2 || retry.TotalAmount != 22.42 {
		t.Fatalf("invoice retry swallowed a later charge: %+v, %v", retry, err)
	}
	late, err := ListCapturedCharges(tenantID, owner, today, today, "Captured")
	if err != nil || len(late) != 1 || late[0].ID != lateID {
		t.Fatalf("later charge should remain captured for the next batch: %+v, %v", late, err)
	}
}
