package engines

import (
	"encoding/json"
	"strings"
	"testing"

	"custom_erp/db"
)

// gstinWithCheckDigit completes a 14-character GSTIN body with its check
// digit, so a fixture carries a number the validator accepts.
func gstinWithCheckDigit(t *testing.T, body string) string {
	t.Helper()
	for _, c := range gstinChars {
		if g := body + string(c); ValidGSTINChecksum(g) {
			return g
		}
	}
	t.Fatalf("no check digit completes %s", body)
	return ""
}

// Stage 47.12.4: a step is Done once a record exists, but its readiness
// state comes from what the record says - GSTIN present and valid, state
// matching the GSTIN, stores linked to an active entity, tax series inside
// GST's 16 characters - with the failing records named, a severity, a
// validator version, and Awaiting Approval / Waived where they apply.
func TestStage47124SetupReadinessIsNotRowCount(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec("DELETE FROM " + schema + ".documents WHERE doctype IN ('LegalEntity', 'Location', 'HSNCode')")
	doc := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status)
	}
	setData := func(id string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		exec("UPDATE "+schema+".documents SET data = data || $2::jsonb WHERE id = $1", id, string(raw))
	}
	step := func(id string) SetupPathStep {
		t.Helper()
		steps, err := GetSetupPath(tenantID)
		if err != nil {
			t.Fatalf("GetSetupPath: %v", err)
		}
		for _, s := range steps {
			if s.ID == id {
				return s
			}
		}
		t.Fatalf("no step %s", id)
		return SetupPathStep{}
	}
	check := func(s SetupPathStep, id string) SetupCheck {
		t.Helper()
		for _, c := range s.Checks {
			if c.ID == id {
				return c
			}
		}
		t.Fatalf("step %s has no check %s: %+v", s.ID, id, s.Checks)
		return SetupCheck{}
	}

	// Every step carries a state, its checks and the rule set's version.
	steps, err := GetSetupPath(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	valid := map[string]bool{SetupStateDraft: true, SetupStateInvalid: true, SetupStateAwaitingApproval: true, SetupStateReady: true, SetupStateWaived: true}
	for _, s := range steps {
		if !valid[s.State] || len(s.Checks) == 0 || s.ValidatorVersion != SetupValidatorVersion {
			t.Errorf("step %s: state %q, %d checks, version %q", s.ID, s.State, len(s.Checks), s.ValidatorVersion)
		}
		if s.Optional && s.Count == 0 && s.State != SetupStateWaived {
			t.Errorf("an unused optional step is Waived, not %s: %s", s.State, s.ID)
		}
		for _, c := range s.Checks {
			if c.Label == "" || (c.Severity != SetupSeverityBlocking && c.Severity != SetupSeverityWarning) || (!c.Passed && c.Detail == "") {
				t.Errorf("step %s check %+v: a label, a severity, and a detail when it fails", s.ID, c)
			}
		}
	}
	if e := step("entity"); e.State != SetupStateDraft || e.Severity != SetupSeverityBlocking {
		t.Fatalf("an empty legal entity step is a blocking Draft: %+v", e)
	}

	// Row count says done; readiness says Invalid until the GSTIN is there.
	doc("LE-R", "LegalEntity", "Active", map[string]interface{}{"code": "LE-R", "name": "Acme Pvt Ltd"})
	e := step("entity")
	if !e.Done || e.State != SetupStateInvalid || check(e, "entity-gstin").Passed || check(e, "entity-gstin").Detail != "LE-R" {
		t.Fatalf("one record without a GSTIN is Done but Invalid, naming it: %+v", e)
	}

	// A GSTIN with a wrong check digit is not a GSTIN.
	good := gstinWithCheckDigit(t, "29ABCDE1234F1Z")
	bad := good[:14] + map[bool]string{true: "A", false: "B"}[good[14] != 'A']
	setData("LE-R", map[string]interface{}{"gstin": bad, "state_code": "29"})
	if c := check(step("entity"), "entity-gstin-valid"); c.Passed {
		t.Fatalf("a mistyped GSTIN fails the validity check: %+v", c)
	}

	// The state must agree with the GSTIN's first two digits.
	setData("LE-R", map[string]interface{}{"gstin": good, "state_code": "27"})
	if c := check(step("entity"), "entity-state"); c.Passed {
		t.Fatalf("a Maharashtra state on a Karnataka GSTIN fails: %+v", c)
	}
	setData("LE-R", map[string]interface{}{"state_code": "29"})
	e = step("entity")
	if e.State != SetupStateReady || e.Severity != SetupSeverityWarning || check(e, "entity-address").Passed {
		t.Fatalf("valid GSTIN and state make it Ready, the missing address a warning: %+v", e)
	}
	setData("LE-R", map[string]interface{}{"address": "1 MG Road", "pincode": "560001"})
	if e = step("entity"); e.State != SetupStateReady || e.Severity != "" {
		t.Fatalf("nothing failing: Ready with no severity: %+v", e)
	}

	// A second entity still waiting for its checker holds the step.
	doc("LE-P", "LegalEntity", "Pending Approval", map[string]interface{}{"code": "LE-P", "name": "Acme Two", "gstin": good})
	if e = step("entity"); e.State != SetupStateAwaitingApproval {
		t.Fatalf("a record pending approval: Awaiting Approval, got %+v", e)
	}
	exec("DELETE FROM "+schema+".documents WHERE id = $1", "LE-P")

	// A store must belong to an active legal entity.
	doc("ST-R", "Location", "Active", map[string]interface{}{"code": "ST-R", "name": "Store R", "type": "Store"})
	l := step("locations")
	if !l.Done || l.State != SetupStateInvalid || check(l, "locations-entity").Detail != "ST-R" {
		t.Fatalf("a store with no legal entity is Invalid, naming it: %+v", l)
	}
	setData("ST-R", map[string]interface{}{"legal_entity": "LE-R"})
	if l = step("locations"); l.State != SetupStateReady || l.Severity != SetupSeverityWarning || check(l, "locations-cost-centre").Passed {
		t.Fatalf("linked: Ready, the missing cost centre a warning: %+v", l)
	}
	exec("UPDATE "+schema+".documents SET status = 'Inactive' WHERE id = $1", "LE-R")
	if l = step("locations"); check(l, "locations-entity").Passed {
		t.Fatalf("a store whose legal entity is switched off fails: %+v", l)
	}
	exec("UPDATE "+schema+".documents SET status = 'Active' WHERE id = $1", "LE-R")

	// An HSN code that is not 4, 6 or 8 digits is refused on an Item, so it
	// cannot stand as the tax step's evidence.
	doc("HSN-R", "HSNCode", "Active", map[string]interface{}{"code": "HSN-R", "hsn": "61091"})
	tx := step("tax")
	if !tx.Done || tx.State != SetupStateInvalid || check(tx, "tax-hsn-shape").Detail != "HSN-R" {
		t.Fatalf("a 5-digit HSN code: Done but Invalid: %+v", tx)
	}
	setData("HSN-R", map[string]interface{}{"hsn": "6109"})
	if tx = step("tax"); tx.State != SetupStateReady {
		t.Fatalf("a 4-digit HSN code and a GSTIN: Ready, got %+v", tx)
	}

	// The shipped tax series print the store's code: with a 4-character code
	// a debit note number is 18 characters - over GST's 16 - and the step
	// says so; a short code brings every series back inside it.
	n := step("numbering")
	if c := check(n, "numbering-tax-series"); !c.Passed {
		t.Fatalf("provisioning seeds the tax series: %+v", c)
	}
	if c := check(n, "numbering-tax-length"); c.Passed || !strings.Contains(c.Detail, "DN prints DN/ST-R/") {
		t.Fatalf("a 4-character store code pushes the debit note number over 16: %+v", c)
	}
	setData("ST-R", map[string]interface{}{"short_code": "R"})
	if c := check(step("numbering"), "numbering-tax-length"); !c.Passed {
		t.Fatalf("a 1-character short code fits every tax series: %+v", c)
	}

	// A sales invoice series that prints more than GST's 16 characters.
	exec("UPDATE " + schema + ".prefix_configs SET prefix = 'SALESINVOICEX', include_store = TRUE WHERE doc_type = 'SI'")
	n = step("numbering")
	if c := check(n, "numbering-tax-length"); c.Passed || !strings.HasPrefix(c.Detail, "SI prints SALESINVOICEX") || n.State != SetupStateInvalid {
		t.Fatalf("an 18+ character invoice number fails the GST length check: %+v", n)
	}
	exec("UPDATE " + schema + ".prefix_configs SET active_status = FALSE WHERE doc_type = 'CN'")
	if c := check(step("numbering"), "numbering-tax-series"); c.Passed || !strings.Contains(c.Detail, "CN") {
		t.Fatalf("a switched-off credit note series fails: %+v", c)
	}
}
