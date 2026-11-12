package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
)

// Stage 47.12.4: a step's state change leaves a history row (who, when, from,
// to), a person can add a note with an attachment reference, and history rows
// cannot be edited or deleted.
func TestStage47124SetupStepHistoryIsAppendOnly(t *testing.T) {
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
	exec("DELETE FROM " + schema + ".documents WHERE doctype = 'LegalEntity'")

	rowsFor := func(step string) []SetupStepHistoryRow {
		t.Helper()
		h, err := ListSetupStepHistory(tenantID, step)
		if err != nil {
			t.Fatalf("ListSetupStepHistory: %v", err)
		}
		return h
	}
	if _, err := GetSetupPathAs(tenantID, "asha"); err != nil {
		t.Fatal(err)
	}
	h := rowsFor("entity")
	if len(h) != 1 || h[0].FromState != "" || h[0].ToState != SetupStateDraft || h[0].ChangedBy != "asha" || h[0].Kind != "State" {
		t.Fatalf("first read: %+v, want one baseline row Draft by asha", h)
	}
	// reading again with nothing changed adds nothing
	if _, err := GetSetupPathAs(tenantID, "ravi"); err != nil {
		t.Fatal(err)
	}
	if h = rowsFor("entity"); len(h) != 1 {
		t.Fatalf("unchanged state added a row: %+v", h)
	}
	// a legal entity without a GSTIN: the step moves, and the row says why
	raw, _ := json.Marshal(map[string]interface{}{"name": "Acme", "gstin": "NOT-A-GSTIN", "state": "Karnataka"})
	exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ('LE-HIST', 'LegalEntity', $1, 'Active', 'system')", raw)
	if _, err := GetSetupPathAs(tenantID, "ravi"); err != nil {
		t.Fatal(err)
	}
	h = rowsFor("entity")
	if len(h) != 2 || h[0].FromState != SetupStateDraft || h[0].ToState != SetupStateInvalid || h[0].ChangedBy != "ravi" || len(h[0].Failing) == 0 || h[0].ChangedAt == "" {
		t.Fatalf("after the change: %+v, want Draft -> Invalid by ravi with the failing check", h)
	}
	// a note with an attachment reference
	if err := AddSetupStepNote(tenantID, "entity", "GSTIN certificate awaited", "gst-reg-2026.pdf", "asha"); err != nil {
		t.Fatalf("AddSetupStepNote: %v", err)
	}
	if err := AddSetupStepNote(tenantID, "no-such-step", "x", "", "asha"); err == nil {
		t.Errorf("a note on an unknown step should be refused")
	}
	if err := AddSetupStepNote(tenantID, "entity", "  ", "", "asha"); err == nil {
		t.Errorf("an empty note should be refused")
	}
	h = rowsFor("entity")
	if len(h) != 3 || h[0].Kind != "Note" || h[0].Note != "GSTIN certificate awaited" || h[0].Attachment != "gst-reg-2026.pdf" || h[0].ChangedBy != "asha" {
		t.Fatalf("after the note: %+v", h)
	}
	// history cannot be edited or deleted
	if _, err := db.DB.Exec("UPDATE " + schema + ".setup_step_history SET note = 'edited'"); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Errorf("UPDATE of history: err = %v, want append-only refusal", err)
	}
	// (the tests' connections are a sanctioned purge; production's is not)
	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec("SET LOCAL erp.sanctioned_write = ''"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec("DELETE FROM " + schema + ".setup_step_history"); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Errorf("DELETE of history: err = %v, want append-only refusal", err)
	}
	tx.Rollback()
	if h = rowsFor("entity"); len(h) != 3 {
		t.Errorf("history rows after refused edits = %d, want 3", len(h))
	}
}
