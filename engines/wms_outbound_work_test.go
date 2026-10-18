package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestOutboundLabourBillingStage558 covers 55.8 on a provisioned throwaway
// client: a wave is built from the location's waveable tasks and refuses the
// rest by name; a labour standard's worked example matches the engine's
// total; and a billing review shows each charge's arithmetic and lists the
// minimum-charge, missing-evidence and missing-snapshot cases to check.
func TestOutboundLabourBillingStage558(t *testing.T) {
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
	const loc = "OB-558"
	task := func(id, status, location, wave string) {
		items, _ := json.Marshal([]map[string]interface{}{{"sku": "S558", "qty": 2}})
		d := map[string]interface{}{"order_id": "SO-" + id, "location_code": location, "status": status, "items": json.RawMessage(items)}
		if wave != "" {
			d["wave_id"] = wave
		}
		insert(id, "FulfillmentTask", status, d)
	}
	task("T1", "Pending", loc, "")
	task("T2", "Picking", loc, "")
	task("T3", "Pending", "ELSEWHERE", "")
	task("T4", "Packed", loc, "")
	task("T5", "Pending", loc, "W-OLD")

	work, err := OutboundWork(tenantID, loc)
	if err != nil || len(work.Eligible) != 2 || work.Eligible[0].Units != 2 {
		t.Fatalf("eligible: %v %+v", err, work.Eligible)
	}
	res, err := CreateWaveFromSelection(tenantID, loc, "", []string{"T1", "T3", "T4", "T5"}, "planner")
	if err != nil || len(res.Tagged) != 1 || res.Tagged[0] != "T1" || len(res.Refused) != 3 ||
		res.Refused["T3"] != "at ELSEWHERE" || res.Refused["T4"] != "already Packed" || res.Refused["T5"] != "in wave W-OLD" {
		t.Fatalf("new wave: %v %+v", err, res)
	}
	wave := res.WaveID
	if res, err := CreateWaveFromSelection(tenantID, loc, wave, []string{"T2"}, "planner"); err != nil || len(res.Tagged) != 1 {
		t.Fatalf("add to open wave: %v %+v", err, res)
	}
	if _, err := CreateWaveFromSelection(tenantID, "ELSEWHERE", wave, []string{"T3"}, "planner"); code(err) != "INVENT-0499" {
		t.Fatalf("wave of another location: %v", err)
	}
	if _, err := CreateWaveFromSelection(tenantID, loc, "", []string{"T4"}, "planner"); code(err) != "INVENT-0499" {
		t.Fatalf("nothing waveable: %v", err)
	}
	work, _ = OutboundWork(tenantID, loc)
	var found *OutboundWave
	for i := range work.Waves {
		if work.Waves[i].WaveID == wave {
			found = &work.Waves[i]
		}
	}
	if found == nil || found.Status != "Planned" || found.Tasks != 2 || found.Remaining != 2 || found.ByStatus["Picking"] != 1 || len(work.Eligible) != 0 {
		t.Fatalf("open wave: %+v eligible %+v", found, work.Eligible)
	}

	// Labour: 10 units = E1 (5 + 2×10) + E2 (1×10) + travel (30 + 0.5×10)
	// = 70 s, allowance 10% + 4 s = 11 s, total 81 s, cost 81/3600 × 360.
	insert("OP1", "LaborOperation", "Active", map[string]interface{}{"code": "OP1", "name": "Pick", "task_type": "Pick", "labor_rate_per_hour": 360, "status": "Active"})
	insert("E1", "LaborElement", "Active", map[string]interface{}{"code": "E1", "operation_code": "OP1", "description": "Reach", "fixed_seconds": 5, "standard_seconds_per_unit": 2, "status": "Active"})
	insert("E2", "LaborElement", "Active", map[string]interface{}{"code": "E2", "operation_code": "OP1", "description": "Scan", "standard_seconds_per_unit": 1, "status": "Active"})
	insert("TR1", "TravelSection", "Active", map[string]interface{}{"code": "TR1", "description": "Aisle", "seconds_per_task": 30, "seconds_per_unit": 0.5, "status": "Active"})
	insert("AL1", "LaborAllowance", "Active", map[string]interface{}{"code": "AL1", "description": "Rest", "allowance_pct": 10, "fixed_seconds": 4, "status": "Active"})
	insert("ST1", "LaborStandard", "Active", map[string]interface{}{"code": "ST1", "name": "Each pick", "operation_code": "OP1", "element_codes": "E1,E2", "travel_section_code": "TR1", "allowance_codes": "AL1", "status": "Active"})
	p, err := ExplainLaborStandard(tenantID, "ST1", 10)
	if err != nil || !p.Agrees || len(p.Lines) != 4 || p.TotalSeconds != 81 || p.EstimatedLaborCost != 8.1 || p.Lines[3].Seconds != 11 {
		t.Fatalf("labour preview: %v %+v", err, p)
	}

	// Billing.
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	insert("CC-H", "ChargeCode", "Active", map[string]interface{}{"code": "CC-H", "trigger_event": "Manual", "default_rate": 5, "markup_pct": 10, "tax_rate": 18, "status": "Active"})
	insert("CC-M", "ChargeCode", "Active", map[string]interface{}{"code": "CC-M", "trigger_event": "Manual", "default_rate": 1, "minimum_charge": 50, "status": "Active"})
	if _, err := CaptureManualCharge(tenantID, "EV-1", "CC-H", "OWN1", loc, 4, today, "billing"); err != nil {
		t.Fatalf("capture: %v", err)
	}
	if _, err := CaptureManualCharge(tenantID, "EV-2", "CC-M", "OWN1", loc, 2, today, "billing"); err != nil {
		t.Fatalf("capture minimum: %v", err)
	}
	insert("CHG-OLD", "CapturedCharge", "Captured", map[string]interface{}{"code": "CHG-OLD", "event_key": "EV-0", "trigger_event": "Manual", "charge_code": "CC-H",
		"owner_id": "OWN1", "quantity": 1, "net_amount": 5, "tax_amount": 0, "total_amount": 5, "occurred_on": today, "status": "Captured"})
	insert("SR1", "StorageBillingRate", "Active", map[string]interface{}{"code": "SR1", "owner_id": "OWN1", "location_code": loc, "storage_rate_per_unit_per_day": 0.5, "status": "Active"})
	insert("SR2", "StorageBillingRate", "Active", map[string]interface{}{"code": "SR2", "owner_id": "OWN1", "location_code": "OB-OTHER", "storage_rate_per_unit_per_day": 1, "status": "Active"})
	insert("SBS-1", "StorageBalanceSnapshot", "Captured", map[string]interface{}{"snapshot_date": today, "owner_id": "OWN1", "location_code": loc, "item": "S558", "quantity": 10, "status": "Captured"})

	if _, err := BillingReview(tenantID, "", yesterday, today); code(err) != "INVENT-0500" {
		t.Fatalf("no owner: %v", err)
	}
	rev, err := BillingReview(tenantID, "OWN1", yesterday, today)
	if err != nil || len(rev.Charges) != 3 {
		t.Fatalf("review: %v %+v", err, rev)
	}
	kinds := map[string]bool{}
	for _, x := range rev.Exceptions {
		kinds[x.Kind] = true
	}
	for _, k := range []string{"Minimum charge applied", "No rate evidence", "Missing storage days", "No storage snapshots"} {
		if !kinds[k] {
			t.Fatalf("missing exception %q in %+v", k, rev.Exceptions)
		}
	}
	for _, c := range rev.Charges {
		if c.EventKey == "EV-1" && !strings.HasPrefix(c.Explanation, "4 × 5.00 = 20.00; +10% markup → net 22.00; tax 18% = 3.96; total 25.96") {
			t.Fatalf("explanation: %q", c.Explanation)
		}
	}
	if rev.NotInvoiced != 25.96+50+5 {
		t.Fatalf("not invoiced total: %v", rev.NotInvoiced)
	}
	for _, s := range rev.Storage {
		if s.RateCode == "SR1" && (s.SnapshotDays != 1 || s.PeriodDays != 2 || s.TotalAmount != 5) {
			t.Fatalf("storage row: %+v", s)
		}
	}
}
