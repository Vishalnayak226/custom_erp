package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
)

// Stage 55.3: the setup path follows dependencies and resumes from data.
func TestSetupPathOrdersPrerequisites(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	stepOf := func(steps []SetupPathStep, id string) SetupPathStep {
		for _, s := range steps {
			if s.ID == id {
				return s
			}
		}
		t.Fatalf("no step %s", id)
		return SetupPathStep{}
	}
	// Clear whatever provisioning seeded for the masters this checks, so the
	// path starts from an empty client.
	for _, dt := range []string{"LegalEntity", "Location", "Department", "CostCenter"} {
		if _, err := db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = $1", dt); err != nil {
			t.Fatal(err)
		}
	}
	steps, err := GetSetupPath(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if e := stepOf(steps, "entity"); e.Done || !e.Next {
		t.Fatalf("an empty client starts at the legal entity: %+v", e)
	}
	if l := stepOf(steps, "locations"); l.Done || len(l.Waiting) != 1 || l.Waiting[0] != "entity" {
		t.Fatalf("locations wait for the entity: %+v", l)
	}
	raw, _ := json.Marshal(map[string]interface{}{"code": "LE-1", "name": "Acme Pvt Ltd"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ('LE-1', 'LegalEntity', $1, 'Active', 'system')", raw); err != nil {
		t.Fatal(err)
	}
	steps, _ = GetSetupPath(tenantID)
	if e := stepOf(steps, "entity"); !e.Done || e.Count != 1 {
		t.Fatalf("entity done after one record: %+v", e)
	}
	if l := stepOf(steps, "locations"); len(l.Waiting) != 0 {
		t.Fatalf("locations no longer wait: %+v", l)
	}
	next := 0
	for _, s := range steps {
		if s.Next {
			next++
		}
	}
	if next != 1 {
		t.Fatalf("exactly one step is Next, got %d", next)
	}
}

// Stage 55.3: one GSTIN, one vendor (compared upper-cased).
func TestVendorDuplicateGSTINRefused(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	raw, _ := json.Marshal(map[string]interface{}{"code": "V-1", "name": "Acme Supplies", "gstin": "27AAPFU0939F1ZV"})
	db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ('V-1', 'Vendor', $1, 'Active', 'system')", raw)
	err := ValidateMasterDataRules(tenantID, "", "Vendor", map[string]interface{}{"name": "Acme again", "gstin": "27aapfu0939f1zv"})
	if v, ok := err.(*ValidationError); !ok || v.Code != "MASTER-0472" {
		t.Fatalf("a second vendor with the same GSTIN must be refused, got %v", err)
	}
	if err := ValidateMasterDataRules(tenantID, "V-1", "Vendor", map[string]interface{}{"name": "Acme Supplies", "gstin": "27AAPFU0939F1ZV"}); err != nil {
		t.Fatalf("the vendor itself may be saved again: %v", err)
	}
}

// Stage 55.0F: a JSON table sent as a real array (an API caller) is accepted
// and kept as the same JSON text the form sends.
func TestJSONTableAcceptsArrayFromAPI(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	payload := map[string]interface{}{"code": "TR-API", "name": "Tidy names", "status": "Active",
		"steps": []interface{}{map[string]interface{}{"sequence": 1, "function": "trim"}}}
	if err := ValidateDocument(tenantID, "PIMTransformRule", payload); err != nil {
		t.Fatalf("an array of steps must be accepted: %v", err)
	}
	text, ok := payload["steps"].(string)
	if !ok || !strings.Contains(text, `"function":"trim"`) {
		t.Fatalf("steps kept as JSON text, got %#v", payload["steps"])
	}
	payload["steps"] = `[{"sequence"`
	if err := ValidateDocument(tenantID, "PIMTransformRule", payload); err == nil {
		t.Fatal("broken JSON text must still be refused")
	}
}

// 55.3 (ask 179): brands, styles and colours are one optional step - waived
// while none exist, done with any one of them, never the Next step and never
// flagged for the registers a client does not use.
func TestStage553RegistersStepIsOptional(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	if _, err := db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype IN ('Brand', 'Style', 'Color')"); err != nil {
		t.Fatal(err)
	}
	registers := func() SetupPathStep {
		t.Helper()
		steps, err := GetSetupPath(tenantID)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range steps {
			if s.ID == "registers" {
				return s
			}
		}
		t.Fatal("no registers step")
		return SetupPathStep{}
	}
	if r := registers(); !r.Optional || r.Done || r.Next || r.State != SetupStateWaived {
		t.Fatalf("with no brand, style or colour: %+v, want optional, waived, not Next", r)
	}
	raw, _ := json.Marshal(map[string]interface{}{"code": "BR-1", "name": "Acme"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ('BR-1', 'Brand', $1, 'Active', 'system')", raw); err != nil {
		t.Fatal(err)
	}
	r := registers()
	if !r.Done || r.Count != 1 || r.State != SetupStateReady {
		t.Fatalf("one brand and no styles or colours: %+v, want done and ready", r)
	}
	for _, c := range r.Checks {
		if !c.Passed {
			t.Errorf("check %s fails with a brand in use", c.ID)
		}
	}
}

// 47.12.4 (ask 191): each step's owner is the roles that may complete it,
// read from Settings › Roles - and it agrees with RoleMayOnDoctype, the check
// a save actually passes.
func TestStage47124StepOwnersAreTheRolesThatMayCompleteIt(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	if _, err := db.DB.Exec("INSERT INTO " + schema + `.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete) VALUES
		('Setup Clerk 47124', 'LegalEntity', TRUE, TRUE, TRUE, FALSE),
		('Setup Clerk 47124', 'ItemType', TRUE, TRUE, FALSE, FALSE),
		('Brand Clerk 47124', 'Brand', TRUE, TRUE, FALSE, FALSE),
		('Reader 47124', 'LegalEntity', TRUE, FALSE, FALSE, FALSE)`); err != nil {
		t.Fatal(err)
	}
	steps, err := GetSetupPath(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	owners := map[string]map[string]bool{}
	for _, s := range steps {
		owners[s.ID] = map[string]bool{}
		for _, r := range s.Owners {
			owners[s.ID][r] = true
		}
		if !owners[s.ID][RoleAdmin] || !owners[s.ID][RoleSuperAdmin] {
			t.Errorf("step %s owners %v: full access always owns a step", s.ID, s.Owners)
		}
	}
	want := map[string]map[string]bool{
		"entity":     {"Setup Clerk 47124": true, "Brand Clerk 47124": false, "Reader 47124": false},
		"item-types": {"Setup Clerk 47124": false},                            // ItemType yes, Category no: all of them
		"registers":  {"Brand Clerk 47124": true, "Setup Clerk 47124": false}, // any one
		"numbering":  {"Setup Clerk 47124": false},                            // a screen behind full access
	}
	for id, roles := range want {
		for role, is := range roles {
			if owners[id][role] != is {
				t.Errorf("step %s: %s owner = %v, want %v", id, role, owners[id][role], is)
			}
		}
	}
	// Every all-of record step agrees with the check a save passes.
	for _, s := range steps {
		if len(s.Doctypes) == 0 || s.ID == "registers" {
			continue
		}
		for _, role := range TenantRoleNames(tenantID) {
			may := true
			for _, dt := range s.Doctypes {
				ok, err := RoleMayOnDoctype(tenantID, role, dt, "create")
				if err != nil {
					t.Fatal(err)
				}
				may = may && ok
			}
			if owners[s.ID][role] != may {
				t.Errorf("step %s: %s owner = %v, RoleMayOnDoctype says %v", s.ID, role, owners[s.ID][role], may)
			}
		}
	}
}
