package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
	"time"
)

// TestQualityAndMaintenanceStage5513 runs 55.13 on a provisioned throwaway
// client: an incoming inspection against a plan, a failed one that can be
// neither accepted nor conceded by its own inspector, whose hold and
// non-conformance report are raised exactly once; the report closed only on
// evidence; and a maintenance order from schedule to completion that cannot
// skip a checklist task.
func TestQualityAndMaintenanceStage5513(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	code := func(err error) string {
		if ve, ok := err.(*ValidationError); ok {
			return ve.Code
		}
		if err == nil {
			return ""
		}
		return err.Error()
	}
	const loc = "QA-55"
	insert(loc, "Location", "Active", map[string]interface{}{"code": loc, "name": "Receiving", "type": "Warehouse"})
	insert("FAB", "Item", "Active", map[string]interface{}{"code": "FAB", "name": "Cotton fabric", "hsn_code": "5208", "tax_treatment": "Exempt"})
	insert("OTHER", "Item", "Active", map[string]interface{}{"code": "OTHER", "name": "Other", "hsn_code": "5208", "tax_treatment": "Exempt"})
	items, _ := json.Marshal([]map[string]interface{}{{"sku": "FAB", "qty": 10, "accepted_qty": 10}})
	insert("GRN-55", "GRN", "Approved", map[string]interface{}{"location": loc, "received_items": string(items)})
	params, _ := json.Marshal([]map[string]interface{}{{"parameter_name": "GSM", "spec": "140-150"}, {"parameter_name": "Shade", "spec": "matches swatch"}})
	insert("IP-FAB", "InspectionPlan", "Active", map[string]interface{}{"code": "IP-FAB", "item": "FAB", "sample_size": 5, "test_parameters": string(params), "status": "Active"})
	insert("QHOLD", "HoldCode", "Active", map[string]interface{}{"code": "QHOLD", "category": "Quality", "status": "Active"})
	insert("RC-Q", "ReasonCode", "Active", map[string]interface{}{"code": "RC-Q", "category": "Quality", "status": "Active"})
	if _, err := PostInventoryLedger(tenantID, loc, []interface{}{map[string]interface{}{"sku": "FAB", "qty": 10.0}}, false); err != nil {
		t.Fatalf("stock: %v", err)
	}
	holdQty := func() (h int) {
		_ = db.DB.QueryRow("SELECT hold_qty FROM "+schema+".inventory_availability WHERE sku = 'FAB' AND location_code = $1", loc).Scan(&h)
		return
	}
	count := func(doctype string) (n int) {
		_ = db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".documents WHERE doctype = $1", doctype).Scan(&n)
		return
	}
	in := InspectionInput{SourceType: InspectIncoming, SourceID: "GRN-55", Item: "FAB", BatchNo: "L1", Qty: 4, InspectionPlan: "IP-FAB",
		Readings: []InspectionReading{{Parameter: "GSM", Reading: "146", Result: "Pass"}}}

	// The plan's Shade reading is missing; an item not on the GRN is refused.
	if _, _, err := RecordQualityInspection(tenantID, in, "inspector"); code(err) != "QUALIT-0562" {
		t.Fatalf("missing plan reading: %v", err)
	}
	other := in
	other.Item = "OTHER"
	if _, _, err := RecordQualityInspection(tenantID, other, "inspector"); code(err) != "QUALIT-0561" {
		t.Fatalf("item not on GRN: %v", err)
	}
	in.Readings = append(in.Readings, InspectionReading{Parameter: "shade", Reading: "darker", Result: "Fail"})
	failID, result, err := RecordQualityInspection(tenantID, in, "inspector")
	if err != nil || result != "Fail" {
		t.Fatalf("record: %v %s", err, result)
	}
	qi, _, _ := fetchDocData(tenantID, "QualityInspection", failID)
	var readings []InspectionReading
	_ = json.Unmarshal([]byte(strField(qi, "readings")), &readings)
	if len(readings) != 2 || readings[1].Parameter != "Shade" || readings[1].Spec != "matches swatch" {
		t.Fatalf("readings carry the plan's names and specs: %+v", readings)
	}

	// A failed inspection is never accepted, nor conceded by its inspector.
	if _, err := DisposeQualityInspection(tenantID, failID, DispositionAccept, "", "", "manager"); code(err) != "QUALIT-0563" {
		t.Fatalf("accepting a fail: %v", err)
	}
	if _, err := DisposeQualityInspection(tenantID, failID, DispositionConcession, "fine", "", "inspector"); code(err) != "QUALIT-0564" {
		t.Fatalf("self-concession: %v", err)
	}
	ncrsBefore := count("NonConformanceReport")
	dec, err := DisposeQualityInspection(tenantID, failID, DispositionHold, "", "", "manager")
	if err != nil || dec.HoldID == "" || dec.NCRID == "" || holdQty() != 4 {
		t.Fatalf("hold: %v %+v hold_qty=%d", err, dec, holdQty())
	}
	if _, err := DisposeQualityInspection(tenantID, failID, DispositionHold, "", "", "manager"); code(err) != "QUALIT-0563" || holdQty() != 4 || count("NonConformanceReport") != ncrsBefore+1 {
		t.Fatalf("a second disposition must change nothing: %v hold=%d", err, holdQty())
	}
	if ats, _ := computeATSForTest(tenantID, "FAB", loc); ats != 6 {
		t.Fatalf("held stock must not be sellable, ATS %v", ats)
	}

	// A pass is accepted with no hold or report; a concession by someone else
	// releases nothing but still raises the report.
	in.Readings[1].Result = "Pass"
	passID, _, _ := RecordQualityInspection(tenantID, in, "inspector")
	if d, err := DisposeQualityInspection(tenantID, passID, "", "", "", "manager"); err != nil || d.Disposition != DispositionAccept || d.HoldID != "" || d.NCRID != "" {
		t.Fatalf("accept: %v %+v", err, d)
	}
	in.Readings[1].Result = "Fail"
	concID, _, _ := RecordQualityInspection(tenantID, in, "inspector")
	if d, err := DisposeQualityInspection(tenantID, concID, DispositionConcession, "shade within customer tolerance", "", "manager"); err != nil || d.HoldID != "" || d.NCRID == "" || holdQty() != 4 {
		t.Fatalf("concession: %v %+v", err, d)
	}
	prod := InspectionInput{SourceType: InspectProduction, SourceID: "NO-SUCH-PRO"}
	if _, _, err := RecordQualityInspection(tenantID, prod, "inspector"); code(err) != "QUALIT-0561" {
		t.Fatalf("unknown production order: %v", err)
	}

	// The report: owner before plan, evidence before close.
	ncr := dec.NCRID
	if err := InvestigateNonConformanceReport(tenantID, ncr, "RC-Q"); err != nil {
		t.Fatalf("investigate: %v", err)
	}
	if err := PlanCorrectiveActionFor(tenantID, ncr, "tighten dye-lot check", "", ""); code(err) != "QUALIT-0565" {
		t.Fatalf("plan without owner: %v", err)
	}
	if err := PlanCorrectiveActionFor(tenantID, ncr, "tighten dye-lot check", "qa.lead", time.Now().AddDate(0, 0, 7).Format("2006-01-02")); err != nil {
		t.Fatalf("plan: %v", err)
	}
	if err := CloseNonConformanceWithEvidence(tenantID, ncr, " ", "qa.lead"); code(err) != "QUALIT-0565" {
		t.Fatalf("close without evidence: %v", err)
	}
	if err := CloseNonConformanceWithEvidence(tenantID, ncr, "vendor CAPA letter + 3 lots passed", "qa.lead"); err != nil {
		t.Fatalf("close: %v", err)
	}
	n, status, _ := fetchDocData(tenantID, "NonConformanceReport", ncr)
	if status != "Closed" || strField(n, "closed_by") != "qa.lead" || strField(n, "source_id") != failID || strField(n, "batch_no") != "L1" {
		t.Fatalf("closed report: %s %+v", status, n)
	}

	// Maintenance: the schedule's checklist travels to the order it spawns.
	insert("MS-55", "MaintenanceSchedule", "Active", map[string]interface{}{"code": "MS-55", "asset": "AST-1", "description": "Monthly service",
		"interval_days": 30, "next_due_date": time.Now().Format("2006-01-02"), "checklist": "Oil the head\nCheck belt\n", "status": "Active"})
	runMaintenanceSchedulingForSchema(schema)
	var moID string
	_ = db.DB.QueryRow("SELECT id FROM " + schema + ".documents WHERE doctype = 'MaintenanceOrder' AND data->>'maintenance_schedule_id' = 'MS-55'").Scan(&moID)
	mo, _, _ := fetchDocData(tenantID, "MaintenanceOrder", moID)
	var tasks []MaintenanceTask
	if err := json.Unmarshal([]byte(strField(mo, "checklist")), &tasks); err != nil || len(tasks) != 2 {
		t.Fatalf("spawned checklist: %v %+v", err, mo)
	}
	insert("MO-55", "MaintenanceOrder", "Draft", map[string]interface{}{"code": "MO-55", "asset": "AST-1", "description": "Repair", "checklist": strField(mo, "checklist"), "status": "Draft"})
	if err := StartMaintenanceOrderBy(tenantID, "MO-55", "tech"); code(err) != "QUALIT-0566" {
		t.Fatalf("start a draft: %v", err)
	}
	if err := ScheduleMaintenanceOrderOn(tenantID, "MO-55", "tomorrow"); code(err) != "QUALIT-0566" {
		t.Fatalf("bad date: %v", err)
	}
	if err := ScheduleMaintenanceOrderOn(tenantID, "MO-55", time.Now().Format("2006-01-02")); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if err := StartMaintenanceOrderBy(tenantID, "MO-55", "tech"); err != nil {
		t.Fatalf("start: %v", err)
	}
	half := MaintenanceCompletion{Notes: "serviced", Hours: 1.5, Checklist: []MaintenanceTask{{Task: "Oil the head", Done: true}}}
	if err := CompleteMaintenanceOrderWith(tenantID, "MO-55", half, "tech"); code(err) != "QUALIT-0566" {
		t.Fatalf("unticked task: %v", err)
	}
	half.Checklist = append(half.Checklist, MaintenanceTask{Task: "Check belt", Done: true})
	half.Parts = []MaintenancePart{{Sku: "NOPE", Qty: 1}}
	if err := CompleteMaintenanceOrderWith(tenantID, "MO-55", half, "tech"); code(err) != "QUALIT-0566" {
		t.Fatalf("unknown part: %v", err)
	}
	half.Parts = []MaintenancePart{{Sku: "FAB", Qty: 0.5}}
	if err := CompleteMaintenanceOrderWith(tenantID, "MO-55", half, "tech"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	done, status, _ := fetchDocData(tenantID, "MaintenanceOrder", "MO-55")
	if status != "Completed" || strField(done, "completed_by") != "tech" || strField(done, "started_by") != "tech" || numFromInterface(done["labour_hours"]) != 1.5 {
		t.Fatalf("completed order: %s %+v", status, done)
	}
	if err := CompleteMaintenanceOrderWith(tenantID, "MO-55", half, "tech"); code(err) != "QUALIT-0566" {
		t.Fatalf("completing twice: %v", err)
	}
}

// computeATSForTest reads available-to-sell the way the availability API does.
func computeATSForTest(tenantID, sku, loc string) (float64, error) {
	a, err := GetAvailableToSell(tenantID, sku, loc)
	if err != nil {
		return 0, err
	}
	return numFromInterface(a["ats"]), nil
}
