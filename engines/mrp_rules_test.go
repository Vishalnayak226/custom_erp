package engines

import (
	"custom_erp/db"
	"math"
	"testing"
)

// Stage 114.6: the three MRP methods, the band text and the save check.
func TestMRPRuleMethods(t *testing.T) {
	bands, err := ParseMRPBands("2000=2.5; 500:3\nabove=2")
	if err != nil {
		t.Fatalf("ParseMRPBands: %v", err)
	}
	if len(bands) != 3 || bands[0].UpTo != 500 || bands[1].UpTo != 2000 || bands[2].UpTo != 0 {
		t.Fatalf("bands not ordered with the open one last: %+v", bands)
	}
	cases := []struct {
		name          string
		rule          MRPRule
		price, weight float64
		want          float64
	}{
		{"single multiplier", MRPRule{Method: MRPMethodMultiplier, Multiplier: 3}, 100, 0, 300},
		{"lowest band", MRPRule{Method: MRPMethodBands, Bands: bands}, 400, 0, 1200},
		{"on a band's edge", MRPRule{Method: MRPMethodBands, Bands: bands}, 500, 0, 1500},
		{"middle band", MRPRule{Method: MRPMethodBands, Bands: bands}, 1000, 0, 2500},
		{"above every band", MRPRule{Method: MRPMethodBands, Bands: bands}, 5000, 0, 10000},
		{"weight and price", MRPRule{Method: MRPMethodWeight, RatePerGram: 50, Multiplier: 1.5}, 200, 10, 800},
		{"weight unknown", MRPRule{Method: MRPMethodWeight, RatePerGram: 50, Multiplier: 1.5}, 200, 0, 300},
	}
	for _, c := range cases {
		if got := c.rule.Apply(c.price, c.weight); math.Abs(got-c.want) > 0.001 {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	// No open band: above the last amount takes the last multiplier.
	closed, _ := ParseMRPBands("500=3, 2000=2.5")
	if got := (MRPRule{Method: MRPMethodBands, Bands: closed}).Apply(5000, 0); got != 12500 {
		t.Errorf("above a closed list: got %v, want 12500", got)
	}

	bad := []map[string]interface{}{
		{"name": "x"},
		{"name": "x", "method": MRPMethodMultiplier},
		{"name": "x", "method": MRPMethodMultiplier, "multiplier": "0"},
		{"name": "x", "method": MRPMethodBands, "bands": "five hundred=3"},
		{"name": "x", "method": MRPMethodBands, "bands": "500=3, 500=2"},
		{"name": "x", "method": MRPMethodBands, "bands": "above=2, over=3"},
		{"name": "x", "method": MRPMethodWeight},
	}
	for i, d := range bad {
		if err := ValidateMRPRule(d); err == nil {
			t.Errorf("bad rule %d accepted: %v", i, d)
		} else if ve, ok := err.(*ValidationError); !ok || ve.Code != "MASTER-0896" {
			t.Errorf("bad rule %d: want MASTER-0896, got %v", i, err)
		}
	}
	if err := ValidateMRPRule(map[string]interface{}{"name": "x", "method": MRPMethodBands, "bands": "500=3, above=2"}); err != nil {
		t.Errorf("good rule refused: %v", err)
	}
}

// A Product Type's rule wins over the default; with neither, the markup the
// MRP step always used.
func TestMRPRuleForItemDB(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	item := map[string]interface{}{"product_type": "rings", "category": ""}
	price, gst := 100.0, 3.0
	plain, rule := suggestedMRPFor(tenantID, item, price, gst, 0, 100, "none")
	if rule != "" || plain != suggestedMRP(tenantID, item, price, gst, 100, "none") {
		t.Fatalf("no rule: got %v by %q, want the markup's", plain, rule)
	}

	if _, err := db.DB.Exec(`INSERT INTO ` + schemaName + `.documents (doctype, id, data, status, created_by) VALUES
		('MRPRule', 'MR1', '{"code":"MR1","name":"Times 2","method":"Multiplier","multiplier":2,"is_default":true,"status":"Active"}', 'Active', 'admin'),
		('MRPRule', 'MR2', '{"code":"MR2","name":"Ring bands","method":"Purchase price bands","bands":"100=4, above=3","status":"Active"}', 'Active', 'admin'),
		('ProductType', 'PT1', '{"code":"PT1","name":"Rings","mrp_rule":"MR2","status":"Active"}', 'Active', 'admin')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Ring: 100 x 1.03 = 103, above the 100 band -> x3 = 309.
	if got, by := suggestedMRPFor(tenantID, item, price, gst, 0, 100, "none"); by != "Ring bands" || math.Abs(got-309) > 0.001 {
		t.Errorf("product type's rule: got %v by %q, want 309 by Ring bands", got, by)
	}
	// Another type: the default, 103 x 2 = 206.
	other := map[string]interface{}{"product_type": "Necklace"}
	if got, by := suggestedMRPFor(tenantID, other, price, gst, 0, 100, "none"); by != "Times 2" || math.Abs(got-206) > 0.001 {
		t.Errorf("default rule: got %v by %q, want 206 by Times 2", got, by)
	}
}
