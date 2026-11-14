package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// 170.15: the AQL table (general level II, single, normal) and an inspection
// that takes its sample size and result from it.

func TestAQLSamplePlanTable(t *testing.T) {
	cases := []struct {
		lot        int
		aql        string
		letter     string
		sample, ac int
	}{
		{1000, "1.0", "J", 80, 2},
		{2000, "2.5", "K", 125, 7},
		{5000, "1.0", "L", 200, 5},
		{100000, "2.5", "N", 500, 21},
		{1000, "1", "J", 80, 2}, // the spelling "1" is the table's 1.0
	}
	for _, c := range cases {
		p, err := AQLSamplePlan(c.lot, c.aql)
		if err != nil {
			t.Fatalf("lot %d AQL %s: %v", c.lot, c.aql, err)
		}
		if p.CodeLetter != c.letter || p.SampleSize != c.sample || p.AcceptNo != c.ac || p.RejectNo != c.ac+1 {
			t.Errorf("lot %d AQL %s: got %+v, want letter %s sample %d accept %d", c.lot, c.aql, p, c.letter, c.sample, c.ac)
		}
	}
	// A small lot is inspected whole; a bad AQL or lot size is refused.
	if p, _ := AQLSamplePlan(5, "2.5"); !p.FullInspection || p.SampleSize != 5 {
		t.Errorf("lot of 5: %+v", p)
	}
	if _, err := AQLSamplePlan(100, "3.3"); err == nil || err.(*ValidationError).Code != "QUALIT-1760" {
		t.Errorf("bad AQL: %v", err)
	}
	if _, err := AQLSamplePlan(0, "1.0"); err == nil || err.(*ValidationError).Code != "QUALIT-1761" {
		t.Errorf("bad lot: %v", err)
	}
	// Property: for one AQL the sample never exceeds the lot and, among plans
	// that sample (not the whole lot), the accept number never falls as the lot grows.
	for _, aql := range AQLLevels() {
		prev := -1
		for _, lot := range []int{2, 9, 16, 26, 51, 91, 151, 281, 501, 1201, 3201, 10001, 35001, 150001, 500001, 900000} {
			p, err := AQLSamplePlan(lot, aql)
			if err != nil || p.SampleSize > lot {
				t.Errorf("lot %d AQL %s: %+v %v", lot, aql, p, err)
				continue
			}
			if !p.FullInspection {
				if p.AcceptNo < prev {
					t.Errorf("lot %d AQL %s: accept %d fell below %d", lot, aql, p.AcceptNo, prev)
				}
				prev = p.AcceptNo
			}
		}
	}
	p, _ := AQLSamplePlan(1000, "1.0")
	if p.Decide(2) != "Accept" || p.Decide(3) != "Reject" {
		t.Errorf("decide: 2 -> %s, 3 -> %s", p.Decide(2), p.Decide(3))
	}
}

func TestAQLInspectionTakesSampleAndResultFromThePlan(t *testing.T) {
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
	const loc = "QA-170"
	insert(loc, "Location", "Active", map[string]interface{}{"code": loc, "name": "Receiving", "type": "Warehouse"})
	insert("AQLI", "Item", "Active", map[string]interface{}{"code": "AQLI", "name": "Bolt", "hsn_code": "5208", "tax_treatment": "Exempt"})
	items, _ := json.Marshal([]map[string]interface{}{{"sku": "AQLI", "qty": 1000, "accepted_qty": 1000}})
	insert("GRN-170", "GRN", "Approved", map[string]interface{}{"location": loc, "received_items": string(items)})
	insert("IP-AQL", "InspectionPlan", "Active", map[string]interface{}{"code": "IP-AQL", "item": "AQLI", "aql": "1.0", "status": "Active"})
	insert("IP-OLD", "InspectionPlan", "Active", map[string]interface{}{"code": "IP-OLD", "item": "AQLI", "sample_size": 5, "status": "Active"})
	two, three := 2.0, 3.0
	base := InspectionInput{SourceType: InspectIncoming, SourceID: "GRN-170", Item: "AQLI", BatchNo: "B1", Qty: 1000, InspectionPlan: "IP-AQL"}
	stored := func(id string) map[string]interface{} {
		raw := ""
		_ = db.DB.QueryRow("SELECT data::text FROM "+schema+".documents WHERE id = $1", id).Scan(&raw)
		var data map[string]interface{}
		_ = json.Unmarshal([]byte(raw), &data)
		return data
	}

	// No defects count: refused.
	if _, _, err := RecordQualityInspection(tenantID, base, "inspector"); code(err) != "QUALIT-1762" {
		t.Fatalf("no defects: %v", err)
	}
	// 2 defects in the 80 sampled: accept. 3: reject, and the inspection fails.
	base.Defects = &two
	id, res, err := RecordQualityInspection(tenantID, base, "inspector")
	if err != nil || res != "Pass" {
		t.Fatalf("accept: %v %s", err, res)
	}
	data := stored(id)
	if numFromInterface(data["sample_size"]) != 80 || numFromInterface(data["accept_no"]) != 2 || numFromInterface(data["reject_no"]) != 3 || data["aql_result"] != "Accept" {
		t.Fatalf("stored plan: %v", data)
	}
	base.Defects = &three
	if _, res, err = RecordQualityInspection(tenantID, base, "inspector"); err != nil || res != "Fail" {
		t.Fatalf("reject: %v %s", err, res)
	}
	// A plan with only a sample size behaves as it did: no AQL fields, no defects needed.
	old := base
	old.InspectionPlan, old.Defects = "IP-OLD", nil
	old.Readings = []InspectionReading{{Parameter: "Fit", Reading: "ok", Result: "Pass"}}
	id, _, err = RecordQualityInspection(tenantID, old, "inspector")
	if err != nil {
		t.Fatalf("plan without AQL: %v", err)
	}
	if _, has := stored(id)["aql_result"]; has {
		t.Fatalf("plan without AQL got an AQL result: %v", stored(id))
	}
}
