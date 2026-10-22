package engines

import (
	"encoding/json"
	"testing"

	"custom_erp/db"
)

// Stage 91.3: the guided setup's steps in the decided order, the tax step
// held open by a legal entity without its GSTIN, and optional steps never
// marked Next.
func TestStage913GuidedSetupSteps(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	if _, err := db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype IN ('LegalEntity', 'HSNCode')"); err != nil {
		t.Fatal(err)
	}
	steps, err := GetSetupPath(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	pos := map[string]int{}
	for i, s := range steps {
		pos[s.ID] = i
		if s.Note == "" {
			t.Errorf("step %s says what it asks", s.ID)
		}
		if s.Optional && s.Next {
			t.Errorf("optional step %s is never Next", s.ID)
		}
	}
	order := []string{"entity", "locations", "tax", "numbering", "users", "items", "approvals", "printing"}
	for i := 1; i < len(order); i++ {
		if pos[order[i-1]] >= pos[order[i]] {
			t.Errorf("%s comes before %s", order[i-1], order[i])
		}
	}

	doc := func(id, doctype string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', 'system')", id, doctype, raw); err != nil {
			t.Fatal(err)
		}
	}
	taxDone := func() bool {
		steps, _ := GetSetupPath(tenantID)
		return steps[pos["tax"]].Done
	}
	doc("LE-913", "LegalEntity", map[string]interface{}{"code": "LE-913", "name": "Acme Pvt Ltd"})
	doc("HSN-913", "HSNCode", map[string]interface{}{"code": "HSN-913", "hsn_code": "6109", "gst_rate": 5})
	if taxDone() {
		t.Errorf("a legal entity without its GSTIN keeps the tax step open")
	}
	if _, err := db.DB.Exec(`UPDATE ` + schema + `.documents SET data = data || '{"gstin":"29ABCDE1234F1Z5"}' WHERE id = 'LE-913'`); err != nil {
		t.Fatal(err)
	}
	if !taxDone() {
		t.Errorf("GSTIN and an HSN code finish the tax step")
	}
}
